#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    tauri::Builder::default()
        .setup(auth::setup)
        .invoke_handler(tauri::generate_handler![
            auth::login,
            auth::logout,
            auth::api_request,
            auth::open_account,
            auth::device_identifier,
            auth::device_enroll,
            auth::device_contact,
            sync_store::sync_load,
            sync_store::sync_save,
            sync_store::sync_remove,
            sync_store::sync_purge_actor,
            sync_store::sync_request
        ])
        .run(tauri::generate_context!())
        .expect("failed to run koujou-san desktop");
}
pub mod auth;
mod sync_store;
