use super::*;
use crate::auth::{DeviceClient, protect, unprotect};
use journal::Journal;
use rusqlite::{OptionalExtension, params};
use sha2::{Digest, Sha256};
use std::collections::BTreeSet;
use std::time::{SystemTime, UNIX_EPOCH};

#[derive(Clone, Default, Deserialize, Serialize)]
pub(super) struct Settings {
    #[serde(default)]
    billing: String,
    #[serde(default)]
    contract: String,
    #[serde(default)]
    mode: String,
    #[serde(default)]
    model_capture: bool,
    #[serde(default)]
    non_llm: bool,
}

pub(super) fn capture(
    adapter: &execution::Adapter,
    version: &str,
) -> Option<super::model_capture::Capture> {
    if adapter.usage.model_capture
        && adapter.wsl.is_empty()
        && adapter.protocol == "codex-jsonl"
        && version == "0.162.1"
    {
        super::model_capture::Capture::start().ok()
    } else {
        None
    }
}

pub(super) struct Meter {
    run: String,
    generation: String,
    device: String,
    connection: String,
    protocol: String,
    version: String,
    settings: Settings,
    session: String,
    process: u32,
    capture: Option<super::model_capture::Capture>,
    resumed: bool,
    models: BTreeMap<String, Value>,
    messages: BTreeSet<String>,
    position: i64,
    digest: String,
    terminal: bool,
    verified: bool,
}

fn now() -> u64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap_or_default()
        .as_millis() as u64
}
fn hash(bytes: &[u8]) -> String {
    format!("{:x}", Sha256::digest(bytes))
}
fn quantity(v: Option<u64>, includes: &[&str]) -> Value {
    json!({"value":v.map(|v|v.to_string()),"state":if v.is_some(){"reported"}else{"missing"},"unit":"token","includes":includes,"reason":if v.is_some(){""}else{"provider_did_not_report"}})
}
fn quantities(value: &Value, claude: bool) -> Value {
    let names = if claude {
        [
            "inputTokens",
            "cacheReadInputTokens",
            "cacheCreationInputTokens",
            "outputTokens",
            "thinkingTokens",
        ]
    } else {
        [
            "input_tokens",
            "cached_input_tokens",
            "cache_write_input_tokens",
            "output_tokens",
            "reasoning_output_tokens",
        ]
    };
    let mut out = json!({});
    for (key, name) in ["input", "cache_read", "cache_write", "output", "reasoning"]
        .into_iter()
        .zip(names)
    {
        let includes = if key == "input" && !claude {
            vec!["cache_read", "cache_write"]
        } else if key == "output" && value[names[4]].is_u64() {
            vec!["reasoning"]
        } else {
            vec![]
        };
        out[key] = quantity(value[name].as_u64(), &includes);
    }
    out
}

// Preserve the decimal literal; provider estimates are never binary-float money.
fn decimal(value: &Value) -> Option<String> {
    let n = value.as_number()?.to_string();
    let Some((mantissa, exponent)) = n.split_once(['e', 'E']) else {
        return Some(n);
    };
    let exponent: i32 = exponent.parse().ok()?;
    let point = mantissa.find('.').unwrap_or(mantissa.len()) as i32 + exponent;
    let digits = mantissa.replace('.', "");
    if point <= 0 {
        Some(format!("0.{}{}", "0".repeat((-point) as usize), digits))
    } else if point as usize >= digits.len() {
        Some(format!(
            "{}{}",
            digits,
            "0".repeat(point as usize - digits.len())
        ))
    } else {
        Some(format!(
            "{}.{}",
            &digits[..point as usize],
            &digits[point as usize..]
        ))
    }
}

impl Meter {
    pub(super) fn new(assigned: &Value, adapter: &execution::Adapter, verified: bool) -> Self {
        let mut args = adapter.arguments.clone();
        if let Some(parameters) = assigned["spec"]["input"]["command"]["parameters"].as_array() {
            args.extend(
                parameters
                    .iter()
                    .filter_map(Value::as_str)
                    .map(str::to_owned),
            );
        }
        Self {
            run: assigned["run"]["id"].as_str().unwrap_or("").into(),
            generation: assigned["assignment"]["generation"]
                .as_str()
                .unwrap_or("")
                .into(),
            device: assigned["assignment"]["device_id"]
                .as_str()
                .unwrap_or("")
                .into(),
            connection: assigned["spec"]["input"]["command"]["target"]
                .as_str()
                .unwrap_or("")
                .into(),
            protocol: adapter.protocol.clone(),
            version: assigned["assignment"]["connection"]["version"]
                .as_str()
                .unwrap_or("")
                .into(),
            settings: adapter.usage.clone(),
            session: String::new(),
            process: 0,
            capture: None,
            resumed: args.iter().any(|a| {
                if adapter.protocol == "claude-jsonl" {
                    matches!(a.as_str(), "--resume" | "-r" | "--continue" | "-c")
                        || a.starts_with("--resume=")
                } else {
                    matches!(a.as_str(), "resume" | "fork")
                }
            }),
            models: BTreeMap::new(),
            messages: BTreeSet::new(),
            position: 0,
            digest: String::new(),
            terminal: false,
            verified,
        }
    }
    pub(super) fn process(&mut self, pid: u32) {
        self.process = pid;
    }
    fn supported(&self) -> bool {
        self.verified
            && ((self.protocol == "codex-jsonl" && self.version == "0.162.1")
                || (self.protocol == "claude-jsonl" && self.version == "2.1.300"))
    }
    fn scope(&self) -> &str {
        if self.settings.non_llm {
            "non-llm"
        } else if !self.supported() {
            "unavailable"
        } else if self.protocol == "claude-jsonl" {
            "cli-pipeline; outside helpers unavailable"
        } else {
            "main-session; child sessions unavailable"
        }
    }
    fn billing(&self) -> &str {
        match self.settings.billing.as_str() {
            "api" | "subscription" | "local" => &self.settings.billing,
            _ => "unknown",
        }
    }
    pub(super) fn observe(
        &mut self,
        journal: &Journal,
        position: i64,
        bytes: &[u8],
    ) -> Result<(), String> {
        if !self.supported() || self.settings.non_llm {
            return Ok(());
        }
        let Ok(v) = serde_json::from_slice::<Value>(bytes) else {
            return Ok(());
        };
        if let Some(session) = protocol::session(&self.protocol, bytes) {
            self.session = session;
        }
        self.position = position;
        if self.protocol == "codex-jsonl" && v["type"] == "turn.completed" {
            self.models.insert(
                String::new(),
                json!({"quantities":quantities(&v["usage"],false)}),
            );
            self.terminal = true;
        } else if self.protocol == "claude-jsonl" && v["type"] == "result" {
            if let Some(models) = v["modelUsage"].as_object() {
                self.models.clear();
                for (name, usage) in models {
                    let model = usage["canonicalModel"]
                        .as_str()
                        .filter(|m| !m.is_empty() && *m != "auto")
                        .unwrap_or(name);
                    self.models.insert(model.to_owned(),json!({"quantities":quantities(usage,true),"estimate_usd":decimal(&usage["costUSD"])}));
                }
            }
            self.terminal = true;
        } else if self.protocol == "claude-jsonl" && v["type"] == "assistant" {
            let message = &v["message"];
            let Some(id) = message["id"].as_str() else {
                return Ok(());
            };
            let Some(model) = message["model"]
                .as_str()
                .filter(|m| !m.is_empty() && *m != "auto")
            else {
                return Ok(());
            };
            if !self.messages.insert(id.into()) {
                return Ok(());
            }
            let row = self
                .models
                .entry(model.into())
                .or_insert_with(|| json!({"quantities":{}}));
            for (key, source) in [
                ("input", "input_tokens"),
                ("cache_read", "cache_read_input_tokens"),
                ("cache_write", "cache_creation_input_tokens"),
            ] {
                let previous = row["quantities"][key]["value"]
                    .as_str()
                    .and_then(|s| s.parse::<u64>().ok())
                    .unwrap_or(0);
                row["quantities"][key] =
                    quantity(message["usage"][source].as_u64().map(|n| n + previous), &[]);
            }
            row["quantities"]["output"] = quantity(None, &[]);
            row["quantities"]["reasoning"] = quantity(None, &[]);
        } else {
            return Ok(());
        }
        self.digest = hash(bytes);
        if self.settings.mode == "live" && self.protocol == "claude-jsonl" {
            self.persist(journal, false)?;
        }
        Ok(())
    }
    pub(super) fn attach_capture(&mut self, capture: Option<super::model_capture::Capture>) {
        self.capture = capture;
    }
    pub(super) fn finish(&mut self, journal: &Journal) -> Result<(), String> {
        if self.protocol == "codex-jsonl" {
            if let Some(model) = self
                .capture
                .as_ref()
                .and_then(|capture| capture.model(&self.session))
            {
                if let Some(value) = self.models.remove("") {
                    self.models.insert(model, value);
                }
            }
        }
        self.persist(journal, true)
    }
    fn persist(&self, journal: &Journal, ended: bool) -> Result<(), String> {
        let mut models = self.models.clone();
        if models.is_empty() {
            models.insert(String::new(),json!({"quantities":{"input":{"value":null,"state":if self.settings.non_llm{"not_applicable"}else{"missing"},"unit":"token","reason":if self.settings.non_llm{"non_llm"}else{"no_supported_machine_usage"}}}}));
        }
        for (model, row) in models {
            let known = self.supported() && !self.session.is_empty() && !self.resumed;
            let state = if self.settings.non_llm {
                "not_applicable"
            } else if !self.supported() || self.models.is_empty() {
                "unavailable"
            } else if !ended {
                "provisional"
            } else if !self.terminal || !known || model.is_empty() {
                "partial"
            } else {
                "final"
            };
            let reason = if self.resumed {
                "session_boundary_unconfirmed"
            } else if !self.supported() {
                "unsupported_measurement"
            } else if model.is_empty() {
                "model_not_reported"
            } else if !self.terminal {
                "result_not_observed"
            } else {
                "provider_scope_only"
            };
            let series = format!(
                "{}/{}/{}/{}",
                self.run, self.generation, self.session, model
            );
            let source = format!(
                "device:{}/{}",
                self.device,
                if self.protocol.is_empty() {
                    "plain"
                } else {
                    &self.protocol
                }
            );
            let event = hash(
                serde_json::to_string(&json!([series, row, state, self.position]))
                    .unwrap()
                    .as_bytes(),
            );
            let value = json!({"source":source,"transform_version":"runner-usage/v1","event_id":event,"series_id":series,"sequence":self.position.max(0)*2+i64::from(ended),"mode":"cumulative","coverage_group":format!("{}/{}/{}",self.run,self.generation,model),"external_request_id":series,"covers_requests":[],"coverage":if known||self.settings.non_llm{"confirmed"}else{"unknown"},"connection":self.connection,"contract":self.settings.contract,"actual_model":if model.is_empty(){None}else{Some(&model)},"attribution":{"kind":"run","id":self.run,"resource":self.run,"scopes":[]},"quantities":row["quantities"],"measurement":{"state":state,"protocol":if self.protocol.is_empty(){"plain"}else{&self.protocol},"cli_version":self.version,"session":self.session,"process_id":self.process.to_string(),"generation":self.generation,"billing":self.billing(),"scope":self.scope(),"reason":reason,"source_ref":if self.position>0 {format!("runner-log://{}/{}/{}",self.device,hash(self.run.as_bytes()),self.position)}else{String::new()},"source_digest":self.digest,"estimate_usd":if known&&self.terminal {row["estimate_usd"].clone()}else{Value::Null}}});
            let key = hash(format!("{}\0cumulative\0{}", source, series).as_bytes());
            let body = json!({"operation_id":Uuid::new_v4().to_string(),"generation":self.generation,"observed_at_ms":now(),"value":value});
            journal.queue_usage(&event, &self.run, &format!("usage:{key}"), &body)?;
        }
        Ok(())
    }
}

impl Journal {
    fn queue_usage(&self, event: &str, run: &str, id: &str, body: &Value) -> Result<(), String> {
        let data = protect(&serde_json::to_vec(body).map_err(|_| "usage_encoding_failed")?)?;
        self.db()?.execute("INSERT OR IGNORE INTO execution_usage(event_id,run_id,resource_id,payload) VALUES(?1,?2,?3,?4)",params![event,run,id,data]).map_err(|_|"usage_journal_unavailable")?;
        Ok(())
    }
}

// This queue is independent of execution_frames: a usage failure cannot block
// process output/finish or cause the process to be relaunched after recovery.
pub(super) fn drain(client: &DeviceClient, journal: &Journal) -> Result<(), String> {
    send_pending(journal, |run, body| {
        client.request(
            &[
                "runs",
                "workspaces",
                &client.binding.workspace,
                "executions",
                run,
                "usage",
            ],
            "POST",
            body,
            &[],
        )
    })
}

fn send_pending(
    journal: &Journal,
    request: impl FnOnce(&str, Value) -> Result<Value, String>,
) -> Result<(), String> {
    let db = journal.db()?;
    let after = journal
        .usage_cursor
        .load(std::sync::atomic::Ordering::Relaxed);
    // Retry the oldest unacknowledged value in each cumulative series first.
    // Other series may advance while one waits for authorization or its receipt.
    let row:Option<(i64,String,String,String,Vec<u8>)>=db.query_row("SELECT e.rowid,e.event_id,e.run_id,e.resource_id,e.payload FROM execution_usage e WHERE e.sent=0 AND e.rowid>? AND NOT EXISTS (SELECT 1 FROM execution_usage p WHERE p.resource_id=e.resource_id AND p.sent=0 AND p.rowid<e.rowid) ORDER BY e.rowid LIMIT 1",[after],|r|Ok((r.get(0)?,r.get(1)?,r.get(2)?,r.get(3)?,r.get(4)?))).optional().map_err(|_|"usage_journal_unavailable")?;
    let Some((position, event, run, id, data)) = row else {
        if after > 0 {
            journal
                .usage_cursor
                .store(0, std::sync::atomic::Ordering::Relaxed);
            return send_pending(journal, request);
        }
        return Ok(());
    };
    journal
        .usage_cursor
        .store(position, std::sync::atomic::Ordering::Relaxed);
    let mut body: Value =
        serde_json::from_slice(&unprotect(&data)?).map_err(|_| "invalid_usage_journal")?;
    if body.get("expected_version").is_none() {
        let previous: Option<String> = db
            .query_row(
                "SELECT version FROM execution_usage_versions WHERE resource_id=?",
                [&id],
                |r| r.get(0),
            )
            .optional()
            .map_err(|_| "usage_journal_unavailable")?;
        body["expected_version"] = json!(previous.unwrap_or_else(|| "0".into()));
        db.execute(
            "UPDATE execution_usage SET payload=?2 WHERE event_id=?1",
            params![event, protect(&serde_json::to_vec(&body).unwrap())?],
        )
        .map_err(|_| "usage_journal_unavailable")?;
    }
    let reply = request(&run, body)?;
    let version = reply["data"]["version"]
        .as_str()
        .ok_or("usage_not_accepted")?;
    let mut db = journal.db()?;
    let tx = db.transaction().map_err(|_| "usage_journal_unavailable")?;
    tx.execute("INSERT INTO execution_usage_versions VALUES(?1,?2) ON CONFLICT(resource_id) DO UPDATE SET version=excluded.version",params![id,version]).map_err(|_|"usage_journal_unavailable")?;
    tx.execute(
        "UPDATE execution_usage SET sent=1 WHERE event_id=?",
        [event],
    )
    .map_err(|_| "usage_journal_unavailable")?;
    tx.commit().map_err(|_| "usage_journal_unavailable".into())
}

#[cfg(all(test, windows))]
#[path = "usage_tests.rs"]
mod tests;
