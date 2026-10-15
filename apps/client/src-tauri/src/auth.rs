use base64::{Engine, engine::general_purpose::URL_SAFE_NO_PAD};
use reqwest::blocking::Client;
use serde::{Deserialize, Serialize};
use serde_json::{Value, json};
use sha2::{Digest, Sha256};
use std::{
    fs,
    io::{BufRead, BufReader, Write},
    net::{TcpListener, TcpStream},
    path::PathBuf,
    sync::Mutex,
    time::{Duration, Instant, SystemTime, UNIX_EPOCH},
};
use tauri::{Manager, Url};
use uuid::Uuid;

#[derive(Deserialize)]
struct Config {
    issuer: String,
    client_id: String,
    redirect_uri: String,
    api_url: String,
    site_url: String,
    audience: String,
}
#[derive(Default, Deserialize, Serialize)]
struct Tokens {
    access_token: String,
    refresh_token: String,
    #[serde(default)]
    expires_in: u64,
    #[serde(default)]
    expires: u64,
}
#[derive(Deserialize, Serialize)]
struct Saved {
    api: String,
    issuer: String,
    refresh: String,
}

pub struct Auth {
    api: Url,
    client: Client,
    file: PathBuf,
    tokens: Mutex<Tokens>,
    login_lock: Mutex<()>,
    device_lock: Mutex<()>,
}

impl Auth {
    pub(crate) fn sync_binding(&self, user: &str, workspace: &str) -> (PathBuf, String) {
        let prefix = hex_name(&format!("{}\0{}", self.api, user));
        let name = format!("{prefix}-{}.dpapi", hex_name(workspace));
        (
            self.file.parent().unwrap().join("sync").join(name),
            self.api.to_string(),
        )
    }
    pub fn new(api: &str, file: PathBuf) -> Result<Self, String> {
        let api = Url::parse(api).map_err(|_| "invalid_api_url")?;
        if api.scheme() != "https"
            || api.path() != "/"
            || api.query().is_some()
            || api.fragment().is_some()
            || !api.username().is_empty()
        {
            return Err("HTTPS API root required".into());
        }
        let mut client = Client::builder()
            .timeout(Duration::from_secs(10))
            .redirect(reqwest::redirect::Policy::none());
        if matches!(api.host_str(), Some("localhost" | "127.0.0.1")) {
            if let Ok(path) = std::env::var("KOUJOU_DEV_CA_FILE") {
                let pem = fs::read(path).map_err(|_| "development_CA_unavailable")?;
                client = client.add_root_certificate(
                    reqwest::Certificate::from_pem(&pem).map_err(|_| "invalid_development_CA")?,
                );
            }
        }
        Ok(Self {
            api,
            client: client.build().map_err(|_| "http_client_failed")?,
            file,
            tokens: Mutex::new(Tokens::default()),
            login_lock: Mutex::new(()),
            device_lock: Mutex::new(()),
        })
    }
    fn config(&self) -> Result<Config, String> {
        let config: Config = self
            .client
            .get(self.api.join("auth/config").unwrap())
            .send()
            .map_err(|_| "api_unavailable")?
            .error_for_status()
            .map_err(|_| "api_configuration_failed")?
            .json()
            .map_err(|_| "invalid_auth_configuration")?;
        let issuer = Url::parse(&config.issuer).map_err(|_| "invalid_issuer")?;
        let site = Url::parse(&config.site_url).map_err(|_| "invalid_site")?;
        let callback = Url::parse(&config.redirect_uri).map_err(|_| "invalid_callback")?;
        if issuer.scheme() != "https"
            && !(issuer.scheme() == "http"
                && matches!(issuer.host_str(), Some("127.0.0.1" | "localhost")))
        {
            return Err("insecure_issuer".into());
        }
        if config.api_url.trim_end_matches('/') != self.api.as_str().trim_end_matches('/')
            || site.scheme() != "https"
            || callback.scheme() != "http"
            || callback.host_str() != Some("127.0.0.1")
            || callback.port().is_none()
            || callback.query().is_some()
            || callback.fragment().is_some()
            || !callback.username().is_empty()
        {
            return Err("unexpected_auth_configuration".into());
        }
        Ok(config)
    }
    fn token(&self, config: &Config, form: &[(&str, &str)]) -> Result<Tokens, String> {
        let response = self
            .client
            .post(format!("{}oauth2/token", config.issuer))
            .form(form)
            .send()
            .map_err(|_| "identity_provider_unavailable")?;
        if response.status().is_server_error() {
            return Err("identity_provider_unavailable".into());
        }
        let mut tokens: Tokens = response
            .error_for_status()
            .map_err(|_| "authentication_required")?
            .json()
            .map_err(|_| "invalid_token_response")?;
        tokens.expires = now() + tokens.expires_in;
        Ok(tokens)
    }
    fn persist(&self, config: &Config, tokens: &Tokens) -> Result<(), String> {
        let data = serde_json::to_vec(&Saved {
            api: self.api.to_string(),
            issuer: config.issuer.clone(),
            refresh: tokens.refresh_token.clone(),
        })
        .map_err(|_| "encoding_failed")?;
        if let Some(parent) = self.file.parent() {
            fs::create_dir_all(parent).map_err(|_| "credential_directory_failed")?
        }
        let encrypted = protect(&data)?;
        fs::write(&self.file, encrypted).map_err(|_| "credential_storage_failed".into())
    }
    fn access(&self) -> Result<String, String> {
        let mut tokens = self.tokens.lock().map_err(|_| "auth_state_failed")?;
        if !tokens.access_token.is_empty() && tokens.expires > now() + 15 {
            return Ok(tokens.access_token.clone());
        }
        let config = self.config()?;
        if tokens.refresh_token.is_empty() {
            let bytes = fs::read(&self.file).map_err(|_| "authentication_required")?;
            let saved: Saved =
                serde_json::from_slice(&unprotect(&bytes)?).map_err(|_| "invalid_credentials")?;
            if saved.api != self.api.as_str() || saved.issuer != config.issuer {
                return Err("credential_server_mismatch".into());
            }
            tokens.refresh_token = saved.refresh;
        }
        let refreshed = self.token(
            &config,
            &[
                ("grant_type", "refresh_token"),
                ("refresh_token", &tokens.refresh_token),
                ("client_id", &config.client_id),
            ],
        )?;
        self.persist(&config, &refreshed)?;
        *tokens = refreshed;
        Ok(tokens.access_token.clone())
    }
    pub(crate) fn request_response(
        &self,
        path: &str,
        method: &str,
        body: Value,
    ) -> Result<reqwest::blocking::Response, String> {
        if !path.starts_with('/') || path.starts_with("//") {
            return Err("relative_API_path_required".into());
        }
        let target = self.api.join(path).map_err(|_| "invalid_API_path")?;
        if target.origin() != self.api.origin() {
            return Err("foreign_API_origin".into());
        }
        let verb = reqwest::Method::from_bytes(method.as_bytes()).map_err(|_| "invalid_method")?;
        let mut request = self.client.request(verb, target);
        if !matches!(path, "/healthz" | "/readyz" | "/auth/config") {
            request = request.bearer_auth(self.access()?)
        }
        if !body.is_null() {
            request = request.json(&body)
        }
        let response = request.send().map_err(|_| "api_unavailable")?;
        Ok(response)
    }
    pub fn request(&self, path: &str, method: &str, body: Value) -> Result<Value, String> {
        let response = self.request_response(path, method, body)?;
        if !response.status().is_success() {
            return Err(if response.status() == 401 {
                "authentication_required"
            } else if response.status() == 403 {
                "forbidden"
            } else if response.status() == 409 {
                "version_conflict"
            } else {
                "api_request_failed"
            }
            .into());
        }
        response.json().map_err(|_| "invalid_API_response".into())
    }
    pub fn login(&self, open: impl FnOnce(&str) -> Result<(), String>) -> Result<(), String> {
        self.login_until(open, Duration::from_secs(300))
    }
    fn login_until(
        &self,
        open: impl FnOnce(&str) -> Result<(), String>,
        lifetime: Duration,
    ) -> Result<(), String> {
        let _guard = self
            .login_lock
            .try_lock()
            .map_err(|_| "login_already_pending")?;
        let config = self.config()?;
        let redirect = Url::parse(&config.redirect_uri).map_err(|_| "invalid_callback")?;
        let listener = TcpListener::bind(("127.0.0.1", redirect.port().unwrap()))
            .map_err(|_| "callback_port_unavailable")?;
        listener
            .set_nonblocking(true)
            .map_err(|_| "callback_failed")?;
        let state = Uuid::new_v4().simple().to_string();
        let verifier = format!("{}{}", Uuid::new_v4().simple(), Uuid::new_v4().simple());
        let challenge = URL_SAFE_NO_PAD.encode(Sha256::digest(verifier.as_bytes()));
        let authorize = Url::parse_with_params(
            &format!("{}oauth2/auth", config.issuer),
            &[
                ("client_id", config.client_id.as_str()),
                ("redirect_uri", &config.redirect_uri),
                ("response_type", "code"),
                ("scope", "openid offline_access email profile koujou"),
                ("audience", &config.audience),
                ("state", &state),
                ("code_challenge", &challenge),
                ("code_challenge_method", "S256"),
            ],
        )
        .map_err(|_| "invalid_authorization_url")?;
        open(authorize.as_str())?;
        let start = Instant::now();
        while start.elapsed() < lifetime {
            match listener.accept() {
                Ok((mut stream, _)) => {
                    stream
                        .set_read_timeout(Some(Duration::from_secs(3)))
                        .map_err(|_| "callback_failed")?;
                    let mut line = String::new();
                    let mut reader = BufReader::new(&stream);
                    reader.read_line(&mut line).map_err(|_| "callback_failed")?;
                    let route = line.split_whitespace().nth(1).unwrap_or("");
                    let callback = Url::parse(&format!(
                        "{}{route}",
                        redirect.origin().ascii_serialization()
                    ))
                    .map_err(|_| "invalid_callback")?;
                    let values: std::collections::HashMap<_, _> = callback.query_pairs().collect();
                    if !line.starts_with("GET ")
                        || callback.path() != redirect.path()
                        || values.get("state").map(|v| v.as_ref()) != Some(state.as_str())
                        || !values.contains_key("code")
                    {
                        respond(&mut stream, "400 Bad Request", "認証要求を確認できません。");
                        continue;
                    }
                    let code = values.get("code").unwrap();
                    let tokens = self.token(
                        &config,
                        &[
                            ("grant_type", "authorization_code"),
                            ("code", code),
                            ("redirect_uri", &config.redirect_uri),
                            ("code_verifier", &verifier),
                            ("client_id", &config.client_id),
                        ],
                    )?;
                    self.client
                        .get(self.api.join("auth/me").unwrap())
                        .bearer_auth(&tokens.access_token)
                        .send()
                        .map_err(|_| "api_unavailable")?
                        .error_for_status()
                        .map_err(|_| "identity_validation_failed")?;
                    self.persist(&config, &tokens)?;
                    *self.tokens.lock().map_err(|_| "auth_state_failed")? = tokens;
                    respond(
                        &mut stream,
                        "200 OK",
                        "認証できました。koujou-sanへ戻ってください。",
                    );
                    return Ok(());
                }
                Err(err) if err.kind() == std::io::ErrorKind::WouldBlock => {
                    std::thread::sleep(Duration::from_millis(50))
                }
                Err(_) => return Err("callback_failed".into()),
            }
        }
        Err("login_expired".into())
    }
    pub fn logout(&self) -> Result<Value, String> {
        let _ = self.access();
        let config = self.config();
        let mut tokens = self.tokens.lock().map_err(|_| "auth_state_failed")?;
        let mut revoked = true;
        if let Ok(config) = config {
            for token in [&tokens.refresh_token, &tokens.access_token] {
                if token.is_empty() {
                    continue;
                }
                let result = self
                    .client
                    .post(format!("{}oauth2/revoke", config.issuer))
                    .form(&[
                        ("token", token.as_str()),
                        ("client_id", config.client_id.as_str()),
                    ])
                    .send();
                if !result.is_ok_and(|r| r.status().is_success()) {
                    revoked = false
                }
            }
        } else {
            revoked = false
        }
        if self.file.exists() {
            fs::remove_file(&self.file).map_err(|_| "credential_delete_failed")?
        }
        *tokens = Tokens::default();
        Ok(
            json!({"app_session":"ended","oauth_tokens":if revoked{"revoked"}else{"unconfirmed"},"kratos_session":"browser_session_unchanged"}),
        )
    }
}

fn now() -> u64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap_or_default()
        .as_secs()
}

#[derive(Default, Deserialize, Serialize)]
struct DeviceSaved {
    api: String,
    token: String,
    pending: Option<DevicePending>,
}
#[derive(Deserialize, Serialize)]
struct DevicePending {
    kind: String,
    body: Value,
}
impl Auth {
    fn device_file(&self, workspace: &str, user: &str) -> PathBuf {
        self.file.with_file_name(format!(
            "device-{}.dpapi",
            hex_name(&format!("{}\0{workspace}\0{user}", self.api))
        ))
    }
    pub fn device_identifier(&self) -> Result<String, String> {
        let path = self.file.with_file_name("device-identifier.txt");
        if let Ok(value) = fs::read_to_string(&path) {
            return Ok(value);
        }
        if let Some(parent) = path.parent() {
            fs::create_dir_all(parent).map_err(|_| "credential_directory_failed")?
        }
        let value = Uuid::new_v4().to_string();
        fs::write(path, &value).map_err(|_| "device_identifier_failed")?;
        Ok(value)
    }
    fn save_device(&self, path: &std::path::Path, record: &DeviceSaved) -> Result<(), String> {
        let bytes = serde_json::to_vec(record).map_err(|_| "encoding_failed")?;
        fs::write(path, protect(&bytes)?).map_err(|_| "credential_storage_failed".into())
    }
    fn load_device(&self, path: &std::path::Path) -> Result<DeviceSaved, String> {
        if !path.exists() {
            return Ok(DeviceSaved {
                api: self.api.to_string(),
                ..Default::default()
            });
        }
        let bytes = fs::read(path).map_err(|_| "credential_storage_failed")?;
        let record: DeviceSaved = serde_json::from_slice(&unprotect(&bytes)?)
            .map_err(|_| "invalid_device_credentials")?;
        if record.api != self.api.as_str() {
            return Err("credential_server_mismatch".into());
        };
        Ok(record)
    }
    pub fn device_enroll(&self, workspace: &str, name: &str) -> Result<Value, String> {
        let _guard = self.device_lock.lock().map_err(|_| "device_state_failed")?;
        let profile = self.request("/auth/me", "GET", Value::Null)?;
        let user = profile["user"]["id"]
            .as_str()
            .ok_or("authentication_required")?;
        let file = self.device_file(workspace, user);
        let mut record = self.load_device(&file)?;
        if record.pending.is_none() {
            let identifier = self.device_identifier()?;
            let mine = self.request(
                &format!("/access/workspaces/{workspace}/mine?identifier={identifier}"),
                "GET",
                Value::Null,
            )?;
            let device = &mine["device"];
            let (kind, id, version) = if device.is_null() {
                (
                    "device-register".to_owned(),
                    Uuid::new_v4().to_string(),
                    "0".to_owned(),
                )
            } else {
                (
                    "device-rotate".to_owned(),
                    device["id"]
                        .as_str()
                        .ok_or("invalid_device_response")?
                        .to_owned(),
                    device["version"]
                        .as_str()
                        .ok_or("invalid_device_response")?
                        .to_owned(),
                )
            };
            record.pending = Some(DevicePending {
                kind,
                body: json!({"id":id,"identifier":identifier,"name":name,"operation_id":Uuid::new_v4().to_string(),"expected_version":version}),
            });
            self.save_device(&file, &record)?;
        }
        let pending = record.pending.as_ref().unwrap();
        let result = self.request(
            &format!("/access/workspaces/{workspace}/{}", pending.kind),
            "POST",
            pending.body.clone(),
        );
        let value = match result {
            Ok(v) => v,
            Err(e) => {
                if e == "version_conflict" {
                    record.pending = None;
                    self.save_device(&file, &record)?
                };
                return Err(e);
            }
        };
        record.token = value["credential"]
            .as_str()
            .ok_or("invalid_device_response")?
            .to_owned();
        record.pending = None;
        self.save_device(&file, &record)?;
        Ok(value["device"].clone())
    }
    pub fn device_contact(&self, workspace: &str, user: &str) -> Result<Value, String> {
        let _guard = self.device_lock.lock().map_err(|_| "device_state_failed")?;
        let record = self.load_device(&self.device_file(workspace, user))?;
        if record.token.is_empty() {
            return Err("device_not_registered".into());
        }
        let state = self
            .client
            .get(
                self.api
                    .join(&format!("access/workspaces/{workspace}/device-state"))
                    .map_err(|_| "invalid_API_path")?,
            )
            .bearer_auth(&record.token)
            .send()
            .map_err(|_| "api_unavailable")?;
        if !state.status().is_success() {
            return Err("device_access_revoked".into());
        }
        let state: Value = state.json().map_err(|_| "invalid_device_response")?;
        let response=self.client.post(self.api.join(&format!("access/workspaces/{workspace}/contact")).map_err(|_|"invalid_API_path")?).bearer_auth(&record.token)
            .json(&json!({"operation_id":Uuid::new_v4().to_string(),"expected_version":state["device"]["version"]})).send().map_err(|_|"api_unavailable")?;
        if !response.status().is_success() {
            return Err("device_contact_failed".into());
        }
        Ok(json!({"contact":"confirmed"}))
    }
}

#[tauri::command]
pub async fn device_identifier(app: tauri::AppHandle) -> Result<String, String> {
    tauri::async_runtime::spawn_blocking(move || app.state::<Auth>().device_identifier())
        .await
        .map_err(|_| "device_task_failed")?
}
#[tauri::command]
pub async fn device_enroll(
    app: tauri::AppHandle,
    workspace: String,
    name: String,
) -> Result<Value, String> {
    tauri::async_runtime::spawn_blocking(move || {
        app.state::<Auth>().device_enroll(&workspace, &name)
    })
    .await
    .map_err(|_| "device_task_failed")?
}
#[tauri::command]
pub async fn device_contact(
    app: tauri::AppHandle,
    workspace: String,
    user: String,
) -> Result<Value, String> {
    tauri::async_runtime::spawn_blocking(move || {
        app.state::<Auth>().device_contact(&workspace, &user)
    })
    .await
    .map_err(|_| "device_task_failed")?
}
fn respond(stream: &mut TcpStream, status: &str, text: &str) {
    let _ = write!(
        stream,
        "HTTP/1.1 {status}\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{text}",
        text.len()
    );
}

#[cfg(windows)]
fn crypt(data: &[u8], encrypt: bool) -> Result<Vec<u8>, String> {
    use windows::Win32::Foundation::{HLOCAL, LocalFree};
    use windows::Win32::Security::Cryptography::{
        CRYPT_INTEGER_BLOB, CRYPTPROTECT_UI_FORBIDDEN, CryptProtectData, CryptUnprotectData,
    };
    let input = CRYPT_INTEGER_BLOB {
        cbData: data.len() as u32,
        pbData: data.as_ptr() as *mut u8,
    };
    let mut output = CRYPT_INTEGER_BLOB::default();
    unsafe {
        let result = if encrypt {
            CryptProtectData(
                &input,
                None,
                None,
                None,
                None,
                CRYPTPROTECT_UI_FORBIDDEN,
                &mut output,
            )
        } else {
            CryptUnprotectData(
                &input,
                None,
                None,
                None,
                None,
                CRYPTPROTECT_UI_FORBIDDEN,
                &mut output,
            )
        };
        result.map_err(|_| "credential_protection_failed")?;
        let result = std::slice::from_raw_parts(output.pbData, output.cbData as usize).to_vec();
        let _ = LocalFree(Some(HLOCAL(output.pbData.cast())));
        Ok(result)
    }
}
#[cfg(windows)]
pub(crate) fn protect(data: &[u8]) -> Result<Vec<u8>, String> {
    crypt(data, true)
}
#[cfg(windows)]
pub(crate) fn unprotect(data: &[u8]) -> Result<Vec<u8>, String> {
    crypt(data, false)
}
#[cfg(not(windows))]
pub(crate) fn protect(_: &[u8]) -> Result<Vec<u8>, String> {
    Err("Windows credential storage required".into())
}
#[cfg(not(windows))]
pub(crate) fn unprotect(_: &[u8]) -> Result<Vec<u8>, String> {
    Err("Windows credential storage required".into())
}

fn open_browser(url: &str) -> Result<(), String> {
    #[cfg(windows)]
    {
        use std::os::windows::process::CommandExt;
        std::process::Command::new("rundll32.exe")
            .args(["url.dll,FileProtocolHandler", url])
            .creation_flags(0x08000000)
            .spawn()
            .map_err(|_| "browser_open_failed")?;
        Ok(())
    }
    #[cfg(not(windows))]
    {
        let _ = url;
        Err("Windows desktop is the initial supported target".into())
    }
}

#[tauri::command]
pub async fn login(app: tauri::AppHandle) -> Result<(), String> {
    tauri::async_runtime::spawn_blocking(move || app.state::<Auth>().login(open_browser))
        .await
        .map_err(|_| "login_task_failed")?
}
#[tauri::command]
pub async fn logout(app: tauri::AppHandle) -> Result<Value, String> {
    tauri::async_runtime::spawn_blocking(move || app.state::<Auth>().logout())
        .await
        .map_err(|_| "logout_task_failed")?
}
#[tauri::command]
pub async fn api_request(
    app: tauri::AppHandle,
    path: String,
    method: String,
    body: Value,
) -> Result<Value, String> {
    tauri::async_runtime::spawn_blocking(move || {
        let result = app.state::<Auth>().request(&path, &method, body)?;
        public_response(result)
    })
    .await
    .map_err(|_| "request_task_failed")?
}
pub(crate) fn public_response(value: Value) -> Result<Value, String> {
    if value.get("credential").is_some() {
        Err("private_native_operation_required".into())
    } else {
        Ok(value)
    }
}
#[tauri::command]
pub async fn open_account(app: tauri::AppHandle, path: String) -> Result<(), String> {
    if path != "settings" {
        return Err("invalid_account_path".into());
    };
    tauri::async_runtime::spawn_blocking(move || {
        let config = app.state::<Auth>().config()?;
        open_browser(&format!(
            "{}/auth/settings",
            config.site_url.trim_end_matches('/')
        ))
    })
    .await
    .map_err(|_| "account_task_failed")?
}

pub fn setup(app: &mut tauri::App) -> Result<(), Box<dyn std::error::Error>> {
    let api =
        std::env::var("KOUJOU_PUBLIC_API_URL").unwrap_or_else(|_| "https://localhost:8080/".into());
    let key = hex_name(&api);
    let file = app
        .path()
        .app_local_data_dir()?
        .join(format!("auth-{key}.dpapi"));
    app.manage(Auth::new(&api, file).map_err(std::io::Error::other)?);
    Ok(())
}
fn hex_name(value: &str) -> String {
    Sha256::digest(value.as_bytes())
        .iter()
        .map(|byte| format!("{byte:02x}"))
        .collect()
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn api_origin_and_storage_boundary() {
        let dir = std::env::temp_dir().join(format!("koujou-auth-{}", Uuid::new_v4()));
        let auth = Auth::new("https://localhost:8080/", dir.join("credential")).unwrap();
        assert!(
            auth.request("https://example.com/", "GET", Value::Null)
                .is_err()
        );
        assert!(auth.request("//example.com/", "GET", Value::Null).is_err());
        assert!(!auth.file.exists());
    }
    #[cfg(windows)]
    #[test]
    fn windows_dpapi_roundtrip() {
        let value = b"synthetic refresh credential";
        let encrypted = protect(value).unwrap();
        assert_ne!(encrypted, value);
        assert_eq!(unprotect(&encrypted).unwrap(), value);
    }
    #[cfg(windows)]
    #[test]
    #[ignore = "requires the owned Context UI fixture and an unused native OAuth path"]
    fn live_context_ui_prepare() {
        let fixture: Value = serde_json::from_slice(
            &fs::read(std::env::var("KOUJOU_CONTEXT_UI_FIXTURE").unwrap()).unwrap(),
        )
        .unwrap();
        let file = PathBuf::from(std::env::var("KOUJOU_NATIVE_TEST_FILE").unwrap());
        assert!(!file.exists(), "select the unused fixture API namespace");
        let auth = Auth::new(fixture["api"].as_str().unwrap(), file).unwrap();
        let config = auth.config().unwrap();
        assert_eq!(config.issuer, fixture["issuer"]);
        let refresh = fixture["native_refresh"].as_str().unwrap();
        assert!(!refresh.is_empty());
        auth.persist(
            &config,
            &Tokens {
                refresh_token: refresh.into(),
                ..Tokens::default()
            },
        )
        .unwrap();
        let user = auth.request("/auth/me", "GET", Value::Null).unwrap();
        assert!(user["user"]["id"].is_string());
        println!("Owned native fixture authenticated; refresh credential remains DPAPI-protected.");
    }
    #[cfg(windows)]
    #[test]
    #[ignore = "requires the development API/Ory and an explicitly selected fixture OAuth file; close the development app first"]
    fn live_workspace_device_lifecycle() {
        let file = PathBuf::from(
            std::env::var("KOUJOU_NATIVE_TEST_FILE").expect("explicit fixture OAuth file required"),
        );
        let api = "https://localhost:8080/";
        let auth = Auth::new(api, file.clone()).unwrap();
        let profile = auth.request("/auth/me", "GET", Value::Null).unwrap();
        let user = profile["user"]["id"].as_str().unwrap();
        let workspace = Uuid::new_v4().to_string();
        auth.request(&format!("/access/workspaces/{workspace}/create"),"POST",json!({"name":"Windows device fixture","operation_id":Uuid::new_v4().to_string(),"expected_version":"0"})).unwrap();
        let first = auth.device_enroll(&workspace, "Windows fixture").unwrap();
        assert!(first["enabled"].as_bool().unwrap());
        assert_eq!(first["can_input"], false);
        assert_eq!(first["can_report"], false);
        assert!(first.get("credential").is_none());
        let path = auth.device_file(&workspace, user);
        let encrypted = fs::read(&path).unwrap();
        let saved = auth.load_device(&path).unwrap();
        assert!(!String::from_utf8_lossy(&encrypted).contains(&saved.token));
        assert!(saved.pending.is_none());
        auth.device_contact(&workspace, user).unwrap();
        let original = saved.token;
        drop(auth);
        let restarted = Auth::new(api, file).unwrap();
        restarted.device_contact(&workspace, user).unwrap();
        let second = restarted
            .device_enroll(&workspace, "Windows fixture")
            .unwrap();
        let current = restarted.load_device(&path).unwrap();
        assert!(original != current.token);
        let endpoint = restarted
            .api
            .join(&format!("access/workspaces/{workspace}/device-state"))
            .unwrap();
        assert_eq!(
            restarted
                .client
                .get(endpoint)
                .bearer_auth(original)
                .send()
                .unwrap()
                .status()
                .as_u16(),
            403
        );
        let id = second["id"].as_str().unwrap();
        let private=restarted.request(&format!("/access/workspaces/{workspace}/device-rotate"),"POST",json!({"id":id,"operation_id":Uuid::new_v4().to_string(),"expected_version":second["version"]})).unwrap();
        assert!(
            matches!(public_response(private),Err(e) if e=="private_native_operation_required")
        );
        restarted
            .device_enroll(&workspace, "Windows fixture")
            .unwrap();
        restarted.device_contact(&workspace, user).unwrap();
        let members = restarted
            .request(
                &format!("/access/workspaces/{workspace}/members"),
                "GET",
                Value::Null,
            )
            .unwrap();
        let member = members["data"]["items"]
            .as_array()
            .unwrap()
            .iter()
            .find(|m| m["id"] == user)
            .unwrap();
        restarted.request(&format!("/access/workspaces/{workspace}/leave"),"POST",json!({"operation_id":Uuid::new_v4().to_string(),"expected_version":member["version"]})).unwrap();
        assert_eq!(
            restarted.device_contact(&workspace, user),
            Err("device_access_revoked".into())
        );
        fs::remove_file(path).unwrap();
    }
    #[cfg(windows)]
    #[test]
    #[ignore = "requires running development Ory/API and interactive system browser"]
    fn live_ory_browser_restart_refresh_logout() {
        let file = PathBuf::from(
            std::env::var("KOUJOU_NATIVE_TEST_FILE").expect("explicit fixture path required"),
        );
        let api = "https://localhost:8080/";
        let auth = Auth::new(api, file.clone()).unwrap();
        auth.login(open_browser).unwrap();
        let profile = auth.request("/auth/me", "GET", Value::Null).unwrap();
        assert!(profile["user"]["id"].is_string());
        let raw = fs::read(&file).unwrap();
        assert!(!String::from_utf8_lossy(&raw).contains("refresh_token"));
        drop(auth);
        let restarted = Auth::new(api, file.clone()).unwrap();
        assert_eq!(
            restarted.request("/auth/me", "GET", Value::Null).unwrap()["user"]["id"],
            profile["user"]["id"]
        );
        assert_eq!(restarted.logout().unwrap()["oauth_tokens"], "revoked");
        assert!(!file.exists());
        assert!(restarted.request("/auth/me", "GET", Value::Null).is_err());
        let mut callback_check = None;
        let result = restarted.login_until(
            |authorize| {
                let authorize = Url::parse(authorize).unwrap();
                let values: std::collections::HashMap<_, _> = authorize.query_pairs().collect();
                let callback = Url::parse(values.get("redirect_uri").unwrap()).unwrap();
                let state = values.get("state").unwrap().to_string();
                callback_check = Some(std::thread::spawn(move || {
                    use std::io::Read;
                    for route in [
                        format!("{}?state=wrong&code=synthetic", callback.path()),
                        format!("/wrong-path?state={state}&code=synthetic"),
                    ] {
                        let mut stream =
                            TcpStream::connect(("127.0.0.1", callback.port().unwrap())).unwrap();
                        write!(
                            stream,
                            "GET {route} HTTP/1.1\r\nHost: 127.0.0.1\r\nConnection: close\r\n\r\n"
                        )
                        .unwrap();
                        let mut response = String::new();
                        stream.read_to_string(&mut response).unwrap();
                        assert!(response.starts_with("HTTP/1.1 400"));
                    }
                }));
                Ok(())
            },
            Duration::from_millis(500),
        );
        callback_check.unwrap().join().unwrap();
        assert_eq!(result, Err("login_expired".into()));
        assert!(!file.exists());
    }
}
