// Standalone synthetic-token probe for Issue #3; not a product authentication
// handler. It does not expose token or process APIs to the common Web UI.
#[cfg(windows)]
mod probe {
    use base64::{Engine, engine::general_purpose::URL_SAFE_NO_PAD};
    use sha2::{Digest, Sha256};
    use std::io::{Read, Write};
    use std::net::{TcpListener, TcpStream};
    use tauri::Manager;
    use uuid::Uuid;
    use windows::Win32::Foundation::{HLOCAL, LocalFree};
    use windows::Win32::Security::Credentials::{
        CRED_PERSIST_LOCAL_MACHINE, CRED_TYPE_GENERIC, CREDENTIALW, CredDeleteW, CredFree,
        CredReadW, CredWriteW,
    };
    use windows::Win32::Security::Cryptography::{
        CRYPT_INTEGER_BLOB, CRYPTPROTECT_UI_FORBIDDEN, CryptProtectData, CryptUnprotectData,
    };
    use windows::core::{PCWSTR, PWSTR};

    fn dpapi_roundtrip(access_token: &str) -> Result<(), Box<dyn std::error::Error>> {
        let mut input = access_token.as_bytes().to_vec();
        let plain = CRYPT_INTEGER_BLOB {
            cbData: input.len() as u32,
            pbData: input.as_mut_ptr(),
        };
        let mut protected = CRYPT_INTEGER_BLOB::default();
        unsafe {
            CryptProtectData(
                &plain,
                PCWSTR::null(),
                None,
                None,
                None,
                CRYPTPROTECT_UI_FORBIDDEN,
                &mut protected,
            )?
        };
        let encrypted = unsafe {
            let data =
                std::slice::from_raw_parts(protected.pbData, protected.cbData as usize).to_vec();
            LocalFree(Some(HLOCAL(protected.pbData.cast())));
            data
        };
        assert_ne!(encrypted, input);
        let file = std::env::temp_dir().join(format!("koujou-issue3-dpapi-{}.bin", Uuid::new_v4()));
        let mut output = std::fs::OpenOptions::new()
            .create_new(true)
            .write(true)
            .open(&file)?;
        output.write_all(&encrypted)?;
        drop(output);
        let result: Result<(), Box<dyn std::error::Error>> = (|| {
            let mut saved = std::fs::read(&file)?;
            let protected = CRYPT_INTEGER_BLOB {
                cbData: saved.len() as u32,
                pbData: saved.as_mut_ptr(),
            };
            let mut recovered = CRYPT_INTEGER_BLOB::default();
            unsafe {
                CryptUnprotectData(
                    &protected,
                    None,
                    None,
                    None,
                    None,
                    CRYPTPROTECT_UI_FORBIDDEN,
                    &mut recovered,
                )?
            };
            let matches = unsafe {
                let bytes = std::slice::from_raw_parts(recovered.pbData, recovered.cbData as usize);
                let matches = bytes == input;
                LocalFree(Some(HLOCAL(recovered.pbData.cast())));
                matches
            };
            if !matches {
                return Err("DPAPI recovered content differs".into());
            }
            Ok(())
        })();
        std::fs::remove_file(&file)?;
        result
    }

    fn exchange(
        issuer_port: u16,
        code: &str,
        redirect: &str,
        verifier: &str,
    ) -> std::io::Result<String> {
        let body = tauri::Url::parse_with_params(
            "http://127.0.0.1/token",
            &[
                ("grant_type", "authorization_code"),
                ("code", code),
                ("redirect_uri", redirect),
                ("code_verifier", verifier),
            ],
        )
        .unwrap()
        .query()
        .unwrap()
        .to_string();
        let mut stream = TcpStream::connect(("127.0.0.1", issuer_port))?;
        stream.set_read_timeout(Some(std::time::Duration::from_secs(5)))?;
        write!(
            stream,
            "POST /token HTTP/1.1\r\nHost: 127.0.0.1:{issuer_port}\r\nContent-Type: application/x-www-form-urlencoded\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{body}",
            body.len()
        )?;
        let mut response = String::new();
        stream.read_to_string(&mut response)?;
        Ok(response)
    }

    fn credential_roundtrip(access_token: &str) -> Result<(), Box<dyn std::error::Error>> {
        let name = format!("koujou-san/issue3-probe/{}", Uuid::new_v4());
        let mut wide: Vec<u16> = name.encode_utf16().chain(Some(0)).collect();
        let mut token = access_token.as_bytes().to_vec();
        let credential = CREDENTIALW {
            Type: CRED_TYPE_GENERIC,
            TargetName: PWSTR(wide.as_mut_ptr()),
            CredentialBlobSize: token.len() as u32,
            CredentialBlob: token.as_mut_ptr(),
            Persist: CRED_PERSIST_LOCAL_MACHINE,
            ..Default::default()
        };
        // The API receives owned buffers valid for the duration of each call.
        unsafe { CredWriteW(&credential, 0)? };
        let result: Result<(), Box<dyn std::error::Error>> = (|| {
            let mut saved = std::ptr::null_mut();
            unsafe { CredReadW(PCWSTR(wide.as_ptr()), CRED_TYPE_GENERIC, None, &mut saved)? };
            let matches = unsafe {
                let data = &*saved;
                let bytes = std::slice::from_raw_parts(
                    data.CredentialBlob,
                    data.CredentialBlobSize as usize,
                );
                let matches = bytes == token;
                CredFree(saved.cast());
                matches
            };
            if !matches {
                return Err("credential content mismatch".into());
            }
            Ok(())
        })();
        let cleanup = unsafe { CredDeleteW(PCWSTR(wide.as_ptr()), CRED_TYPE_GENERIC, None) };
        result?;
        cleanup?;
        let mut remaining = std::ptr::null_mut();
        if unsafe {
            CredReadW(
                PCWSTR(wide.as_ptr()),
                CRED_TYPE_GENERIC,
                None,
                &mut remaining,
            )
        }
        .is_ok()
        {
            unsafe { CredFree(remaining.cast()) };
            return Err("probe credential remained after deletion".into());
        }
        Ok(())
    }

    fn reply(stream: &mut TcpStream, status: &str, body: &str) -> std::io::Result<()> {
        write!(
            stream,
            "HTTP/1.1 {status}\r\nContent-Type: text/plain\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{body}",
            body.len()
        )
    }

    pub fn run() {
        let issuer_port: u16 = std::env::args()
            .nth(1)
            .expect("synthetic issuer port")
            .parse()
            .unwrap();
        let backend = std::env::args()
            .nth(2)
            .unwrap_or_else(|| "credential-manager".into());
        assert!(backend == "credential-manager" || backend == "dpapi-current-user");
        let listener = TcpListener::bind("127.0.0.1:0").expect("loopback bind");
        let port = listener.local_addr().unwrap().port();
        let state = Uuid::new_v4().simple().to_string();
        let verifier = format!("{}{}", Uuid::new_v4().simple(), Uuid::new_v4().simple());
        let challenge = URL_SAFE_NO_PAD.encode(Sha256::digest(verifier.as_bytes()));
        let redirect = format!("http://127.0.0.1:{port}/callback");
        let authorize = tauri::Url::parse_with_params(
            &format!("http://127.0.0.1:{issuer_port}/authorize"),
            &[
                ("redirect_uri", redirect.as_str()),
                ("state", state.as_str()),
                ("code_challenge", challenge.as_str()),
                ("code_challenge_method", "S256"),
            ],
        )
        .unwrap();
        println!(
            "{}",
            serde_json::json!({"port":port,"authorize_url":authorize.as_str()})
        );
        let mut context = tauri::generate_context!();
        context.config_mut().app.windows[0].visible = false;
        tauri::Builder::default()
            .setup(move |app| {
                app.get_webview_window("main").unwrap().hide()?;
                let handle = app.handle().clone();
                std::thread::spawn(move || {
                    // Independent Go issuer validates the actual verifier and
                    // consumes a one-time code; Ory integration belongs to #5.
                    for incoming in listener.incoming() {
                        let mut stream = incoming.unwrap();
                        stream.set_read_timeout(Some(std::time::Duration::from_secs(5))).unwrap();
                        let mut bytes = [0_u8; 4096];
                        let size = stream.read(&mut bytes).unwrap_or(0);
                        let request = String::from_utf8_lossy(&bytes[..size]);
                        let route = request.lines().next().unwrap_or("").split_whitespace().nth(1).unwrap_or("");
                        let callback = tauri::Url::parse(&format!("http://127.0.0.1:{port}{route}")).unwrap();
                        let values: std::collections::HashMap<_, _> = callback.query_pairs().collect();
                        if callback.path() == "/callback" && values.get("state").is_some_and(|value| value == state.as_str()) && values.contains_key("code") {
                            let code = values.get("code").unwrap();
                            let incorrect = exchange(issuer_port, code, &redirect, &"0".repeat(64)).unwrap();
                            assert!(incorrect.starts_with("HTTP/1.1 400"));
                            let exchanged = exchange(issuer_port, code, &redirect, &verifier).unwrap();
                            assert!(exchanged.starts_with("HTTP/1.1 200"));
                            let body = exchanged.split("\r\n\r\n").nth(1).unwrap();
                            let token: serde_json::Value = serde_json::from_str(body).unwrap();
                            assert_eq!(token["access_token"], "synthetic-issue3-token");
                            let reused = exchange(issuer_port, code, &redirect, &verifier).unwrap();
                            assert!(reused.starts_with("HTTP/1.1 400"));
                            let access_token = token["access_token"].as_str().unwrap();
                            let stored = if backend == "dpapi-current-user" { dpapi_roundtrip(access_token) } else { credential_roundtrip(access_token) };
                            match stored {
                                Ok(()) => {
                                    reply(&mut stream, "200 OK", "Synthetic native callback and credential probe passed.").unwrap();
                                    println!("{}", serde_json::json!({"callback":true,"pkce_s256":true,"wrong_verifier_rejected":true,"code_reuse_rejected":true,"credential_roundtrip":true,"probe_material_deleted":true,"storage_backend":backend,"token_exposed_to_ui":false}));
                                    handle.exit(0);
                                }
                                Err(error) => {
                                    reply(&mut stream, "500 Internal Server Error", "Credential probe failed.").unwrap();
                                    eprintln!("credential probe failed: {error}");
                                    handle.exit(1);
                                }
                            }
                            break;
                        } else {
                            reply(&mut stream, "400 Bad Request", "Callback state or route mismatch.").unwrap();
                            println!("{{\"callback_rejected\":true,\"credential_write\":false}}");
                        }
                    }
                });
                Ok(())
            })
            .run(context)
            .expect("probe runtime failed");
    }
}

#[cfg(windows)]
fn main() {
    probe::run();
}

#[cfg(not(windows))]
fn main() {
    eprintln!("This probe covers Windows; other OS credential stores are not verified.");
    std::process::exit(2);
}
