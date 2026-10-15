#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    tauri::Builder::default()
        .setup(auth::setup)
        .invoke_handler(tauri::generate_handler![
            auth::login,
            auth::logout,
            auth::api_request,
            auth::open_account
        ])
        .run(tauri::generate_context!())
        .expect("failed to run koujou-san desktop");
}
pub mod auth;
