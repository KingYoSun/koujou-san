use super::*;
use std::collections::BTreeSet;
use std::io::{BufRead, BufReader, Read, Write};
use std::net::TcpListener;
use std::sync::{
    Arc, Mutex,
    atomic::{AtomicBool, Ordering},
};
use std::thread::{self, JoinHandle};
use std::time::Duration;

// Only a per-process loopback exporter is configured. No prompt, account,
// endpoint, tool output or other telemetry attribute is retained or forwarded.
pub(super) struct Capture {
    endpoint: String,
    models: Arc<Mutex<BTreeMap<String, BTreeSet<String>>>>,
    stop: Arc<AtomicBool>,
    task: Option<JoinHandle<()>>,
}

fn extract(value: &Value) -> Vec<(String, String)> {
    let mut out = vec![];
    for resource in value["resourceLogs"].as_array().into_iter().flatten() {
        for scope in resource["scopeLogs"].as_array().into_iter().flatten() {
            for record in scope["logRecords"].as_array().into_iter().flatten() {
                let mut session = None;
                let mut model = None;
                for attr in record["attributes"].as_array().into_iter().flatten() {
                    match attr["key"].as_str() {
                        Some("conversation.id") => session = attr["value"]["stringValue"].as_str(),
                        Some("model") => model = attr["value"]["stringValue"].as_str(),
                        _ => {}
                    }
                }
                if let (Some(session), Some(model)) = (session, model) {
                    if !session.is_empty() && !model.is_empty() && model != "auto" {
                        out.push((session.into(), model.into()));
                    }
                }
            }
        }
    }
    out
}

impl Capture {
    pub(super) fn start() -> Result<Self, String> {
        let listener = TcpListener::bind("127.0.0.1:0").map_err(|_| "model_capture_unavailable")?;
        listener
            .set_nonblocking(true)
            .map_err(|_| "model_capture_unavailable")?;
        let address = listener
            .local_addr()
            .map_err(|_| "model_capture_unavailable")?;
        let path = format!("/usage/{}", Uuid::new_v4());
        let endpoint = format!("http://{address}{path}");
        let models = Arc::new(Mutex::new(BTreeMap::<String, BTreeSet<String>>::new()));
        let stop = Arc::new(AtomicBool::new(false));
        let data = models.clone();
        let done = stop.clone();
        let task = thread::spawn(move || {
            while !done.load(Ordering::Relaxed) {
                let (mut stream, _) = match listener.accept() {
                    Ok(v) => v,
                    Err(e) if e.kind() == std::io::ErrorKind::WouldBlock => {
                        thread::sleep(Duration::from_millis(10));
                        continue;
                    }
                    Err(_) => break,
                };
                let _ = stream.set_read_timeout(Some(Duration::from_secs(1)));
                let _ = stream.set_write_timeout(Some(Duration::from_secs(1)));
                let result = (|| -> Result<(), String> {
                    let mut input = BufReader::new(&mut stream);
                    let mut line = String::new();
                    input.read_line(&mut line).map_err(|_| "invalid_capture")?;
                    if line.trim() != format!("POST {path} HTTP/1.1") {
                        return Err("invalid_capture".into());
                    }
                    let mut length = None;
                    loop {
                        line.clear();
                        if input.read_line(&mut line).map_err(|_| "invalid_capture")? == 0 {
                            return Err("invalid_capture".into());
                        }
                        if line == "\r\n" {
                            break;
                        }
                        if let Some((name, value)) = line.split_once(':') {
                            if name.eq_ignore_ascii_case("content-length") {
                                length = value.trim().parse::<u64>().ok();
                            }
                        }
                    }
                    let length = length.ok_or("invalid_capture")?;
                    let value: Value = serde_json::from_reader(input.take(length))
                        .map_err(|_| "invalid_capture")?;
                    if let Ok(mut values) = data.lock() {
                        for (session, model) in extract(&value) {
                            values.entry(session).or_default().insert(model);
                        }
                    }
                    Ok(())
                })();
                let response = if result.is_ok() {
                    "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: 2\r\nConnection: close\r\n\r\n{}"
                } else {
                    "HTTP/1.1 400 Bad Request\r\nContent-Length: 0\r\nConnection: close\r\n\r\n"
                };
                let _ = stream.write_all(response.as_bytes());
            }
        });
        Ok(Self {
            endpoint,
            models,
            stop,
            task: Some(task),
        })
    }
    pub(super) fn config(&self) -> String {
        format!(
            "otel.exporter={{otlp-http={{endpoint=\"{}\",protocol=\"json\"}}}}",
            self.endpoint
        )
    }
    pub(super) fn model(&self, session: &str) -> Option<String> {
        let values = self.models.lock().ok()?;
        let models = values.get(session)?;
        if models.len() != 1 {
            return None;
        }
        models.iter().next().cloned()
    }
}
impl Drop for Capture {
    fn drop(&mut self) {
        self.stop.store(true, Ordering::Relaxed);
        if let Some(task) = self.task.take() {
            let _ = task.join();
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn local_export_retains_only_session_and_model() {
        let capture = Capture::start().unwrap();
        let body = json!({"resourceLogs":[{"scopeLogs":[{"logRecords":[{"attributes":[{"key":"conversation.id","value":{"stringValue":"owned"}},{"key":"model","value":{"stringValue":"model-fixed"}},{"key":"user.email","value":{"stringValue":"SYNTHETIC_PRIVATE_EMAIL"}},{"key":"prompt","value":{"stringValue":"SYNTHETIC_SECRET"}}]}]}]}]});
        let client = reqwest::blocking::Client::new();
        assert!(
            client
                .post(&capture.endpoint)
                .json(&body)
                .send()
                .unwrap()
                .status()
                .is_success()
        );
        assert_eq!(capture.model("owned").as_deref(), Some("model-fixed"));
        assert_eq!(capture.model("other"), None);
        let retained = format!("{:?}", capture.models.lock().unwrap());
        assert!(!retained.contains("SYNTHETIC"));
        assert!(
            client
                .post(format!("{}/wrong", capture.endpoint))
                .json(&body)
                .send()
                .unwrap()
                .status()
                .is_client_error()
        );
    }
}
