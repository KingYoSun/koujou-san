use crate::auth::{hex_name, protect, unprotect};
use chatgpt::auth::{AuthContext, DeviceCodeHandler, FileTokenStore, StoreLock, TokenStore};
use rig_core::{
    error::BoxError,
    http_client::HttpClientExt,
    providers::{chatgpt, copilot},
    wasm_compat::WasmBoxedFuture,
};
use serde_json::{Value, json};
use std::{
    fs,
    path::{Path, PathBuf},
};
use tauri::ipc::Channel;

#[derive(Clone)]
pub(super) struct Store {
    dir: PathBuf,
}
impl Store {
    pub(super) fn new(file: &Path, id: &str) -> Self {
        Self {
            dir: file.parent().unwrap().join("rig-oauth").join(hex_name(id)),
        }
    }
    fn path(&self, key: &str) -> PathBuf {
        self.dir.join(format!("{}.dpapi", hex_name(key)))
    }
    pub(super) fn present(&self, provider: &str) -> bool {
        self.path(provider).exists()
    }
}
impl TokenStore for Store {
    fn load<'a>(&'a self, key: &'a str) -> WasmBoxedFuture<'a, Result<Option<String>, BoxError>> {
        Box::pin(async move {
            let bytes = match fs::read(self.path(key)) {
                Ok(v) => v,
                Err(e) if e.kind() == std::io::ErrorKind::NotFound => return Ok(None),
                Err(e) => return Err(e.into()),
            };
            let clear = unprotect(&bytes).map_err(std::io::Error::other)?;
            Ok(Some(String::from_utf8(clear)?))
        })
    }
    fn save<'a>(
        &'a self,
        key: &'a str,
        record: &'a str,
    ) -> WasmBoxedFuture<'a, Result<(), BoxError>> {
        Box::pin(async move {
            let encrypted = protect(record.as_bytes()).map_err(std::io::Error::other)?;
            fs::create_dir_all(&self.dir)?;
            fs::write(self.path(key), encrypted)?;
            Ok(())
        })
    }
    fn delete<'a>(&'a self, key: &'a str) -> WasmBoxedFuture<'a, Result<(), BoxError>> {
        Box::pin(async move {
            match fs::remove_file(self.path(key)) {
                Ok(()) => Ok(()),
                Err(e) if e.kind() == std::io::ErrorKind::NotFound => Ok(()),
                Err(e) => Err(e.into()),
            }
        })
    }
    fn lock<'a>(&'a self, key: &'a str) -> WasmBoxedFuture<'a, Result<StoreLock, BoxError>> {
        Box::pin(async move { FileTokenStore::new(&self.dir).lock(key).await })
    }
}

pub(super) async fn context<H: HttpClientExt>(
    provider: &str,
    store: Store,
    http: &H,
) -> Result<AuthContext, String> {
    let result = match provider {
        "chatgpt" => {
            chatgpt::auth::Authenticator::new(
                chatgpt::auth::AuthSource::OAuth,
                store,
                DeviceCodeHandler::default(),
                false,
            )
            .auth_context(http)
            .await
        }
        "copilot" => {
            copilot::auth::Authenticator::new(
                copilot::auth::AuthSource::OAuth,
                store,
                DeviceCodeHandler::default(),
                false,
            )
            .auth_context(http)
            .await
        }
        _ => return Err("invalid_model_provider".into()),
    };
    result.map_err(|_| "model_sign_in_required".into())
}

pub(super) async fn sign_in<H: HttpClientExt>(
    provider: &str,
    store: Store,
    http: &H,
    channel: Channel<Value>,
) -> Result<(), String> {
    let device = DeviceCodeHandler::new({
        let channel = channel.clone();
        move |p| {
            let _ = channel.send(json!({"url":p.verification_uri,"code":p.user_code}));
            if let Ok(url) = tauri::Url::parse(&p.verification_uri) {
                if url.scheme() == "https"
                    && matches!(url.host_str(), Some("auth.openai.com" | "github.com"))
                {
                    let _ = crate::auth::open_browser(url.as_str());
                }
            }
        }
    });
    let result = match provider {
        "chatgpt" => {
            chatgpt::auth::Authenticator::new(chatgpt::auth::AuthSource::OAuth, store, device, true)
                .sign_in(
                    http,
                    chatgpt::auth::SignInMethod::Browser,
                    move |prompt| match prompt {
                        chatgpt::auth::SignInPrompt::Browser(p) => {
                            let _ = channel.send(
                                json!({"url":p.authorize_url,"browser_opened":p.browser_launched}),
                            );
                        }
                        chatgpt::auth::SignInPrompt::BrowserUnavailable(_) => {
                            let _ = channel.send(json!({"message":"browser_unavailable"}));
                        }
                    },
                )
                .await
        }
        "copilot" => {
            copilot::auth::Authenticator::new(copilot::auth::AuthSource::OAuth, store, device, true)
                .auth_context(http)
                .await
        }
        _ => return Err("invalid_model_provider".into()),
    };
    result
        .map(|_| ())
        .map_err(|_| "model_sign_in_failed".into())
}

pub(super) async fn sign_out<H: HttpClientExt>(
    provider: &str,
    store: Store,
    http: &H,
) -> Result<Value, String> {
    match provider {
        "chatgpt" => {
            let result = chatgpt::auth::Authenticator::new(
                chatgpt::auth::AuthSource::OAuth,
                store,
                DeviceCodeHandler::default(),
                false,
            )
            .sign_out(http)
            .await
            .map_err(|_| "model_sign_out_failed")?;
            let revoked = match result {
                chatgpt::auth::SignedOut::NotSignedIn => true,
                chatgpt::auth::SignedOut::Forgotten { revoked } => revoked.is_ok(),
            };
            Ok(json!({"local_deleted":true,"remote_revoked":revoked}))
        }
        "copilot" => {
            store
                .delete("copilot")
                .await
                .map_err(|_| "model_sign_out_failed")?;
            Ok(json!({"local_deleted":true,"remote_revoked":false}))
        }
        _ => Err("invalid_model_provider".into()),
    }
}

#[cfg(all(test, windows))]
mod tests {
    use super::*;
    use rig_core::http_client::{
        self as http, Bytes, LazyBody, MultipartForm, Request, Response, StreamingResponse,
    };
    use std::sync::{Arc, Mutex};
    #[derive(Clone)]
    struct OAuthHttp {
        sent: Arc<Mutex<Vec<String>>>,
        token: String,
    }
    impl HttpClientExt for OAuthHttp {
        fn send<T, U>(
            &self,
            req: Request<T>,
        ) -> impl Future<Output = http::Result<Response<LazyBody<U>>>> + Send + 'static
        where
            T: Into<Bytes> + Send,
            U: From<Bytes> + Send + 'static,
        {
            assert!(matches!(
                req.uri().host(),
                Some("auth.openai.com" | "api.github.com")
            ));
            assert!(!req.headers().contains_key("Cookie"));
            assert!(!req.headers().contains_key("X-Koujou-Operation"));
            let path = req.uri().path().to_string();
            self.sent.lock().unwrap().push(path.clone());
            let value = match path.as_str() {
                "/oauth/token" => {
                    json!({"access_token":self.token,"refresh_token":"owned-refreshed"})
                }
                "/api/accounts/oauth/revoke" => json!({}),
                "/copilot_internal/v2/token" => {
                    assert_eq!(req.uri().host(), Some("api.github.com"));
                    let expires = std::time::SystemTime::now()
                        .duration_since(std::time::UNIX_EPOCH)
                        .unwrap()
                        .as_secs()
                        + 3600;
                    json!({"token":"owned-copilot-session","expires_at":expires,"endpoints":{"api":"https://api.individual.githubcopilot.com"}})
                }
                _ => panic!("unexpected interactive/model request"),
            };
            let bytes = Bytes::from(value.to_string());
            async move {
                let body: LazyBody<U> = Box::pin(async move { Ok(U::from(bytes)) });
                Ok(Response::builder()
                    .status(200)
                    .header("Content-Type", "application/json")
                    .body(body)
                    .unwrap())
            }
        }
        fn send_multipart<U>(
            &self,
            _: Request<MultipartForm>,
        ) -> impl Future<Output = http::Result<Response<LazyBody<U>>>> + Send + 'static
        where
            U: From<Bytes> + Send + 'static,
        {
            async { panic!("no auth multipart") }
        }
        fn send_streaming<T>(
            &self,
            _: Request<T>,
        ) -> impl Future<Output = http::Result<StreamingResponse>> + Send
        where
            T: Into<Bytes> + Send,
        {
            async { panic!("no auth model stream") }
        }
    }
    #[test]
    fn sdk_refresh_uses_the_encrypted_store_and_never_starts_login_during_model_execution() {
        use base64::Engine;
        tauri::async_runtime::block_on(async {
            let root =
                std::env::temp_dir().join(format!("koujou-rig-refresh-{}", uuid::Uuid::new_v4()));
            let store = Store::new(&root.join("metadata.dpapi"), "owned");
            let expires = std::time::SystemTime::now()
                .duration_since(std::time::UNIX_EPOCH)
                .unwrap()
                .as_secs()
                + 3600;
            let claims = json!({"exp":expires,"https://api.openai.com/auth":{"chatgpt_account_id":"owned-account"}});
            let token = format!(
                "owned.{}.signature",
                base64::engine::general_purpose::URL_SAFE_NO_PAD.encode(claims.to_string())
            );
            let http = OAuthHttp {
                sent: Arc::default(),
                token: token.clone(),
            };
            store.save("chatgpt",&json!({"access_token":"expired-owned","refresh_token":"owned-refresh","expires_at":0}).to_string()).await.unwrap();
            let ctx = context("chatgpt", store.clone(), &http).await.unwrap();
            assert!(ctx.api_key.expose() == token);
            assert_eq!(ctx.account_id.as_deref(), Some("owned-account"));
            assert_eq!(http.sent.lock().unwrap().len(), 1);
            assert!(
                !fs::read(store.path("chatgpt"))
                    .unwrap()
                    .windows(token.len())
                    .any(|b| b == token.as_bytes())
            );
            context("chatgpt", store.clone(), &http).await.unwrap();
            assert_eq!(http.sent.lock().unwrap().len(), 1);
            assert_eq!(
                sign_out("chatgpt", store.clone(), &http).await.unwrap()["remote_revoked"],
                true
            );
            assert!(!store.present("chatgpt"));
            assert!(context("chatgpt", store.clone(), &http).await.is_err());
            assert_eq!(http.sent.lock().unwrap().len(), 3);
            fs::remove_file(store.dir.join("chatgpt.lock")).unwrap();
            fs::remove_dir(&store.dir).unwrap();
            fs::remove_dir(root.join("rig-oauth")).unwrap();
            fs::remove_dir(root).unwrap();
        });
    }
    #[test]
    fn copilot_exchanges_and_caches_the_owned_session_without_interactive_login_or_shared_keys() {
        tauri::async_runtime::block_on(async {
            let root =
                std::env::temp_dir().join(format!("koujou-copilot-owned-{}", uuid::Uuid::new_v4()));
            let store = Store::new(&root.join("models.dpapi"), "owned");
            let http = OAuthHttp {
                sent: Arc::default(),
                token: String::new(),
            };
            store
                .save(
                    "copilot",
                    &json!({"github_token":"owned-github-token","expires_at":0}).to_string(),
                )
                .await
                .unwrap();
            let ctx = context("copilot", store.clone(), &http).await.unwrap();
            assert_eq!(ctx.api_key.expose(), "owned-copilot-session");
            assert_eq!(
                ctx.api_base.as_deref(),
                Some("https://api.individual.githubcopilot.com")
            );
            context("copilot", store.clone(), &http).await.unwrap();
            assert_eq!(http.sent.lock().unwrap().len(), 1);
            assert_eq!(
                sign_out("copilot", store.clone(), &http).await.unwrap()["remote_revoked"],
                false
            );
            assert!(!store.present("copilot"));
            assert!(context("copilot", store.clone(), &http).await.is_err());
            assert_eq!(http.sent.lock().unwrap().len(), 1);
            fs::remove_file(store.dir.join("copilot.lock")).unwrap();
            fs::remove_dir(&store.dir).unwrap();
            fs::remove_dir(root.join("rig-oauth")).unwrap();
            fs::remove_dir(root).unwrap();
        });
    }
    #[test]
    fn tokens_are_dpapi_encrypted_and_scoped_to_the_connection_and_actor() {
        tauri::async_runtime::block_on(async {
            let root =
                std::env::temp_dir().join(format!("koujou-rig-auth-{}", uuid::Uuid::new_v4()));
            let store = Store::new(&root.join("alice/metadata.dpapi"), "owned");
            let secret = uuid::Uuid::new_v4().to_string();
            store.save("chatgpt", &secret).await.unwrap();
            assert_eq!(store.load("chatgpt").await.unwrap(), Some(secret.clone()));
            assert!(
                !fs::read(store.path("chatgpt"))
                    .unwrap()
                    .windows(secret.len())
                    .any(|b| b == secret.as_bytes())
            );
            assert!(
                Store::new(&root.join("bob/metadata.dpapi"), "owned")
                    .load("chatgpt")
                    .await
                    .unwrap()
                    .is_none()
            );
            assert!(
                Store::new(&root.join("alice/metadata.dpapi"), "another")
                    .load("chatgpt")
                    .await
                    .unwrap()
                    .is_none()
            );
            store.delete("chatgpt").await.unwrap();
            assert!(!store.present("chatgpt"));
            fs::remove_dir(&store.dir).unwrap();
            fs::remove_dir(root.join("alice/rig-oauth")).unwrap();
            fs::remove_dir(root.join("alice")).unwrap();
            fs::remove_dir(root).unwrap();
        });
    }
}
