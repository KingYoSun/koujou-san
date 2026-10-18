#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    tauri::Builder::default()
        .setup(connection::setup)
        .invoke_handler(tauri::generate_handler![
            connection::connection_status,
            connection::connection_configure,
            auth::login,
            auth::logout,
            auth::api_request,
            auth::open_account,
            auth::open_github,
            auth::device_identifier,
            auth::device_enroll,
            auth::device_contact,
            auth::mcp::mcp_login,
            auth::mcp::mcp_status,
            auth::mcp::mcp_request,
            model_connection::model_connection_load,
            model_connection::model_connection_save,
            model_connection::model_connection_request,
            model_connection::model_connection_sign_in,
            model_connection::model_connection_sign_out,
            model_connection::model_connection_open_authorization,
            sync_store::sync_load,
            sync_store::sync_save,
            sync_store::sync_page,
            sync_store::sync_read,
            sync_store::sync_remove,
            sync_store::sync_purge_actor,
            sync_store::run_answer_draft,
            sync_store::sync_request,
            run_export::export_run,
            run_export::export_definition,
            run_export::export_workflow,
            runner::runner_configure,
            runner::runner_contact,
            runner::prepare_run,
            runner::runner_execution_save,
            runner::runner_start,
            runner::runner_status,
            runner::reconcile_run,
            runner::runner_stop_accepting
        ])
        .run(tauri::generate_context!())
        .expect("failed to run koujou-san desktop");
}
pub mod auth;
mod connection;
mod run_export;
mod sync_store;

mod model_connection;
mod runner;

pub fn run_background_runner() {
    runner::background_entry();
}
