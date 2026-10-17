use crate::auth::{Auth, protect, unprotect};
use reqwest::blocking::Client;
use serde::{Deserialize, Serialize};
use serde_json::{Value, json};
use std::{
    fs,
    path::{Path, PathBuf},
    sync::Mutex,
    time::Duration,
};
use tauri::{Manager, Url};
use uuid::Uuid;

static CONNECTION_LOCK: Mutex<()> = Mutex::new(());
#[derive(Clone, Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
pub struct Profile {
    id: String,
    name: String,
    endpoint: String,
    model: String,
    allowed_models: Vec<String>,
    #[serde(default = "bearer")]
    auth: String,
    #[serde(default)]
    revision: String,
    #[serde(default)]
    contract: String,
    #[serde(default)]
    prices: Value,
    #[serde(default)]
    valuation: String,
    #[serde(default)]
    cost: Value,
}
fn bearer() -> String {
    "bearer".into()
}
#[derive(Deserialize, Serialize)]
struct Stored {
    profile: Profile,
    key: String,
}
fn endpoint(value: &str) -> Result<Url, String> {
    let u = Url::parse(value).map_err(|_| "invalid_model_endpoint")?;
    let local = matches!(
        u.host_str(),
        Some("localhost" | "127.0.0.1" | "[::1]" | "::1")
    );
    if (u.scheme() != "https" && !(u.scheme() == "http" && local))
        || u.host_str().is_none()
        || !u.username().is_empty()
        || u.password().is_some()
        || u.query().is_some()
        || u.fragment().is_some()
    {
        return Err("invalid_model_endpoint".into());
    };
    Ok(u)
}
fn binding(auth: &Auth) -> Result<PathBuf, String> {
    let user = auth.request("/auth/me", "GET", Value::Null)?;
    let id = user["user"]["id"]
        .as_str()
        .ok_or("authentication_required")?;
    let (path, _) = auth.sync_binding(id, "model-connections");
    Ok(path
        .parent()
        .unwrap()
        .parent()
        .unwrap()
        .join("model-connections")
        .join(path.file_name().unwrap()))
}
fn load(path: &Path) -> Result<Vec<Stored>, String> {
    if !path.exists() {
        return Ok(vec![]);
    }
    serde_json::from_slice(&unprotect(
        &fs::read(path).map_err(|_| "model_key_unavailable")?,
    )?)
    .map_err(|_| "invalid_model_storage".into())
}
fn save(path: &Path, entries: &[Stored]) -> Result<(), String> {
    let b = serde_json::to_vec(entries).map_err(|_| "invalid_model_storage")?;
    let encrypted = protect(&b)?;
    fs::create_dir_all(path.parent().unwrap()).map_err(|_| "model_storage_unavailable")?;
    fs::write(path, encrypted).map_err(|_| "model_storage_unavailable".into())
}
fn store(path: &Path, mut profile: Profile, key: String) -> Result<Profile, String> {
    endpoint(&profile.endpoint)?;
    if profile.id.is_empty()
        || profile.name.is_empty()
        || profile.model.is_empty()
        || profile.model == "auto"
        || profile.allowed_models.is_empty()
    {
        return Err("invalid_model_profile".into());
    }
    let mut entries = load(path)?;
    let old = entries.iter().find(|e| e.profile.id == profile.id);
    // An edited destination needs its own key; never reuse another origin's key.
    if profile.auth != "bearer" && profile.auth != "none" {
        return Err("invalid_model_profile".into());
    }
    let key = if profile.auth == "none" {
        String::new()
    } else if key.is_empty() {
        let old = old.ok_or("model_key_required")?;
        if old.profile.endpoint != profile.endpoint {
            return Err("model_key_required".into());
        };
        old.key.clone()
    } else {
        key
    };
    profile.revision = Uuid::new_v4().to_string();
    entries.retain(|e| e.profile.id != profile.id);
    entries.push(Stored {
        profile: profile.clone(),
        key,
    });
    save(path, &entries)?;
    Ok(profile)
}
#[tauri::command]
pub async fn model_connection_load(app: tauri::AppHandle) -> Result<Vec<Profile>, String> {
    tauri::async_runtime::spawn_blocking(move || {
        let path = binding(&app.state::<Auth>())?;
        let _guard = CONNECTION_LOCK
            .lock()
            .map_err(|_| "model_storage_unavailable")?;
        Ok(load(&path)?.into_iter().map(|e| e.profile).collect())
    })
    .await
    .map_err(|_| "model_storage_task_failed")?
}
#[tauri::command]
pub async fn model_connection_save(
    app: tauri::AppHandle,
    profile: Profile,
    key: String,
) -> Result<Profile, String> {
    tauri::async_runtime::spawn_blocking(move || {
        let path = binding(&app.state::<Auth>())?;
        let _guard = CONNECTION_LOCK
            .lock()
            .map_err(|_| "model_storage_unavailable")?;
        store(&path, profile, key)
    })
    .await
    .map_err(|_| "model_storage_task_failed")?
}
fn request(stored: &Stored, revision: &str, body: Value, timeout_ms: u64) -> Result<Value, String> {
    if revision != stored.profile.revision
        || body["model"].as_str() != Some(stored.profile.model.as_str())
        || body["stream"].as_bool() == Some(true)
        || timeout_ms == 0
    {
        return Err("model_connection_changed".into());
    }
    let mut target = endpoint(&stored.profile.endpoint)?;
    target.set_path(&format!(
        "{}/chat/completions",
        target.path().trim_end_matches('/')
    ));
    let mut builder = Client::builder()
        .redirect(reqwest::redirect::Policy::none())
        .timeout(Duration::from_millis(timeout_ms));
    if matches!(target.host_str(), Some("localhost" | "127.0.0.1")) {
        if let Ok(file) = std::env::var("KOUJOU_MODEL_CA_FILE") {
            let b = fs::read(file).map_err(|_| "invalid_model_ca")?;
            builder = builder.add_root_certificate(
                reqwest::Certificate::from_pem(&b).map_err(|_| "invalid_model_ca")?,
            )
        }
    }
    let client = builder
        .build()
        .map_err(|_| "model_connection_unavailable")?;
    let mut call = client.post(target).json(&body);
    if stored.profile.auth == "bearer" {
        call = call.bearer_auth(&stored.key)
    }
    let response = call.send().map_err(|_| "model_connection_unavailable")?;
    let status = response.status().as_u16();
    let text = response.text().map_err(|_| "invalid_model_response")?;
    let value = serde_json::from_str::<Value>(&text).unwrap_or(Value::String(text));
    Ok(json!({"status":status,"body":value}))
}
#[tauri::command]
pub async fn model_connection_request(
    app: tauri::AppHandle,
    id: String,
    revision: String,
    body: Value,
    timeout_ms: u64,
) -> Result<Value, String> {
    tauri::async_runtime::spawn_blocking(move || {
        let path = binding(&app.state::<Auth>())?;
        let stored = {
            let _guard = CONNECTION_LOCK
                .lock()
                .map_err(|_| "model_storage_unavailable")?;
            load(&path)?
                .into_iter()
                .find(|e| e.profile.id == id)
                .ok_or("model_connection_unavailable")?
        };
        request(&stored, &revision, body, timeout_ms)
    })
    .await
    .map_err(|_| "model_request_task_failed")?
}

#[cfg(test)]
mod tests {
    use super::*;
    fn profile(endpoint: &str) -> Profile {
        Profile {
            id: "owned".into(),
            name: "owned".into(),
            endpoint: endpoint.into(),
            model: "owned-model".into(),
            allowed_models: vec!["owned-model".into()],
            auth: "bearer".into(),
            revision: "v1".into(),
            contract: String::new(),
            prices: json!({}),
            valuation: "estimated".into(),
            cost: Value::Null,
        }
    }
    #[test]
    fn endpoint_does_not_carry_keys_or_plain_remote_http() {
        for bad in [
            "http://remote.example/v1",
            "https://user:secret@example.test/v1",
            "https://example.test/v1?key=value",
            "https://example.test/v1#key",
        ] {
            assert!(endpoint(bad).is_err())
        }
        assert!(endpoint("https://gateway.example.test/v1").is_ok());
        assert!(endpoint("http://127.0.0.1:2099/v1").is_ok());
    }
    #[cfg(windows)]
    #[test]
    fn encrypted_key_is_not_returned_in_metadata_or_reused_for_another_endpoint() {
        let dir = std::env::temp_dir().join(format!("koujou-model-test-{}", Uuid::new_v4()));
        let file = dir.join("owned.dpapi");
        let key = Uuid::new_v4().to_string();
        let saved = store(
            &file,
            profile("https://gateway.example.test/v1"),
            key.clone(),
        )
        .unwrap();
        let bytes = fs::read(&file).unwrap();
        assert!(!bytes.windows(key.len()).any(|b| b == key.as_bytes()));
        let entries = load(&file).unwrap();
        assert!(entries[0].key == key);
        assert!(!serde_json::to_string(&saved).unwrap().contains(&key));
        let mut changed = saved.clone();
        changed.endpoint = "https://another.example.test/v1".into();
        assert!(store(&file, changed, String::new()).is_err());
        let edited = store(&file, saved.clone(), String::new()).unwrap();
        assert!(edited.revision != saved.revision);
        fs::remove_file(file).unwrap();
        fs::remove_dir(dir).unwrap();
    }
    #[test]
    fn registered_request_sends_one_key_to_the_fixed_endpoint_without_redirect() {
        use std::{
            io::{Read, Write},
            net::TcpListener,
            thread,
        };
        let destination = TcpListener::bind("127.0.0.1:0").unwrap();
        let address = destination.local_addr().unwrap();
        let redirected = TcpListener::bind("127.0.0.1:0").unwrap();
        redirected.set_nonblocking(true).unwrap();
        let other = redirected.local_addr().unwrap();
        let key = Uuid::new_v4().to_string();
        let expected = key.clone();
        let worker = thread::spawn(move || {
            let (mut stream, _) = destination.accept().unwrap();
            stream
                .set_read_timeout(Some(Duration::from_secs(2)))
                .unwrap();
            let mut b = [0; 8192];
            let n = stream.read(&mut b).unwrap();
            let text = String::from_utf8_lossy(&b[..n]);
            assert!(text.starts_with("POST /v1/chat/completions "));
            assert!(text.contains(&expected));
            assert!(!text.contains("X-Koujou-Operation"));
            let body = "{}";
            write!(stream,"HTTP/1.1 302 Found\r\nLocation: http://{other}/stolen\r\nContent-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{body}",body.len()).unwrap()
        });
        let stored = Stored {
            profile: profile(&format!("http://{address}/v1")),
            key,
        };
        assert!(
            request(
                &stored,
                "another-version",
                json!({"model":"owned-model"}),
                2000
            )
            .is_err()
        );
        let result = request(&stored, "v1", json!({"model":"owned-model"}), 2000).unwrap();
        assert_eq!(result["status"], 302);
        worker.join().unwrap();
        assert!(redirected.accept().is_err());
    }
}
