use rig_core::http_client::{
    self as http, Bytes, HttpClientExt, LazyBody, MultipartForm, Request, Response,
    StreamingResponse,
};
use serde_json::{Value, json};
use std::sync::{Arc, Mutex};
use tauri::Url;

#[derive(Clone)]
pub(crate) struct Transport {
    client: reqwest::Client,
    target: Url,
    observed: Arc<Mutex<Option<Value>>>,
}
impl Transport {
    pub(crate) fn new(client: reqwest::Client, target: Url) -> Self {
        Self {
            client,
            target,
            observed: Arc::default(),
        }
    }
    pub(crate) fn observed(&self) -> Option<Value> {
        self.observed.lock().ok()?.clone()
    }
    async fn execute(&self, request: Request<Bytes>) -> http::Result<Response<Bytes>> {
        let (parts, body) = request.into_parts();
        let url = Url::parse(&parts.uri.to_string()).map_err(http::Error::instance)?;
        let response = self
            .client
            .request(parts.method, url.as_str())
            .headers(parts.headers)
            .body(body)
            .send()
            .await
            .map_err(http::Error::instance)?;
        let status = response.status();
        let headers = response.headers().clone();
        let bytes = response.bytes().await.map_err(http::Error::instance)?;
        // OAuth replies use this transport too. Capture only the model origin/path,
        // so a failed refresh can never become a model's saved raw response.
        if url.origin() == self.target.origin() && url.path().starts_with(self.target.path()) {
            let text = String::from_utf8_lossy(&bytes);
            let raw =
                serde_json::from_str::<Value>(&text).unwrap_or(Value::String(text.into_owned()));
            *self.observed.lock().map_err(|_| {
                http::Error::instance(std::io::Error::other("model response lock"))
            })? = Some(json!({"status":status.as_u16(),"body":raw}));
        }
        if !status.is_success() {
            return Err(http::Error::non_success_with_details(
                status,
                headers,
                String::from_utf8_lossy(&bytes).into_owned(),
            ));
        }
        let mut response = Response::builder().status(status);
        *response.headers_mut().unwrap() = headers;
        response.body(bytes).map_err(http::Error::Protocol)
    }
}
impl HttpClientExt for Transport {
    fn send<T, U>(
        &self,
        request: Request<T>,
    ) -> impl Future<Output = http::Result<Response<LazyBody<U>>>> + Send + 'static
    where
        T: Into<Bytes> + Send,
        U: From<Bytes> + Send + 'static,
    {
        let request = request.map(Into::into);
        let this = self.clone();
        async move {
            let response = this.execute(request).await?;
            let (parts, bytes) = response.into_parts();
            let body: LazyBody<U> = Box::pin(async move { Ok(U::from(bytes)) });
            Ok(Response::from_parts(parts, body))
        }
    }
    fn send_multipart<U>(
        &self,
        _: Request<MultipartForm>,
    ) -> impl Future<Output = http::Result<Response<LazyBody<U>>>> + Send + 'static
    where
        U: From<Bytes> + Send + 'static,
    {
        async {
            Err(http::Error::instance(std::io::Error::other(
                "Context selection accepts JSON requests only",
            )))
        }
    }
    fn send_streaming<T>(
        &self,
        request: Request<T>,
    ) -> impl Future<Output = http::Result<StreamingResponse>> + Send
    where
        T: Into<Bytes> + Send,
    {
        let request = request.map(Into::into);
        let this = self.clone();
        async move {
            let response = this.execute(request).await?;
            let (parts, bytes) = response.into_parts();
            let stream: http::BoxedStream =
                Box::pin(futures_util::stream::once(async move { Ok(bytes) }));
            Ok(Response::from_parts(parts, stream))
        }
    }
}
