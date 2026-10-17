use crate::auth::{Auth, protect, unprotect};
#[cfg(all(test, windows))]
#[path = "rig_live_tests.rs"]
mod live;
#[path = "rig_model.rs"]
mod rig;
#[path = "rig_auth.rs"]
mod rig_auth;
use serde::{Deserialize, Serialize};
use serde_json::Value;
#[cfg(test)]
use serde_json::json;
use std::{
    fs,
    path::{Path, PathBuf},
    sync::Mutex,
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
    #[serde(default = "compatible")]
    provider: String,
    #[serde(default)]
    credentials_saved: bool,
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
fn compatible() -> String {
    "compatible".into()
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
    if profile.provider == "compatible" {
        if profile.auth != "bearer" && profile.auth != "none" {
            return Err("invalid_model_profile".into());
        }
    } else {
        let expected = match profile.provider.as_str() {
            "chatgpt" => "https://chatgpt.com/backend-api/codex",
            "copilot" => "https://api.githubcopilot.com",
            _ => return Err("invalid_model_provider".into()),
        };
        if profile.auth != "oauth" || profile.endpoint.trim_end_matches('/') != expected {
            return Err("invalid_model_profile".into());
        }
    }
    let key = if profile.auth == "none" || profile.auth == "oauth" {
        String::new()
    } else if key.is_empty() {
        let old = old.ok_or("model_key_required")?;
        if old.profile.endpoint != profile.endpoint
            || old.profile.auth != "bearer"
            || old.key.is_empty()
        {
            return Err("model_key_required".into());
        };
        old.key.clone()
    } else {
        key
    };
    profile.revision = Uuid::new_v4().to_string();
    profile.credentials_saved = profile.auth == "oauth"
        && rig_auth::Store::new(path, &profile.id).present(&profile.provider);
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
        Ok(load(&path)?
            .into_iter()
            .map(|e| {
                let mut p = e.profile;
                p.credentials_saved =
                    p.auth == "oauth" && rig_auth::Store::new(&path, &p.id).present(&p.provider);
                p
            })
            .collect())
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
fn request(
    stored: &Stored,
    revision: &str,
    body: Value,
    timeout_ms: u64,
    path: &Path,
) -> Result<Value, String> {
    if revision != stored.profile.revision
        || body["model"].as_str() != Some(stored.profile.model.as_str())
        || body["stream"].as_bool() == Some(true)
        || timeout_ms == 0
    {
        return Err("model_connection_changed".into());
    }
    tauri::async_runtime::block_on(rig::request(
        stored,
        body,
        timeout_ms,
        rig_auth::Store::new(path, &stored.profile.id),
    ))
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
        let started = std::time::Instant::now();
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
        let remaining = timeout_ms.saturating_sub(started.elapsed().as_millis() as u64);
        request(&stored, &revision, body, remaining, &path)
    })
    .await
    .map_err(|_| "model_request_task_failed")?
}

fn current(app: &tauri::AppHandle, id: &str, revision: &str) -> Result<(Stored, PathBuf), String> {
    let path = binding(&app.state::<Auth>())?;
    let _guard = CONNECTION_LOCK
        .lock()
        .map_err(|_| "model_storage_unavailable")?;
    let entry = load(&path)?
        .into_iter()
        .find(|e| e.profile.id == id && e.profile.revision == revision)
        .ok_or("model_connection_changed")?;
    Ok((entry, path))
}
#[tauri::command]
pub async fn model_connection_sign_in(
    app: tauri::AppHandle,
    id: String,
    revision: String,
    prompt: tauri::ipc::Channel<Value>,
) -> Result<Profile, String> {
    tauri::async_runtime::spawn_blocking(move || {
        let (entry, path) = current(&app, &id, &revision)?;
        let http = rig::http(&entry.profile, 15 * 60 * 1000)?;
        tauri::async_runtime::block_on(rig_auth::sign_in(
            &entry.profile.provider,
            rig_auth::Store::new(&path, &id),
            &http,
            prompt,
        ))?;
        let (_, now) = current(&app, &id, &revision)?;
        if now != path {
            return Err("model_connection_changed".into());
        }
        let _guard = CONNECTION_LOCK
            .lock()
            .map_err(|_| "model_storage_unavailable")?;
        let mut entries = load(&path)?;
        let saved = entries
            .iter_mut()
            .find(|e| e.profile.id == id && e.profile.revision == revision)
            .ok_or("model_connection_changed")?;
        saved.profile.revision = Uuid::new_v4().to_string();
        saved.profile.credentials_saved = true;
        let profile = saved.profile.clone();
        save(&path, &entries)?;
        Ok(profile)
    })
    .await
    .map_err(|_| "model_sign_in_task_failed")?
}
#[tauri::command]
pub async fn model_connection_sign_out(
    app: tauri::AppHandle,
    id: String,
    revision: String,
) -> Result<Value, String> {
    tauri::async_runtime::spawn_blocking(move || {
        let (entry, path) = current(&app, &id, &revision)?;
        let http = rig::http(&entry.profile, 30000)?;
        tauri::async_runtime::block_on(rig_auth::sign_out(
            &entry.profile.provider,
            rig_auth::Store::new(&path, &id),
            &http,
        ))
    })
    .await
    .map_err(|_| "model_sign_out_task_failed")?
}

#[tauri::command]
pub fn model_connection_open_authorization(url: String) -> Result<(), String> {
    let parsed = Url::parse(&url).map_err(|_| "invalid_model_authorization_url")?;
    if parsed.scheme() != "https"
        || !parsed.username().is_empty()
        || parsed.password().is_some()
        || !matches!(parsed.host_str(), Some("auth.openai.com" | "github.com"))
    {
        return Err("invalid_model_authorization_url".into());
    }
    crate::auth::open_browser(parsed.as_str())
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::time::Duration;
    fn profile(endpoint: &str) -> Profile {
        Profile {
            id: "owned".into(),
            name: "owned".into(),
            endpoint: endpoint.into(),
            model: "owned-model".into(),
            allowed_models: vec!["owned-model".into()],
            auth: "bearer".into(),
            provider: compatible(),
            credentials_saved: false,
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
                2000,
                &std::env::temp_dir().join("koujou-owned-model-test.dpapi")
            )
            .is_err()
        );
        let result = request(
            &stored,
            "v1",
            json!({"model":"owned-model","messages":[{"role":"user","content":"owned"}]}),
            2000,
            &std::env::temp_dir().join("koujou-owned-model-test.dpapi"),
        )
        .unwrap();
        assert_eq!(result["status"], 302);
        worker.join().unwrap();
        assert!(redirected.accept().is_err());
    }
}
