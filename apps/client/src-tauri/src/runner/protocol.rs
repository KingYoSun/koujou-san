use super::*;
use base64::{Engine, engine::general_purpose::STANDARD};
use sha2::{Digest, Sha256};

// All bytes stay in the encrypted local journal. Provider control/initialization
// records are represented by a fixed, explicitly unfetched reference in shared output.
pub(super) fn frames(
    protocol: &str,
    device: &str,
    run: &str,
    position: i64,
    channel: &str,
    source: &str,
    bytes: &[u8],
) -> Vec<Value> {
    let value: Value = serde_json::from_slice(bytes).unwrap_or(Value::Null);
    let private = match protocol {
        "codex-jsonl" => {
            channel == "stderr"
                || value["item"]["type"] == "error"
                || !matches!(
                    value["type"].as_str(),
                    Some("item.started" | "item.updated" | "item.completed")
                )
        }
        "claude-jsonl" => {
            channel == "stderr"
                || !matches!(
                    value["type"].as_str(),
                    Some("assistant" | "user" | "result")
                )
        }
        _ => false,
    };
    let mut result = vec![];
    let output = if private {
        let digest = format!("{:x}", Sha256::digest(bytes));
        let key = format!("{:x}", Sha256::digest(run.as_bytes()));
        json!({"type":"output","channel":channel,"source":"cli-local-diagnostics","artifacts":[{"uri":format!("runner-log://{device}/{key}/{position}"),"revision":digest,"digest":digest,"range":format!("record:{position}"),"delivery":"reference","state":"unfetched"}]})
    } else {
        let raw = match std::str::from_utf8(bytes) {
            Ok(s) => json!(s),
            Err(_) => json!({"encoding":"base64","data":STANDARD.encode(bytes)}),
        };
        json!({"type":"output","channel":channel,"source":source,"value":raw})
    };
    result.push(output);
    if protocol == "jsonl-v1" && channel == "stdout" {
        let e = &value["koujou_event"];
        if e["type"] == "pause" {
            result.push(json!({"type":"pause","pause":e["pause"]}));
        } else if e["type"] == "resumed" {
            result.push(
                json!({"type":"resumed","pause_id":e["pause_id"],"response_id":e["response_id"]}),
            );
        }
    }
    result
}

pub(super) fn result(protocol: &str, bytes: &[u8], structured: bool) -> Option<Value> {
    let value: Value = serde_json::from_slice(bytes).ok()?;
    if protocol == "jsonl-v1" && value["koujou_event"]["type"] == "result" {
        return value["koujou_event"].get("report").cloned();
    }
    let raw = if protocol == "codex-jsonl"
        && value["type"] == "item.completed"
        && value["item"]["type"] == "agent_message"
    {
        value["item"].get("text")
    } else if protocol == "claude-jsonl" && value["type"] == "result" {
        if let Some(output) = value.get("structured_output") {
            return Some(output.clone());
        };
        value.get("result")
    } else {
        None
    }?;
    if structured {
        raw.as_str()
            .and_then(|s| serde_json::from_str(s).ok())
            .or_else(|| Some(raw.clone()))
    } else {
        Some(raw.clone())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn original_output_and_explicit_protocol_have_different_jobs() {
        let ordinary = b"Do you need a reply?\n";
        let plain = frames(
            "plain", "device", "run:one", 1, "stdout", "fixture", ordinary,
        );
        assert_eq!(plain.len(), 1);
        assert_eq!(plain[0]["value"], "Do you need a reply?\n");
        let pause=b"{\"koujou_event\":{\"type\":\"pause\",\"pause\":{\"id\":\"one\",\"reason\":\"question\",\"request\":\"choice?\"}}}\n";
        let parsed = frames(
            "jsonl-v1", "device", "run:one", 2, "stdout", "fixture", pause,
        );
        assert_eq!(parsed.len(), 2);
        assert_eq!(parsed[0]["value"], String::from_utf8_lossy(pause).as_ref());
        assert_eq!(parsed[1]["pause"]["id"], "one");
        let local = b"{\"type\":\"system\",\"cwd\":\"C:/private-local-path\"}\n";
        let filtered = frames(
            "claude-jsonl",
            "device",
            "run:one",
            3,
            "stdout",
            "fixture",
            local,
        );
        assert!(!filtered[0].to_string().contains("private-local-path"));
        assert_eq!(filtered[0]["artifacts"][0]["state"], "unfetched");
        let warning = br#"{"type":"item.completed","item":{"type":"error","message":"user C:/private-local-path/config.toml: ignored setting"}}"#;
        let diagnostic = frames(
            "codex-jsonl",
            "device",
            "run:one",
            4,
            "stdout",
            "fixture",
            warning,
        );
        assert!(!diagnostic[0].to_string().contains("private-local-path"));
        assert_eq!(diagnostic[0]["artifacts"][0]["state"], "unfetched");
        let binary = frames(
            "plain",
            "device",
            "run:one",
            4,
            "stderr",
            "fixture",
            &[0xff, 0x00],
        );
        assert_eq!(binary[0]["value"]["encoding"], "base64");
    }
}
