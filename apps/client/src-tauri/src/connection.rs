use crate::auth::{self, Auth};
use serde::{Deserialize, Serialize};
use std::{fs, path::PathBuf, sync::Mutex};
use tauri::Manager;

struct Connection {
    root: PathBuf,
    gate: Mutex<()>,
}

#[derive(Serialize, Deserialize)]
pub struct Profile {
    api: Option<String>,
}

fn read(root: &std::path::Path, supplied: Option<String>) -> Result<Profile, String> {
    if supplied.is_some() {
        return Ok(Profile { api: supplied });
    }
    let file = root.join("connection.json");
    if !file.exists() {
        return Ok(Profile { api: None });
    }
    serde_json::from_slice(&fs::read(file).map_err(|_| "connection_unavailable")?)
        .map_err(|_| "invalid_connection".to_string())
}

pub fn setup(app: &mut tauri::App) -> Result<(), Box<dyn std::error::Error>> {
    let root = app.path().app_local_data_dir()?;
    let profile = read(&root, std::env::var("KOUJOU_PUBLIC_API_URL").ok())?;
    if let Some(api) = &profile.api {
        auth::initialize(app.handle(), api)?;
    }
    app.manage(Connection {
        root,
        gate: Mutex::new(()),
    });
    Ok(())
}

#[tauri::command]
pub fn connection_status(app: tauri::AppHandle) -> Result<Profile, String> {
    read(
        &app.state::<Connection>().root,
        std::env::var("KOUJOU_PUBLIC_API_URL").ok(),
    )
}

#[tauri::command]
pub async fn connection_configure(app: tauri::AppHandle, api: String) -> Result<Profile, String> {
    tauri::async_runtime::spawn_blocking(move || {
        let state = app.state::<Connection>();
        let _guard = state.gate.lock().map_err(|_| "connection_busy")?;
        if app.try_state::<Auth>().is_some() {
            return Err("connection_already_initialized".into());
        }
        let api = api.trim().to_string();
        let auth = Auth::new(&api, state.root.join("unused"))?;
        auth.verify_connection()?;
        let profile = Profile {
            api: Some(api.clone()),
        };
        fs::create_dir_all(&state.root).map_err(|_| "connection_save_failed")?;
        fs::write(
            state.root.join("connection.json"),
            serde_json::to_vec(&profile).unwrap(),
        )
        .map_err(|_| "connection_save_failed")?;
        auth::initialize(&app, &api)?;
        Ok(profile)
    })
    .await
    .map_err(|_| "connection_task_failed")?
}

#[test]
fn first_start_and_saved_origin_remain_separate_from_credentials() {
    let dir = std::env::temp_dir().join(format!("koujou-connection-{}", uuid::Uuid::new_v4()));
    assert!(read(&dir, None).unwrap().api.is_none());
    fs::create_dir_all(&dir).unwrap();
    let profile = Profile {
        api: Some("https://example.test/".into()),
    };
    fs::write(
        dir.join("connection.json"),
        serde_json::to_vec(&profile).unwrap(),
    )
    .unwrap();
    assert_eq!(read(&dir, None).unwrap().api, profile.api);
    assert_eq!(
        read(&dir, Some("https://localhost:8080/".into()))
            .unwrap()
            .api
            .as_deref(),
        Some("https://localhost:8080/")
    );
    assert_eq!(fs::read_dir(&dir).unwrap().count(), 1);
    fs::remove_file(dir.join("connection.json")).unwrap();
    fs::remove_dir(dir).unwrap();
}

#[test]
fn server_origin_rejects_password_even_without_username() {
    assert!(Auth::new("https://:secret@example.test/", PathBuf::from("unused")).is_err());
}
