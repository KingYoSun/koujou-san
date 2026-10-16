#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    tauri::Builder::default()
        .setup(auth::setup)
        .invoke_handler(tauri::generate_handler![
            auth::login,
            auth::logout,
            auth::api_request,
            auth::open_account,
            auth::open_github,
            auth::device_identifier,
            auth::device_enroll,
            auth::device_contact,
            sync_store::sync_load,
            sync_store::sync_save,
            sync_store::sync_page,
            sync_store::sync_read,
            sync_store::sync_remove,
            sync_store::sync_purge_actor,
            sync_store::sync_request,
            run_export::export_run,
            runner::runner_configure,
            runner::runner_contact,
            runner::prepare_run
        ])
        .run(tauri::generate_context!())
        .expect("failed to run koujou-san desktop");
}
pub mod auth;
mod run_export;
mod sync_store;

mod runner;
