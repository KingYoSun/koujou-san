use super::*;
use rig_core::http_client::{Bytes, HttpClientExt, Request};
use std::{
    io::{Read, Write},
    net::{TcpListener, TcpStream},
    path::Path,
    thread,
};

fn incoming(stream: &mut TcpStream) -> (String, Value) {
    stream
        .set_read_timeout(Some(Duration::from_secs(5)))
        .unwrap();
    let mut bytes = vec![];
    let mut part = [0; 4096];
    loop {
        let n = stream.read(&mut part).unwrap();
        assert!(n > 0);
        bytes.extend_from_slice(&part[..n]);
        if let Some(i) = bytes.windows(4).position(|p| p == b"\r\n\r\n") {
            let headers = String::from_utf8(bytes[..i].to_vec()).unwrap();
            let length: usize = headers
                .lines()
                .find_map(|l| {
                    l.to_lowercase()
                        .strip_prefix("content-length:")
                        .map(|v| v.trim().parse().unwrap())
                })
                .unwrap_or(0);
            if bytes.len() >= i + 4 + length {
                let body = if length == 0 {
                    Value::Null
                } else {
                    serde_json::from_slice(&bytes[i + 4..i + 4 + length]).unwrap()
                };
                return (headers, body);
            }
        }
    }
}
fn answer(stream: &mut TcpStream, kind: &str, body: &str) {
    write!(stream,"HTTP/1.1 200 OK\r\nContent-Type: {kind}\r\nContent-Length: {}\r\nX-Request-Id: owned-http-id\r\nConnection: close\r\n\r\n{body}",body.len()).unwrap();
}
#[test]
fn each_sending_intent_performs_one_sdk_turn_and_preserves_raw_history_for_the_next_tool_result() {
    let listener = TcpListener::bind("127.0.0.1:0").unwrap();
    let address = listener.local_addr().unwrap();
    let worker = thread::spawn(move || {
        for turn in 0..2 {
            let (mut stream, _) = listener.accept().unwrap();
            let (headers, body) = incoming(&mut stream);
            assert!(headers.starts_with("POST /v1/chat/completions "));
            assert!(
                headers
                    .to_lowercase()
                    .contains("authorization: bearer owned-sdk")
            );
            assert!(!headers.to_lowercase().contains("cookie:"));
            assert!(!headers.to_lowercase().contains("x-koujou"));
            assert!(!body.to_string().contains("rig_message"));
            assert!(!body.to_string().contains("rig_connection"));
            let message = if turn == 0 {
                json!({"role":"assistant","content":null,"tool_calls":[{"id":"owned-call","type":"function","function":{"name":"context_search","arguments":"{\"limit\":1}"}}]})
            } else {
                assert!(
                    body["messages"]
                        .as_array()
                        .unwrap()
                        .iter()
                        .any(|m| m["role"] == "tool" && m["tool_call_id"] == "owned-call")
                );
                json!({"role":"assistant","content":"{\"items\":[],\"questions\":[],\"unknown\":[]}"})
            };
            answer(&mut stream,"application/json",&json!({"id":"owned-reply","object":"chat.completion","created":0,"model":"owned-model","choices":[{"index":0,"message":message,"finish_reason":if turn==0 {"tool_calls"} else {"stop"}}],"usage":{"prompt_tokens":10,"completion_tokens":2},"vendor_field":"preserved-owned"}).to_string());
        }
    });
    tauri::async_runtime::block_on(async {
        let stored:Stored=serde_json::from_value(json!({"profile":{"id":"owned","name":"owned","endpoint":format!("http://{address}/v1"),"model":"owned-model","allowed_models":["owned-model"],"auth":"bearer","revision":"owned-version"},"key":"owned-sdk"})).unwrap();
        let user = json!({"role":"user","content":"Select owned fixed Context"});
        let mut body = json!({"model":"owned-model","messages":[user],"tools":[{"type":"function","function":{"name":"context_search","description":"owned read","parameters":{"type":"object","properties":{"limit":{"type":"integer"}},"required":["limit"],"additionalProperties":false}}}]});
        let store = super::super::rig_auth::Store::new(Path::new("owned-metadata.dpapi"), "owned");
        let first = request(&stored, body.clone(), 5000, store.clone())
            .await
            .unwrap();
        assert_eq!(first["body"]["vendor_field"], "preserved-owned");
        assert!(!first["body"]["choices"][0]["message"]["rig_message"].is_null());
        body["messages"].as_array_mut().unwrap().extend([
            first["body"]["choices"][0]["message"].clone(),
            json!({"role":"tool","tool_call_id":"owned-call","content":"owned candidate page"}),
        ]);
        let second = request(&stored, body, 5000, store).await.unwrap();
        assert_eq!(second["body"]["vendor_field"], "preserved-owned");
        assert_eq!(second["body"]["rig_request_id"], "owned-http-id");
        assert_eq!(second["body"]["usage"]["prompt_tokens"], 10);
    });
    worker.join().unwrap();
}

#[test]
fn raw_capture_keeps_the_original_sse_but_never_captures_an_oauth_reply() {
    let listener = TcpListener::bind("127.0.0.1:0").unwrap();
    let address = listener.local_addr().unwrap();
    let sse = "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"owned\":true}}\n\n";
    let worker = thread::spawn(move || {
        for turn in 0..2 {
            let (mut stream, _) = listener.accept().unwrap();
            let (headers, _) = incoming(&mut stream);
            if turn == 0 {
                assert!(headers.starts_with("POST /oauth/token "));
                answer(
                    &mut stream,
                    "application/json",
                    "{\"access_token\":\"OWNED_AUTH_ONLY\"}",
                );
            } else {
                answer(&mut stream, "text/event-stream", sse);
            }
        }
    });
    tauri::async_runtime::block_on(async {
        let http = Transport::new(
            reqwest::Client::new(),
            tauri::Url::parse(&format!("http://{address}/model")).unwrap(),
        );
        http.send::<_, Vec<u8>>(
            Request::post(format!("http://{address}/oauth/token"))
                .body(Bytes::new())
                .unwrap(),
        )
        .await
        .unwrap();
        assert!(http.observed().is_none());
        let response = http
            .send_streaming(
                Request::post(format!("http://{address}/model/responses"))
                    .body(Bytes::new())
                    .unwrap(),
            )
            .await
            .unwrap();
        use futures_util::StreamExt;
        let mut stream = response.into_body();
        assert_eq!(
            stream.next().await.unwrap().unwrap().as_ref(),
            sse.as_bytes()
        );
        assert_eq!(http.observed().unwrap()["body"], sse);
    });
    worker.join().unwrap();
}

#[cfg(windows)]
#[test]
fn subscription_responses_sse_decodes_text_usage_and_preserves_unmodeled_raw_fields() {
    subscription_response_case(false)
}
#[cfg(windows)]
#[test]
fn subscription_token_limit_is_not_changed_to_a_complete_selection() {
    subscription_response_case(true)
}
#[cfg(windows)]
fn subscription_response_case(incomplete: bool) {
    let listener = TcpListener::bind("127.0.0.1:0").unwrap();
    let address = listener.local_addr().unwrap();
    let content = "{\"items\":[],\"questions\":[],\"unknown\":[]}";
    let mut response = json!({"id":"resp-owned","object":"response","created_at":0,"status":"completed","model":"gpt-5.4","output":[{"id":"msg-owned","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":content,"annotations":[]}]}],"usage":{"input_tokens":12,"output_tokens":4,"total_tokens":16,"input_tokens_details":{"cached_tokens":2},"output_tokens_details":{"reasoning_tokens":1}},"vendor_extension":"OWNED_UNMODELED"});
    if incomplete {
        response["status"] = json!("incomplete");
        response["incomplete_details"] = json!({"reason":"max_output_tokens"});
    }
    let event = if incomplete {
        "response.incomplete"
    } else {
        "response.completed"
    };
    let sse = format!(
        "event: {event}\ndata: {}\n\n",
        json!({"type":event,"response":response})
    );
    let original = sse.clone();
    let worker = thread::spawn(move || {
        let (mut stream, _) = listener.accept().unwrap();
        let (headers, body) = incoming(&mut stream);
        assert!(headers.starts_with("POST /responses "));
        assert!(
            headers
                .to_lowercase()
                .contains("authorization: bearer owned-monthly-token")
        );
        assert!(!headers.to_lowercase().contains("x-koujou"));
        assert!(!headers.to_lowercase().contains("cookie:"));
        assert_eq!(body["stream"], true);
        assert_eq!(body["store"], false);
        answer(&mut stream, "text/event-stream", &sse);
    });
    tauri::async_runtime::block_on(async {
        use rig_core::providers::chatgpt::auth::TokenStore;
        let root = std::env::temp_dir().join(format!("koujou-rig-sse-{}", uuid::Uuid::new_v4()));
        let store = super::super::rig_auth::Store::new(&root.join("models.dpapi"), "owned");
        let expires = std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .unwrap()
            .as_secs()
            + 3600;
        store.save("chatgpt",&json!({"access_token":"owned-monthly-token","expires_at":expires,"account_id":"owned-account"}).to_string()).await.unwrap();
        let stored:Stored=serde_json::from_value(json!({"profile":{"id":"owned","name":"owned","provider":"chatgpt","auth":"oauth","endpoint":format!("http://{address}"),"model":"gpt-5.4","allowed_models":["gpt-5.4"],"revision":"owned-version"},"key":""})).unwrap();
        let result=request(&stored,json!({"model":"gpt-5.4","messages":[{"role":"user","content":"owned structured selection"}]}),5000,store.clone()).await.unwrap();
        assert_eq!(
            result["body"]["choices"][0]["finish_reason"],
            if incomplete { "length" } else { "stop" }
        );
        assert_eq!(result["body"]["model"], "gpt-5.4");
        assert_eq!(result["body"]["choices"][0]["message"]["content"], content);
        assert_eq!(result["body"]["usage"]["prompt_tokens"], 12);
        assert_eq!(
            result["body"]["usage"]["prompt_tokens_details"]["cached_tokens"],
            2
        );
        assert_eq!(result["body"]["provider_raw"], original);
        store.delete("chatgpt").await.unwrap();
        let directory = &store.dir;
        fs::remove_file(directory.join("chatgpt.lock")).unwrap();
        fs::remove_dir(directory).unwrap();
        fs::remove_dir(root.join("rig-oauth")).unwrap();
        fs::remove_dir(root).unwrap();
    });
    worker.join().unwrap();
}
