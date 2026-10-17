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
fn storage_error(_: impl std::fmt::Debug) -> String {
    "sync_storage_failed".into()
}
fn encrypted(api: &str, user: &str, workspace: &str, value: Value) -> Result<Vec<u8>, String> {
    protect(
        &serde_json::to_vec(&Saved {
            api: api.into(),
            user: user.into(),
            workspace: workspace.into(),
            payload: value,
        })
        .map_err(storage_error)?,
    )
}
fn decoded(api: &str, user: &str, workspace: &str, raw: Vec<u8>) -> Result<Value, String> {
    let saved: Saved = serde_json::from_slice(&unprotect(&raw)?).map_err(storage_error)?;
    if saved.api != api || saved.user != user || saved.workspace != workspace {
        return Err("sync_storage_binding_mismatch".into());
    }
    Ok(saved.payload)
}
fn open(auth: &Auth, user: &str, workspace: &str) -> Result<rusqlite::Connection, String> {
    use rusqlite::{OptionalExtension, params};
    let (legacy, api) = auth.sync_binding(user, workspace);
    fs::create_dir_all(legacy.parent().unwrap()).map_err(storage_error)?;
    let mut db =
        rusqlite::Connection::open(legacy.with_extension("sqlite")).map_err(storage_error)?;
    db.execute_batch("PRAGMA secure_delete=ON; CREATE TABLE IF NOT EXISTS meta(key TEXT PRIMARY KEY,value BLOB NOT NULL); CREATE TABLE IF NOT EXISTS records(key TEXT PRIMARY KEY,payload BLOB NOT NULL,dirty INTEGER NOT NULL); CREATE INDEX IF NOT EXISTS record_drafts ON records(dirty,key); CREATE TABLE IF NOT EXISTS answers(key TEXT PRIMARY KEY,payload BLOB NOT NULL);").map_err(storage_error)?;
    let initialized = db
        .query_row("SELECT 1 FROM meta WHERE key='format'", [], |_| Ok(true))
        .optional()
        .map_err(storage_error)?
        .unwrap_or(false);
    if !initialized {
        let tx = db.transaction().map_err(storage_error)?;
        if legacy.exists() {
            let mut value = decoded(
                &api,
                user,
                workspace,
                fs::read(&legacy).map_err(storage_error)?,
            )?;
            let header = value.as_object_mut().ok_or("sync_storage_invalid")?;
            let items = header.remove("items").ok_or("sync_storage_invalid")?;
            let drafts = header.remove("drafts").ok_or("sync_storage_invalid")?;
            let receipts = header.remove("receipts").ok_or("sync_storage_invalid")?;
            let maps = [items, drafts, receipts];
            let mut keys = std::collections::BTreeSet::new();
            for map in &maps {
                keys.extend(
                    map.as_object()
                        .ok_or("sync_storage_invalid")?
                        .keys()
                        .cloned(),
                );
            }
            for key in keys {
                let entry = serde_json::json!({"key": key, "item": maps[0].get(&key), "draft": maps[1].get(&key), "receipt": maps[2].get(&key)});
                tx.execute(
                    "INSERT INTO records VALUES(?1,?2,?3)",
                    params![
                        key,
                        encrypted(&api, user, workspace, entry)?,
                        i64::from(maps[1].get(&key).is_some())
                    ],
                )
                .map_err(storage_error)?;
            }
            tx.execute(
                "INSERT INTO meta VALUES('header',?1)",
                [encrypted(&api, user, workspace, value)?],
            )
            .map_err(storage_error)?;
        }
        tx.execute("INSERT INTO meta VALUES('format',X'02')", [])
            .map_err(storage_error)?;
        tx.commit().map_err(storage_error)?;
    }
    // A crash after commit can only leave a redundant legacy file. Never import
    // it again over newer drafts. Old and new applications must not be mixed.
    if legacy.exists() {
        fs::remove_file(legacy).map_err(storage_error)?;
    }
    Ok(db)
}
fn load(auth: &Auth, user: &str, workspace: &str) -> Result<Option<Value>, String> {
    use rusqlite::OptionalExtension;
    let _lock = CACHE_LOCK.lock().map_err(storage_error)?;
    let db = open(auth, user, workspace)?;
    let (_, api) = auth.sync_binding(user, workspace);
    let raw = db
        .query_row("SELECT value FROM meta WHERE key='header'", [], |row| {
            row.get::<_, Vec<u8>>(0)
        })
        .optional()
        .map_err(storage_error)?;
    raw.map(|bytes| decoded(&api, user, workspace, bytes))
        .transpose()
}
fn answer_draft(
    auth: &Auth,
    user: &str,
    workspace: &str,
    key: &str,
    action: &str,
    value: Value,
) -> Result<Option<Value>, String> {
    use rusqlite::{OptionalExtension, params};
    if user.is_empty() || workspace.is_empty() || key.is_empty() {
        return Err("invalid_answer_draft".into());
    }
    let _lock = CACHE_LOCK.lock().map_err(storage_error)?;
    let db = open(auth, user, workspace)?;
    let (_, api) = auth.sync_binding(user, workspace);
    match action {
        "read" => {
            let data: Option<Vec<u8>> = db
                .query_row("SELECT payload FROM answers WHERE key=?1", [key], |r| {
                    r.get(0)
                })
                .optional()
                .map_err(storage_error)?;
            data.map(|raw| decoded(&api, user, workspace, raw))
                .transpose()
        }
        "save" => {
            db.execute("INSERT INTO answers VALUES(?1,?2) ON CONFLICT(key) DO UPDATE SET payload=excluded.payload",params![key,encrypted(&api,user,workspace,value)?]).map_err(storage_error)?;
            Ok(None)
        }
        "remove" => {
            db.execute("DELETE FROM answers WHERE key=?1", [key])
                .map_err(storage_error)?;
            Ok(None)
        }
        _ => Err("invalid_answer_draft".into()),
    }
}
#[tauri::command]
pub async fn run_answer_draft(
    app: tauri::AppHandle,
    user: String,
    workspace: String,
    key: String,
    action: String,
    value: Value,
) -> Result<Option<Value>, String> {
    tauri::async_runtime::spawn_blocking(move || {
        answer_draft(
            &app.state::<Auth>(),
            &user,
            &workspace,
            &key,
            &action,
            value,
        )
    })
    .await
    .map_err(|_| "sync_task_failed")?
}
fn save(
    auth: &Auth,
    user: &str,
    workspace: &str,
    header: Value,
    entries: Vec<Value>,
) -> Result<(), String> {
    use rusqlite::params;
    let _lock = CACHE_LOCK.lock().map_err(storage_error)?;
    let mut db = open(auth, user, workspace)?;
    let (_, api) = auth.sync_binding(user, workspace);
    let tx = db.transaction().map_err(storage_error)?;
    tx.execute(
        "INSERT OR REPLACE INTO meta VALUES('header',?1)",
        [encrypted(&api, user, workspace, header)?],
    )
    .map_err(storage_error)?;
    for entry in entries {
        let key = entry["key"].as_str().ok_or("sync_storage_invalid")?;
        if ["item", "draft", "receipt"]
            .iter()
            .any(|field| !entry[*field].is_null())
        {
            tx.execute(
                "INSERT OR REPLACE INTO records VALUES(?1,?2,?3)",
                params![
                    key,
                    encrypted(&api, user, workspace, entry.clone())?,
                    i64::from(!entry["draft"].is_null())
                ],
            )
            .map_err(storage_error)?;
        } else {
            tx.execute("DELETE FROM records WHERE key=?1", [key])
                .map_err(storage_error)?;
        }
    }
    tx.commit().map_err(storage_error)
}
fn page(
    auth: &Auth,
    user: &str,
    workspace: &str,
    after: &str,
    drafts_only: bool,
) -> Result<Value, String> {
    let _lock = CACHE_LOCK.lock().map_err(storage_error)?;
    let db = open(auth, user, workspace)?;
    let (_, api) = auth.sync_binding(user, workspace);
    let sql = if drafts_only {
        "SELECT payload FROM records WHERE dirty=1 AND key>?1 ORDER BY key LIMIT 101"
    } else {
        "SELECT payload FROM records WHERE key>?1 ORDER BY key LIMIT 101"
    };
    let mut query = db.prepare(sql).map_err(storage_error)?;
    let rows = query
        .query_map([after], |row| row.get::<_, Vec<u8>>(0))
        .map_err(storage_error)?;
    let mut entries = Vec::new();
    for row in rows {
        entries.push(decoded(&api, user, workspace, row.map_err(storage_error)?)?);
    }
    let next = if entries.len() > 100 {
        Some(entries[99]["key"].clone())
    } else {
        None
    };
    entries.truncate(100);
    Ok(serde_json::json!({"entries":entries,"next":next}))
}
fn read(auth: &Auth, user: &str, workspace: &str, key: &str) -> Result<Option<Value>, String> {
    use rusqlite::OptionalExtension;
    let _lock = CACHE_LOCK.lock().map_err(storage_error)?;
    let db = open(auth, user, workspace)?;
    let (_, api) = auth.sync_binding(user, workspace);
    let raw = db
        .query_row("SELECT payload FROM records WHERE key=?1", [key], |row| {
            row.get::<_, Vec<u8>>(0)
        })
        .optional()
        .map_err(storage_error)?;
    raw.map(|bytes| decoded(&api, user, workspace, bytes))
        .transpose()
}
fn remove(auth: &Auth, user: &str, workspace: &str) -> Result<(), String> {
    let _lock = CACHE_LOCK.lock().map_err(|_| "sync_storage_failed")?;
    let (path, _) = auth.sync_binding(user, workspace);
    for target in [path.with_extension("sqlite"), path] {
        if target.exists() {
            fs::remove_file(target).map_err(storage_error)?;
        }
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
    header: Value,
    entries: Vec<Value>,
) -> Result<(), String> {
    tauri::async_runtime::spawn_blocking(move || {
        save(&app.state::<Auth>(), &user, &workspace, header, entries)
    })
    .await
    .map_err(|_| "sync_task_failed")?
}
#[tauri::command]
pub async fn sync_page(
    app: tauri::AppHandle,
    user: String,
    workspace: String,
    after: String,
    drafts_only: bool,
) -> Result<Value, String> {
    tauri::async_runtime::spawn_blocking(move || {
        page(&app.state::<Auth>(), &user, &workspace, &after, drafts_only)
    })
    .await
    .map_err(storage_error)?
}
#[tauri::command]
pub async fn sync_read(
    app: tauri::AppHandle,
    user: String,
    workspace: String,
    key: String,
) -> Result<Option<Value>, String> {
    tauri::async_runtime::spawn_blocking(move || {
        read(&app.state::<Auth>(), &user, &workspace, &key)
    })
    .await
    .map_err(storage_error)?
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
    fn scoped_dpapi_records_migrate_restart_page_replace_and_purge() {
        let root = std::env::temp_dir().join(format!("koujou-sync-{}", uuid::Uuid::new_v4()));
        let auth = Auth::new("https://localhost:8080/", root.join("oauth.dpapi")).unwrap();
        let mut old = json!({"items":{},"drafts":{},"receipts":{},"position":"opaque","confirmedAt":null,"nextCursor":null,"pages":1,"available":false});
        for i in 0..205 {
            let key = format!("sentence:s{i:03}");
            old["items"][&key] = json!({"record":{"ref":{"kind":"sentence","id":format!("s{i:03}"),"revision":"old"},"value":{"text":"cached"}},"editable":true});
            old["drafts"][&key] = json!({"base":old["items"][&key],"value":{"text":"unsent draft"},"pending":{"operation_id":format!("op-{i}"),"expected_version":"old","value":{"text":"unsent draft"}},"latest":{"record":{"ref":{"revision":"new"}}},"error":"unconfirmed"});
            old["receipts"][&key] = json!({"operationID":format!("prior-{i}"),"revision":"old"});
        }
        let (legacy, api) = auth.sync_binding("alice", "one");
        fs::create_dir_all(legacy.parent().unwrap()).unwrap();
        fs::write(
            &legacy,
            encrypted(&api, "alice", "one", old.clone()).unwrap(),
        )
        .unwrap();
        assert_eq!(
            load(&auth, "alice", "one").unwrap().unwrap()["position"],
            "opaque"
        );
        assert!(!legacy.exists());
        assert_eq!(
            page(&auth, "alice", "one", "", true).unwrap()["entries"]
                .as_array()
                .unwrap()
                .len(),
            100
        );
        assert_eq!(
            page(&auth, "alice", "one", "sentence:s099", true).unwrap()["next"],
            "sentence:s199"
        );
        assert_eq!(
            page(&auth, "alice", "one", "sentence:s199", true).unwrap()["entries"]
                .as_array()
                .unwrap()
                .len(),
            5
        );
        let entry = read(&auth, "alice", "one", "sentence:s204")
            .unwrap()
            .unwrap();
        assert_eq!(entry["draft"], old["drafts"]["sentence:s204"]);
        assert_eq!(entry["receipt"], old["receipts"]["sentence:s204"]);
        assert!(
            !String::from_utf8_lossy(&fs::read(legacy.with_extension("sqlite")).unwrap())
                .contains("unsent draft")
        );
        let restarted = Auth::new("https://localhost:8080/", root.join("oauth.dpapi")).unwrap();
        let answer = json!({"pauseVersion":"fixed-question","value":"SYNTHETIC_PRIVATE_ANSWER","json":false});
        answer_draft(
            &auth,
            "alice",
            "one",
            "run-pause:one:first",
            "save",
            answer.clone(),
        )
        .unwrap();
        assert_eq!(
            answer_draft(
                &restarted,
                "alice",
                "one",
                "run-pause:one:first",
                "read",
                Value::Null
            )
            .unwrap(),
            Some(answer)
        );
        assert!(
            answer_draft(
                &auth,
                "bob",
                "one",
                "run-pause:one:first",
                "read",
                Value::Null
            )
            .unwrap()
            .is_none()
        );
        assert!(
            !String::from_utf8_lossy(&fs::read(legacy.with_extension("sqlite")).unwrap())
                .contains("SYNTHETIC_PRIVATE_ANSWER")
        );
        let changed = json!({"key":"sentence:s204","draft":{"value":{"text":"changed"}}});
        save(
            &restarted,
            "alice",
            "one",
            json!({"position":"fresh"}),
            vec![changed.clone()],
        )
        .unwrap();
        assert_eq!(
            read(&restarted, "alice", "one", "sentence:s204").unwrap(),
            Some(changed)
        );
        assert_eq!(
            read(&restarted, "alice", "one", "sentence:s203")
                .unwrap()
                .unwrap()["draft"],
            old["drafts"]["sentence:s203"]
        );
        assert!(load(&restarted, "bob", "one").unwrap().is_none());
        let other = Auth::new("https://localhost:8081/", root.join("oauth.dpapi")).unwrap();
        assert!(
            read(&other, "alice", "one", "sentence:s204")
                .unwrap()
                .is_none()
        );
        // Invalid legacy data aborts migration without deleting the source. A
        // corrected legacy value can be imported on the next application start.
        let (broken, api) = auth.sync_binding("alice", "broken");
        fs::write(
            &broken,
            encrypted(&api, "alice", "broken", json!({"drafts":42})).unwrap(),
        )
        .unwrap();
        assert!(load(&auth, "alice", "broken").is_err());
        assert!(broken.exists());
        fs::write(&broken, encrypted(&api, "alice", "broken", old).unwrap()).unwrap();
        assert!(load(&auth, "alice", "broken").unwrap().is_some());
        remove(&auth, "alice", "one").unwrap();
        assert!(
            answer_draft(
                &restarted,
                "alice",
                "one",
                "run-pause:one:first",
                "read",
                Value::Null
            )
            .unwrap()
            .is_none()
        );
        assert!(
            read(&auth, "alice", "one", "sentence:s204")
                .unwrap()
                .is_none()
        );
        purge(&auth, "alice").unwrap();
        purge(&auth, "bob").unwrap();
        purge(&other, "alice").unwrap();
        fs::remove_dir(root.join("sync")).unwrap();
        fs::remove_dir(root).unwrap();
    }
}
