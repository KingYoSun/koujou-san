use crate::auth::{Auth, protect, unprotect};
use serde::{Deserialize, Serialize};
use serde_json::Value;
use std::{fs, sync::Mutex};
use tauri::Manager;

static CACHE_LOCK: Mutex<()> = Mutex::new(());

#[tauri::command]
pub async fn sync_request(
    app: tauri::AppHandle,
    path: String,
    method: String,
    body: Value,
) -> Result<Value, String> {
    tauri::async_runtime::spawn_blocking(move || {
        let response = app.state::<Auth>().request_response(&path, &method, body)?;
        let status = response.status().as_u16();
        let value = response
            .json::<Value>()
            .map_err(|_| "invalid_sync_response")?;
        let value = crate::auth::public_response(value)?;
        Ok(serde_json::json!({"status":status,"body":value}))
    })
    .await
    .map_err(|_| "sync_task_failed")?
}
#[derive(Serialize, Deserialize)]
struct Saved {
    api: String,
    user: String,
    workspace: String,
    payload: Value,
}
fn load(auth: &Auth, user: &str, workspace: &str) -> Result<Option<Value>, String> {
    let _lock = CACHE_LOCK.lock().map_err(|_| "sync_storage_failed")?;
    let (path, api) = auth.sync_binding(user, workspace);
    if !path.exists() {
        return Ok(None);
    }
    let encrypted = fs::read(path).map_err(|_| "sync_storage_failed")?;
    let value: Saved =
        serde_json::from_slice(&unprotect(&encrypted)?).map_err(|_| "sync_storage_invalid")?;
    if value.api != api || value.user != user || value.workspace != workspace {
        return Err("sync_storage_binding_mismatch".into());
    }
    Ok(Some(value.payload))
}
fn save(auth: &Auth, user: &str, workspace: &str, payload: Value) -> Result<(), String> {
    let _lock = CACHE_LOCK.lock().map_err(|_| "sync_storage_failed")?;
    let (path, api) = auth.sync_binding(user, workspace);
    fs::create_dir_all(path.parent().unwrap()).map_err(|_| "sync_storage_failed")?;
    let raw = serde_json::to_vec(&Saved {
        api,
        user: user.into(),
        workspace: workspace.into(),
        payload,
    })
    .map_err(|_| "sync_storage_invalid")?;
    let temporary = path.with_extension("tmp");
    fs::write(&temporary, protect(&raw)?).map_err(|_| "sync_storage_failed")?;
    fs::rename(temporary, path).map_err(|_| "sync_storage_failed".into())
}
fn remove(auth: &Auth, user: &str, workspace: &str) -> Result<(), String> {
    let _lock = CACHE_LOCK.lock().map_err(|_| "sync_storage_failed")?;
    let (path, _) = auth.sync_binding(user, workspace);
    if path.exists() {
        fs::remove_file(path).map_err(|_| "sync_storage_failed")?;
    }
    Ok(())
}
fn purge(auth: &Auth, user: &str) -> Result<(), String> {
    let _lock = CACHE_LOCK.lock().map_err(|_| "sync_storage_failed")?;
    let (path, _) = auth.sync_binding(user, "");
    let root = path.parent().unwrap();
    if !root.exists() {
        return Ok(());
    }
    let prefix = path
        .file_name()
        .unwrap()
        .to_string_lossy()
        .split('-')
        .next()
        .unwrap()
        .to_owned()
        + "-";
    for entry in fs::read_dir(root).map_err(|_| "sync_storage_failed")? {
        let entry = entry.map_err(|_| "sync_storage_failed")?;
        if entry.file_name().to_string_lossy().starts_with(&prefix) {
            fs::remove_file(entry.path()).map_err(|_| "sync_storage_failed")?;
        }
    }
    Ok(())
}
#[tauri::command]
pub async fn sync_load(
    app: tauri::AppHandle,
    user: String,
    workspace: String,
) -> Result<Option<Value>, String> {
    tauri::async_runtime::spawn_blocking(move || load(&app.state::<Auth>(), &user, &workspace))
        .await
        .map_err(|_| "sync_task_failed")?
}
#[tauri::command]
pub async fn sync_save(
    app: tauri::AppHandle,
    user: String,
    workspace: String,
    payload: Value,
) -> Result<(), String> {
    tauri::async_runtime::spawn_blocking(move || {
        save(&app.state::<Auth>(), &user, &workspace, payload)
    })
    .await
    .map_err(|_| "sync_task_failed")?
}
#[tauri::command]
pub async fn sync_remove(
    app: tauri::AppHandle,
    user: String,
    workspace: String,
) -> Result<(), String> {
    tauri::async_runtime::spawn_blocking(move || remove(&app.state::<Auth>(), &user, &workspace))
        .await
        .map_err(|_| "sync_task_failed")?
}
#[tauri::command]
pub async fn sync_purge_actor(app: tauri::AppHandle, user: String) -> Result<(), String> {
    tauri::async_runtime::spawn_blocking(move || purge(&app.state::<Auth>(), &user))
        .await
        .map_err(|_| "sync_task_failed")?
}

#[cfg(all(test, windows))]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    #[ignore = "requires a running local API/Ory, explicit fixture OAuth file and closed desktop app"]
    fn live_sync_fixture_prepare() {
        let file = std::path::PathBuf::from(
            std::env::var("KOUJOU_NATIVE_TEST_FILE")
                .expect("select the existing development OAuth file"),
        );
        let output = std::path::PathBuf::from(
            std::env::var("KOUJOU_SYNC_FIXTURE_FILE")
                .expect("select an owned fixture metadata path"),
        );
        assert!(!output.exists(), "fixture metadata already exists");
        let auth = Auth::new("https://localhost:8080/", file).unwrap();
        let profile = auth
            .request("/auth/me", "GET", Value::Null)
            .expect("fixture identity required");
        let user = profile["user"]["id"].as_str().unwrap();
        let workspace = format!("000-sync-{}", uuid::Uuid::new_v4());
        let operation = || uuid::Uuid::new_v4().to_string();
        auth.request(&format!("/access/workspaces/{workspace}/create"), "POST", json!({"operation_id":operation(),"expected_version":"0","id":workspace,"name":"同期受入検証"})).expect("fixture Workspace creation failed");
        let write = |kind: &str, id: &str, value: Value| {
            auth.request(
                &format!("/contexts/workspaces/{workspace}/{kind}/{id}"),
                "POST",
                json!({"operation_id":operation(),"expected_version":"0","value":value}),
            )
            .expect("fixture Context write failed")
        };
        write(
            "sentence",
            "sync-text",
            json!({"kind":"sentence","name":"同期する本文","text":"初期の共有本文。"}),
        );
        write(
            "sentence",
            "sync-extra",
            json!({"kind":"sentence","name":"追加の文","text":"更新前の文。"}),
        );
        write(
            "scope",
            "sync-scope",
            json!({"kind":"issue","name":"同期の確認"}),
        );
        write(
            "membership",
            "sync-membership",
            json!({"scope":{"kind":"issue","id":"sync-scope"},"context":{"kind":"sentence","id":"sync-text"},"present":true}),
        );
        let invite = auth.request(&format!("/access/workspaces/{workspace}/invite-create"), "POST", json!({"operation_id":operation(),"expected_version":"0","id":operation(),"role":"admin"})).expect("fixture invitation failed");
        let config = auth.request("/auth/config", "GET", Value::Null).unwrap();
        let link = format!(
            "{}/#join={}",
            config["site_url"].as_str().unwrap(),
            invite["invitation"].as_str().unwrap()
        );
        let device = auth.request(&format!("/access/workspaces/{workspace}/device-register"), "POST", json!({"operation_id":operation(),"expected_version":"0","id":"sync-report-device","identifier":"sync-report-fixture","name":"同期報告の検証端末"})).expect("fixture device failed");
        let credential = device["credential"].as_str().unwrap();
        let device_version = device["result"]["data"]["version"].as_str().unwrap();
        auth.request(&format!("/access/workspaces/{workspace}/device-update"), "POST", json!({"operation_id":operation(),"expected_version":device_version,"id":"sync-report-device","active":true,"can_report":true})).expect("fixture report grant failed");
        let secret = output.with_extension("dpapi");
        fs::create_dir_all(output.parent().unwrap()).unwrap();
        fs::write(&secret, protect(credential.as_bytes()).unwrap()).unwrap();
        fs::write(output,serde_json::to_vec(&json!({"workspace":workspace,"native_user":user,"invite_url":link,"device_id":"sync-report-device","target_id":"run:sync-fixture"})).unwrap()).unwrap();
        println!("Local sync UI fixture prepared; private credential is DPAPI-protected.");
    }

    #[test]
    #[ignore = "publishes one declared sequence to the owned local sync fixture; no OAuth file is used"]
    fn live_sync_fixture_report() {
        let output = std::path::PathBuf::from(
            std::env::var("KOUJOU_SYNC_FIXTURE_FILE").expect("select owned fixture metadata"),
        );
        let fixture: Value = serde_json::from_slice(&fs::read(&output).unwrap()).unwrap();
        let sequence =
            std::env::var("KOUJOU_SYNC_REPORT_SEQUENCE").expect("select sequence 1, 3, or 2");
        assert!(matches!(sequence.as_str(), "1" | "2" | "3"));
        let token = String::from_utf8(
            unprotect(&fs::read(output.with_extension("dpapi")).unwrap()).unwrap(),
        )
        .unwrap();
        let cert =
            fs::read(std::env::var("KOUJOU_DEV_CA_FILE").expect("select development CA")).unwrap();
        let client = reqwest::blocking::Client::builder()
            .add_root_certificate(reqwest::Certificate::from_pem(&cert).unwrap())
            .redirect(reqwest::redirect::Policy::none())
            .build()
            .unwrap();
        let url = format!(
            "https://localhost:8080/sync/workspaces/{}/reports",
            fixture["workspace"].as_str().unwrap()
        );
        let response=client.post(url).bearer_auth(token).json(&json!({"event_id":format!("sync-report-{sequence}"),"target_id":fixture["target_id"],"sequence":sequence,"kind":if sequence=="3"{"completion_observed"}else{"output_observed"},"artifact_id":"sync-artifact","artifact_revision":"result-v1"})).send().expect("local report connection failed");
        assert!(response.status().is_success(), "local report rejected");
        let value: Value = response.json().unwrap();
        assert!(value.get("data").is_some() && value.get("error").is_none());
        println!("Owned fixture report accepted; execution result remains unconfirmed.");
    }
    #[test]
    fn scoped_dpapi_cache_restart_replace_and_purge() {
        let root = std::env::temp_dir().join(format!("koujou-sync-{}", uuid::Uuid::new_v4()));
        let auth = Auth::new("https://localhost:8080/", root.join("oauth.dpapi")).unwrap();
        let first =
            json!({"cache":{"s":"confirmed"},"drafts":{"s":"unsent draft"},"position":"opaque"});
        save(&auth, "alice", "one", first.clone()).unwrap();
        save(&auth, "alice", "two", first.clone()).unwrap();
        save(&auth, "bob", "one", first.clone()).unwrap();
        let (path, _) = auth.sync_binding("alice", "one");
        assert!(!String::from_utf8_lossy(&fs::read(&path).unwrap()).contains("unsent draft"));
        let restarted = Auth::new("https://localhost:8080/", root.join("oauth.dpapi")).unwrap();
        assert_eq!(
            load(&restarted, "alice", "one").unwrap(),
            Some(first.clone())
        );
        let changed = json!({"drafts":{"s":"new local draft"}});
        save(&restarted, "alice", "one", changed.clone()).unwrap();
        assert_eq!(load(&restarted, "alice", "one").unwrap(), Some(changed));
        let other = Auth::new("https://localhost:8081/", root.join("oauth.dpapi")).unwrap();
        assert!(load(&other, "alice", "one").unwrap().is_none());
        remove(&restarted, "alice", "one").unwrap();
        assert!(load(&restarted, "alice", "one").unwrap().is_none());
        assert_eq!(
            load(&restarted, "alice", "two").unwrap(),
            Some(first.clone())
        );
        purge(&restarted, "alice").unwrap();
        assert!(load(&restarted, "alice", "two").unwrap().is_none());
        assert_eq!(load(&restarted, "bob", "one").unwrap(), Some(first));
        purge(&restarted, "bob").unwrap();
        fs::remove_dir(root.join("sync")).unwrap();
        fs::remove_dir(root).unwrap();
    }
}
