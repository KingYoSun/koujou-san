use super::*;
pub(super) struct McpAuth(Auth);
pub(super) struct Resource {
    pub config: &'static str,
    pub scope: &'static str,
    pub me: &'static str,
}
impl Auth {
    pub(super) fn resource(&self) -> Resource {
        if self.mcp {
            Resource {
                config: "auth/mcp/config",
                scope: "openid offline_access email profile koujou.mcp",
                me: "auth/mcp/me",
            }
        } else {
            Resource {
                config: "auth/config",
                scope: "openid offline_access email profile koujou",
                me: "auth/me",
            }
        }
    }
}

pub(super) fn setup(
    app: &tauri::App,
    api: &str,
    file: &std::path::Path,
) -> Result<(), Box<dyn std::error::Error>> {
    let path = file.with_file_name(format!(
        "mcp-{}",
        file.file_name().unwrap().to_string_lossy()
    ));
    let mut auth = Auth::new(api, path).map_err(std::io::Error::other)?;
    auth.mcp = true;
    app.manage(McpAuth(auth));
    Ok(())
}
pub(super) fn logout(app: &tauri::AppHandle) -> Result<Value, String> {
    let state = app.state::<McpAuth>();
    if !state.0.file.exists()
        && state
            .0
            .tokens
            .lock()
            .map_err(|_| "auth_state_failed")?
            .access_token
            .is_empty()
    {
        return Ok(json!({"oauth_tokens":"revoked"}));
    }
    state.0.logout()
}
#[tauri::command]
pub async fn mcp_login(app: tauri::AppHandle) -> Result<(), String> {
    tauri::async_runtime::spawn_blocking(move || app.state::<McpAuth>().0.login(open_browser))
        .await
        .map_err(|_| "mcp_login_task_failed")?
}
#[tauri::command]
pub async fn mcp_status(app: tauri::AppHandle) -> Result<Value, String> {
    tauri::async_runtime::spawn_blocking(move || {
        app.state::<McpAuth>()
            .0
            .request("/auth/mcp/me", "GET", Value::Null)
    })
    .await
    .map_err(|_| "mcp_status_task_failed")?
}
#[tauri::command]
pub async fn mcp_request(
    app: tauri::AppHandle,
    workspace: String,
    operation: String,
    client: String,
    body: Value,
) -> Result<Value, String> {
    tauri::async_runtime::spawn_blocking(move || {
        let auth = app.state::<McpAuth>();
        let response = auth
            .0
            .client
            .post(auth.0.api.join("mcp").unwrap())
            .bearer_auth(auth.0.access()?)
            .header("Content-Type", "application/json")
            .header("Accept", "application/json, text/event-stream")
            .header("MCP-Protocol-Version", "2026-07-28")
            .header("Mcp-Method", body["method"].as_str().unwrap_or_default())
            .header(
                "Mcp-Name",
                body["params"]["name"].as_str().unwrap_or_default(),
            )
            .header("X-Koujou-Workspace", workspace)
            .header("X-Koujou-Operation", operation)
            .header("X-Koujou-Client", client)
            .json(&body)
            .send()
            .map_err(|_| "mcp_unavailable")?;
        let status = response.status().as_u16();
        let value = if status == 202 {
            Value::Null
        } else {
            response
                .json::<Value>()
                .map_err(|_| "invalid_mcp_response")?
        };
        Ok(json!({"status":status,"body":public_response(value)?}))
    })
    .await
    .map_err(|_| "mcp_request_task_failed")?
}

#[cfg(all(test, windows))]
mod tests {
    use super::*;
    #[test]
    #[ignore = "requires the owned client-selection API/Ory fixture"]
    fn owned_native_mcp_pkce_refresh_and_logout() {
        let api = std::env::var("KOUJOU_CLIENTOPS_NATIVE_API").expect("owned fixture API required");
        let file = std::env::temp_dir().join(format!("koujou-owned-mcp-{}.dpapi", Uuid::new_v4()));
        let mut auth = Auth::new(&api, file.clone()).unwrap();
        auth.mcp = true;
        let mut driver = None;
        auth.login_until(
            |url| {
                let client = auth.client.clone();
                let target = auth.api.join("__fixture/native-authorize").unwrap();
                let url = url.to_string();
                driver = Some(std::thread::spawn(move || {
                    assert!(
                        client
                            .post(target)
                            .json(&json!({"url":url}))
                            .send()
                            .unwrap()
                            .status()
                            .is_success()
                    );
                }));
                Ok(())
            },
            Duration::from_secs(60),
        )
        .unwrap();
        driver.unwrap().join().unwrap();
        let user = auth.request("/auth/mcp/me", "GET", Value::Null).unwrap();
        assert!(user["user"]["id"].is_string());
        let encrypted = fs::read(&file).unwrap();
        assert!(!String::from_utf8_lossy(&encrypted).contains("refresh"));
        assert_eq!(
            auth.client
                .get(auth.api.join("auth/me").unwrap())
                .bearer_auth(auth.access().unwrap())
                .send()
                .unwrap()
                .status()
                .as_u16(),
            401
        );
        drop(auth);
        let mut restarted = Auth::new(&api, file.clone()).unwrap();
        restarted.mcp = true;
        assert_eq!(
            restarted
                .request("/auth/mcp/me", "GET", Value::Null)
                .unwrap()["user"]["id"],
            user["user"]["id"]
        );
        assert_eq!(restarted.logout().unwrap()["oauth_tokens"], "revoked");
        assert!(!file.exists());
    }
}
