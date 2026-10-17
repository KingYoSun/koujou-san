use super::*;
use crate::auth::{DeviceClient, RuntimeBinding};
use journal::Journal;
use std::{
    collections::HashMap,
    io::{BufRead, BufReader, Read, Write},
    sync::{Arc, Mutex},
    thread,
    time::{Duration, Instant, SystemTime, UNIX_EPOCH},
};

struct Api {
    client: Arc<DeviceClient>,
    id: String,
    generation: String,
}
impl Api {
    fn request(&self, action: &str, method: &str, body: Value) -> Result<Value, String> {
        let ws = &self.client.binding.workspace;
        self.client.request(
            &["runs", "workspaces", ws, "executions", &self.id, action],
            method,
            body,
            &[("generation", &self.generation)],
        )
    }
    fn state(&self) -> Result<Value, String> {
        self.request("runner-state", "GET", Value::Null)
    }
    fn connection(&self, unknown: bool) -> Result<(), String> {
        self.observe(unknown, false)
    }
    fn observe(&self, unknown: bool, session_unconfirmed: bool) -> Result<(), String> {
        let state = self.state()?;
        if matches!(
            state["value"]["state"].as_str(),
            Some("succeeded" | "failed" | "cancelled")
        ) {
            return Ok(());
        }
        let mut body = json!({"operation_id":Uuid::new_v4().to_string(),"expected_version":state["run"]["version"],"generation":self.generation,"unknown":unknown});
        if session_unconfirmed {
            body["session_unconfirmed"] = json!(true);
        }
        self.request("connection", "POST", body)?;
        Ok(())
    }
    fn drain(&self, journal: &Journal, recovering: bool) -> Result<(), String> {
        for _ in 0..32 {
            let Some((position, operation, mut body)) = journal.pending(&self.id)? else {
                return Ok(());
            };
            // An attempted delivery was persisted only after the pipe write
            // succeeded. A crash before that point leaves delivery unconfirmed.
            if recovering && body["type"] == "delivered" && body.get("operation_id").is_none() {
                return Err("cli_delivery_unconfirmed".into());
            }
            for retry in 0..2 {
                if body.get("operation_id").is_none() || retry > 0 {
                    let state = self.state()?;
                    let sequence = state["value"]["reported_through"]
                        .as_str()
                        .ok_or("invalid_runner_state")?
                        .parse::<u64>()
                        .map_err(|_| "invalid_runner_state")?
                        + 1;
                    body["operation_id"] = json!(operation);
                    body["expected_version"] = state["run"]["version"].clone();
                    body["sequence"] = json!(sequence.to_string());
                    body["generation"] = json!(self.generation);
                }
                journal.attempted(position, &body)?;
                match self.request("report", "POST", body.clone()) {
                    Ok(_) => {
                        journal.sent(position)?;
                        break;
                    }
                    Err(e) if e == "version_conflict" && retry == 0 => continue,
                    Err(e) => return Err(e),
                }
            }
        }
        Ok(())
    }
    fn event(&self, journal: &Journal, value: Value) -> Result<(), String> {
        journal.enqueue(&self.id, &value)?;
        self.drain(journal, false)
    }
    fn reconcile(&self, journal: &Journal) -> Result<(), String> {
        self.observe(true, true)?;
        let identity = journal.identity(&self.id)?;
        journal.annotate(
            &self.id,
            "last_observation",
            observation::inspect(identity.as_ref()),
        )?;
        self.drain(journal, true)?;
        if journal.pending(&self.id)?.is_some() {
            return Ok(());
        }
        let state = self.state()?;
        let ended = matches!(
            state["value"]["state"].as_str(),
            Some("succeeded" | "failed" | "cancelled")
        );
        if !ended {
            self.connection(true)?;
        }
        journal.phase(
            &self.id,
            if ended { "completed" } else { "unlocated" },
            None,
        )
    }
}
fn publish(
    client: &DeviceClient,
    config: &Configuration,
    profiles: &execution::ExecutionSettings,
) -> Result<(), String> {
    let ws = &client.binding.workspace;
    let current = client.request(
        &["runs", "workspaces", ws, "runner", "self"],
        "GET",
        Value::Null,
        &[],
    )?;
    let mut connections = config.connections.clone();
    for connection in &mut connections {
        if let Some(adapter) = execution::selected(profiles, connection) {
            if let Some(caps) = connection["capabilities"].as_array_mut() {
                caps.retain(|c| {
                    if c == "pause" {
                        adapter.protocol == "jsonl-v1"
                    } else if c == "stop" {
                        adapter.stop_supported && adapter.wsl.is_empty()
                    } else {
                        true
                    }
                });
            }
        }
    }
    client.request(&["runs","workspaces",ws,"runner"],"POST",json!({"operation_id":Uuid::new_v4().to_string(),"expected_version":current["version"],"runner_id":config.runner_id,"connections":connections}),&[])?;
    Ok(())
}
fn reader(
    stream: impl Read + Send + 'static,
    journal: Journal,
    id: String,
    device: String,
    protocol: String,
    source: String,
    channel: &'static str,
    structured: bool,
    meter: Arc<Mutex<usage::Meter>>,
) -> thread::JoinHandle<Result<(), String>> {
    thread::spawn(move || {
        let mut input = BufReader::new(stream);
        let mut bytes = Vec::new();
        loop {
            bytes.clear();
            let n = input
                .read_until(b'\n', &mut bytes)
                .map_err(|_| "cli_output_unavailable")?;
            if n == 0 {
                break;
            }
            let position = journal.log(&id, channel, &bytes)?;
            if channel == "stdout" {
                if let Ok(mut meter) = meter.lock() {
                    let _ = meter.observe(&journal, position, &bytes);
                }
                if let Some(session) = protocol::session(&protocol, &bytes) {
                    journal.annotate(&id, "provider_session", json!(session))?;
                }
                if let Some(report) = protocol::result(&protocol, &bytes, structured) {
                    journal.result(&id, &report)?;
                }
            }
            for value in
                protocol::frames(&protocol, &device, &id, position, channel, &source, &bytes)
            {
                journal.enqueue(&id, &value)?;
            }
        }
        Ok(())
    })
}
fn run_one(
    api: &Api,
    journal: Journal,
    config: Configuration,
    profiles: execution::ExecutionSettings,
) -> Result<(), String> {
    let root = &api.client.binding.root;
    let initial = api.state()?;
    if initial["value"]["state"] != "queued"
        || initial["value"]["observation"] == "unknown"
        || !initial["value"]["started_at"].is_null()
    {
        return Err("execution_start_unconfirmed".into());
    }
    api.connection(false)?;
    let assigned = api.request("assigned-input", "GET", Value::Null)?;
    journal.input(&api.id, &assigned)?;
    let Some(adapter) = execution::selected(&profiles, &assigned["spec"]["input"]["command"])
    else {
        api.event(
            &journal,
            json!({"type":"preparation-blocked","source":"local_configuration_missing"}),
        )?;
        journal.phase(&api.id, "blocked", None)?;
        return Ok(());
    };
    let area = match execution::repository(&assigned, &config, Some(adapter), root) {
        Ok(v) => v,
        Err(_) => {
            api.event(
                &journal,
                json!({"type":"preparation-blocked","source":"local_configuration_missing"}),
            )?;
            journal.phase(&api.id, "blocked", None)?;
            return Ok(());
        }
    };
    let capture = usage::capture(
        adapter,
        assigned["assignment"]["connection"]["version"]
            .as_str()
            .unwrap_or(""),
    );
    let capture_config = capture.as_ref().map(|value| value.config());
    let mut prepared = match execution::prepare(
        root,
        &assigned,
        adapter,
        area.as_deref(),
        capture_config.as_deref(),
    ) {
        Ok(v) => v,
        Err(e) => {
            let reason = if e == "input_binding_unsupported" {
                e
            } else {
                "local_configuration_missing".into()
            };
            api.event(
                &journal,
                json!({"type":"preparation-blocked","source":reason}),
            )?;
            journal.phase(&api.id, "blocked", None)?;
            return Ok(());
        }
    };
    if assigned["assignment"]["prepared_at"].is_null() {
        api.event(
            &journal,
            json!({"type":"prepared","prepared_area":assigned["assignment"]["area"]}),
        )?;
    }
    let observed_version = match prepared.verify_version(
        assigned["assignment"]["connection"]["version"]
            .as_str()
            .unwrap_or(""),
    ) {
        Ok(v) => v,
        Err(_) => {
            api.event(
                &journal,
                json!({"type":"preparation-blocked","source":"local_configuration_missing"}),
            )?;
            journal.phase(&api.id, "blocked", None)?;
            return Ok(());
        }
    };
    let meter = Arc::new(Mutex::new(usage::Meter::new(
        &assigned,
        adapter,
        observed_version,
    )));
    if let Ok(mut meter) = meter.lock() {
        meter.attach_capture(capture);
    }
    // Resolve current authorization after all Git/file preparation, immediately
    // before the actual task process. No program is inferred from a shared target.
    let current = api.request("assigned-input", "GET", Value::Null)?;
    if current["assignment"]["input"] != assigned["assignment"]["input"]
        || current["assignment"]["area"] != assigned["assignment"]["area"]
    {
        return Err("assignment_changed".into());
    }
    let state = api.state()?;
    if state["value"]["state"] != "queued"
        || state["value"]["observation"] == "unknown"
        || !state["value"]["started_at"].is_null()
    {
        return Err("execution_start_unconfirmed".into());
    }
    if state["value"]["cancel_requested"] == true {
        api.event(&journal, json!({"type":"cancelled"}))?;
        journal.phase(&api.id, "completed", None)?;
        return Ok(());
    }
    journal.phase(&api.id, "starting", None)?;
    let mut child = match prepared.command.spawn() {
        Ok(child) => child,
        Err(_) => {
            api.event(
                &journal,
                json!({"type":"preparation-blocked","source":"process_start_failed"}),
            )?;
            journal.phase(&api.id, "blocked", None)?;
            return Ok(());
        }
    };
    let process = observation::snapshot(child.id()).unwrap_or_else(|_| json!({"pid":child.id()}));
    #[cfg(test)]
    if std::env::var("KOUJOU_TEST_CRASH_AFTER_SPAWN").as_deref() == Ok(api.id.as_str()) {
        fs::write(
            root.join("fixture-crash.json"),
            serde_json::to_vec(&json!({"run":api.id,"pid":child.id(),"process":process})).unwrap(),
        )
        .unwrap();
        std::process::exit(77);
    }
    journal.started(&api.id, child.id(), &json!({"process":process,"protocol":prepared.protocol,"environment":assigned["assignment"]["connection"]["environment"],"session_kind":"anonymous_stdio","transfer_verified":false}))?;
    if let Ok(mut meter) = meter.lock() {
        meter.process(child.id());
    }
    journal.enqueue(&api.id,&json!({"type":"started","execution":{"environment":if adapter.wsl.is_empty(){"windows".to_owned()}else{format!("wsl:{}",adapter.wsl)},"version":assigned["assignment"]["connection"]["version"],"version_observed":observed_version,"pause":prepared.protocol=="jsonl-v1","stop":prepared.stop_supported&&adapter.wsl.is_empty()}}))?;
    let device = assigned["assignment"]["device_id"]
        .as_str()
        .unwrap_or("")
        .to_owned();
    let source = assigned["spec"]["input"]["command"]["target"]
        .as_str()
        .unwrap_or("")
        .to_owned();
    let stdout = reader(
        child.stdout.take().ok_or("cli_output_unavailable")?,
        journal.clone(),
        api.id.clone(),
        device.clone(),
        prepared.protocol.clone(),
        source.clone(),
        "stdout",
        !assigned["spec"]["derivation"]["result_schema"].is_null(),
        meter.clone(),
    );
    let stderr = reader(
        child.stderr.take().ok_or("cli_output_unavailable")?,
        journal.clone(),
        api.id.clone(),
        device,
        prepared.protocol.clone(),
        source,
        "stderr",
        false,
        meter.clone(),
    );
    let mut online = api.drain(&journal, false).is_ok();
    let mut disconnected = !online;
    let mut stdin = child.stdin.take();
    if let Some(input) = stdin.as_mut() {
        if input
            .write_all(&prepared.input)
            .and_then(|_| input.flush())
            .is_err()
        {
            online = false;
        }
    }
    if prepared.protocol != "jsonl-v1" {
        drop(stdin.take());
    }
    let mut renewal = Instant::now();
    let mut stopped = false;
    let mut last_observation = SystemTime::now();
    loop {
        if let Some(status) = child.try_wait().map_err(|_| "cli_status_unavailable")? {
            stdout.join().map_err(|_| "cli_output_unavailable")??;
            stderr.join().map_err(|_| "cli_output_unavailable")??;
            if let Ok(mut meter) = meter.lock() {
                let _ = meter.finish(&journal);
            }
            journal.enqueue(&api.id,&json!({"type":if stopped{"cancelled"}else{"finished"},"exit_code":status.code().unwrap_or(-1),"report":journal.final_result(&api.id)?}))?;
            // The source process has ended; unsent facts remain durable until accepted.
            loop {
                let _ = api.connection(!online);
                if api.drain(&journal, false).is_ok() && journal.pending(&api.id)?.is_none() {
                    journal.phase(&api.id, "completed", None)?;
                    return Ok(());
                }
                journal.phase(&api.id, "unconfirmed", None)?;
                thread::sleep(Duration::from_secs(1));
            }
        }
        let now = SystemTime::now();
        if now.duration_since(last_observation).unwrap_or_default() >= Duration::from_secs(30) {
            online = false;
        }
        last_observation = now;
        if renewal.elapsed() >= Duration::from_secs(30) || !online {
            disconnected |= !online;
            online = api.connection(!online).is_ok();
            renewal = Instant::now();
        }
        if online {
            online = api.drain(&journal, false).is_ok();
        }
        if online && disconnected {
            online = api
                .event(
                    &journal,
                    json!({"type":"known","source":"same_process_observed"}),
                )
                .is_ok();
            if online {
                journal.phase(&api.id, "running", Some(child.id()))?;
                disconnected = false;
            }
        }
        if let Ok(state) = api.state() {
            if state["value"]["cancel_requested"] == true
                && prepared.stop_supported
                && adapter.wsl.is_empty()
                && !stopped
            {
                stopped = child.kill().is_ok();
            }
            let pause = &state["pause"];
            if online && pause["state"] == "received" && prepared.protocol == "jsonl-v1" {
                let response = pause["response_id"]
                    .as_str()
                    .ok_or("invalid_runner_state")?;
                if journal.claim_response(&api.id, response)? {
                    // Queue the delivery fact before writing, so the child's immediate resume
                    // event cannot overtake it. It is sent only after the write succeeds.
                    journal.enqueue(
                        &api.id,
                        &json!({"type":"delivered","pause_id":pause["id"],"response_id":response}),
                    )?;
                    let mut answer=serde_json::to_vec(&json!({"type":"answer","pause_id":pause["id"],"response_id":response,"value":pause["response"]})).map_err(|_|"invalid_runner_state")?;
                    answer.push(b'\n');
                    if stdin
                        .as_mut()
                        .ok_or("cli_input_unavailable")?
                        .write_all(&answer)
                        .and_then(|_| stdin.as_mut().unwrap().flush())
                        .is_err()
                    {
                        journal.phase(&api.id, "unconfirmed", None)?;
                        return Err("cli_delivery_unconfirmed".into());
                    }
                    online = api.drain(&journal, false).is_ok();
                }
            }
        } else {
            online = false;
        }
        if !online {
            journal.phase(&api.id, "unconfirmed", None)?;
        }
        thread::sleep(Duration::from_millis(250));
    }
}
fn lock(root: &Path) -> Result<rusqlite::Connection, String> {
    let db = rusqlite::Connection::open(root.join("runner-lock.sqlite"))
        .map_err(|_| "runner_lock_unavailable")?;
    db.execute_batch("BEGIN EXCLUSIVE")
        .map_err(|_| "runner_already_running")?;
    Ok(db)
}
fn serve(binding: RuntimeBinding) -> Result<(), String> {
    let root = binding.root.clone();
    fs::create_dir_all(&root).map_err(|_| "runner_directory_failed")?;
    let _guard = lock(&root)?;
    let client = Arc::new(DeviceClient::open(binding)?);
    let journal = Journal::open(&root)?;
    let mut jobs: HashMap<String, thread::JoinHandle<()>> = HashMap::new();
    let mut contact = Instant::now() - Duration::from_secs(60);
    journal.recover_unknown()?;
    let mut recovery_after = String::new();
    let mut connected = false;
    loop {
        jobs.retain(|_, job| !job.is_finished());
        let config = config(&root)?;
        let profiles = execution::settings(&root)?;
        if contact.elapsed() >= Duration::from_secs(30) {
            connected = publish(&client, &config, &profiles).is_ok();
            if connected {
                contact = Instant::now();
            }
        }
        if connected {
            let _ = usage::drain(&client, &journal);
            let recovered = journal.recovery_page(&recovery_after)?;
            recovery_after = if recovered.len() == 50 {
                recovered.last().unwrap().0.clone()
            } else {
                String::new()
            };
            for (id, generation) in recovered {
                if jobs.contains_key(&id) {
                    continue;
                }
                let api = Api {
                    client: client.clone(),
                    id: id.clone(),
                    generation: generation.clone(),
                };
                if let Err(e) = api.reconcile(&journal) {
                    if matches!(
                        e.as_str(),
                        "forbidden"
                            | "cli_delivery_unconfirmed"
                            | "state_conflict"
                            | "invalid_request"
                    ) {
                        journal.phase(&id, "unlocated", None)?;
                    }
                }
            }
        }
        let stopping = root.join("stop-accepting").exists();
        if !stopping && connected {
            let mut after = String::new();
            loop {
                let ws = &client.binding.workspace;
                let page = client.request(
                    &["runs", "workspaces", ws, "runner", "assignments"],
                    "GET",
                    Value::Null,
                    &[("after", &after)],
                );
                let Ok(page) = page else {
                    connected = false;
                    break;
                };
                for state in page["items"].as_array().ok_or("invalid_runner_state")? {
                    let Some(id) = state["run"]["id"].as_str() else {
                        continue;
                    };
                    if jobs.contains_key(id)
                        || state["value"]["assignment"]["runner_id"] != config.runner_id
                    {
                        continue;
                    }
                    let generation = state["value"]["assignment"]["generation"]
                        .as_str()
                        .ok_or("invalid_runner_state")?;
                    use sha2::{Digest, Sha256};
                    let hash = format!(
                        "{:x}",
                        Sha256::digest(
                            serde_json::to_vec(&profiles)
                                .map_err(|_| "invalid_execution_configuration")?
                        )
                    );
                    if !journal.begin(id, generation, &hash)? {
                        continue;
                    }
                    if state["value"]["state"] != "queued"
                        || state["value"]["observation"] == "unknown"
                        || !state["value"]["started_at"].is_null()
                    {
                        journal.phase(id, "unknown", None)?;
                        continue;
                    }
                    let api = Api {
                        client: client.clone(),
                        id: id.to_owned(),
                        generation: generation.to_owned(),
                    };
                    let own = journal.clone();
                    let cfg = config.clone();
                    let settings = profiles.clone();
                    let run = id.to_owned();
                    jobs.insert(
                        id.to_owned(),
                        thread::spawn(move || {
                            if run_one(&api, own.clone(), cfg, settings).is_err() {
                                let _ = own.phase(&run, "unknown", None);
                                let _ = api.connection(true);
                            }
                        }),
                    );
                }
                after = page["next"].as_str().unwrap_or("").to_owned();
                if after.is_empty() {
                    break;
                }
            }
        }
        let now = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .unwrap_or_default()
            .as_secs();
        let summary = journal.summary()?;
        fs::write(root.join("runner-status.json"),serde_json::to_vec(&json!({"pid":std::process::id(),"last_seen":now,"connected":connected,"accepting":!stopping,"active":jobs.len(),"journal":summary})).map_err(|_|"runner_status_failed")?).map_err(|_|"runner_status_failed")?;
        if stopping && jobs.is_empty() {
            break;
        }
        thread::sleep(Duration::from_secs(1));
    }
    Ok(())
}
pub(crate) fn main_entry() {
    let mut args = std::env::args().skip(1);
    if args.next().as_deref() != Some("--binding") {
        return;
    }
    let Some(file) = args.next() else { return };
    let result = fs::read(file)
        .map_err(|_| "runner_binding_unavailable".to_owned())
        .and_then(|raw| {
            serde_json::from_slice::<RuntimeBinding>(&raw)
                .map_err(|_| "invalid_runner_binding".to_owned())
        })
        .and_then(serve);
    if result.is_err() {
        std::process::exit(1)
    }
}
pub(super) fn start(auth: &Auth, workspace: &str) -> Result<Value, String> {
    let binding = auth.runner_binding(workspace)?;
    fs::create_dir_all(&binding.root).map_err(|_| "runner_directory_failed")?;
    if execution::settings(&binding.root)?.adapters.is_empty() {
        return Err("local_configuration_missing".into());
    }
    let file = binding.root.join("binding.json");
    fs::write(
        &file,
        serde_json::to_vec(&binding).map_err(|_| "runner_binding_unavailable")?,
    )
    .map_err(|_| "runner_binding_unavailable")?;
    let _ = fs::remove_file(binding.root.join("stop-accepting"));
    let executable = std::env::current_exe()
        .map_err(|_| "runner_binary_unavailable")?
        .with_file_name(if cfg!(windows) {
            "koujou-san-runner.exe"
        } else {
            "koujou-san-runner"
        });
    let mut command = Command::new(executable);
    command
        .arg("--binding")
        .arg(file)
        .stdin(std::process::Stdio::null())
        .stdout(std::process::Stdio::null())
        .stderr(std::process::Stdio::null());
    #[cfg(windows)]
    {
        use std::os::windows::process::CommandExt;
        command.creation_flags(0x08000000 | 0x00000200);
    }
    let child = command.spawn().map_err(|_| "runner_binary_unavailable")?;
    Ok(json!({"requested":true,"pid":child.id(),"command_started":false}))
}
pub(super) fn status(auth: &Auth, workspace: &str) -> Result<Value, String> {
    let root = auth.runner_storage(workspace)?;
    if !root.exists() {
        return Ok(json!({"running":false}));
    }
    let running = match lock(&root) {
        Ok(_) => false,
        Err(e) if e == "runner_already_running" => true,
        Err(e) => return Err(e),
    };
    let value = fs::read(root.join("runner-status.json"))
        .ok()
        .and_then(|s| serde_json::from_slice::<Value>(&s).ok())
        .unwrap_or(Value::Null);
    Ok(json!({"running":running,"last_observation":value}))
}
pub(super) fn reconcile(auth: &Auth, workspace: &str, id: &str) -> Result<Value, String> {
    let binding = auth.runner_binding(workspace)?;
    let journal = Journal::open(&binding.root)?;
    let id = format!("run:{}", id.strip_prefix("run:").unwrap_or(id));
    let (generation, phase) = journal.execution(&id)?.ok_or("execution_not_recorded")?;
    let client = Arc::new(DeviceClient::open(binding)?);
    publish(
        &client,
        &config(&client.binding.root)?,
        &execution::settings(&client.binding.root)?,
    )?;
    let api = Api {
        client,
        id: id.clone(),
        generation,
    };
    // A local PID is never a substitute for current server authorization.
    let state = api.state()?;
    let identity = journal.identity(&id)?;
    let observation = observation::inspect(identity.as_ref());
    let queued = matches!(phase.as_str(), "unknown" | "unconfirmed" | "unlocated");
    if queued {
        journal.phase(&id, "unknown", None)?;
    }
    Ok(
        json!({"run":state["run"],"local_phase":phase,"observation":observation,"queued_reconciliation":queued,"command_started":false}),
    )
}

#[cfg(all(test, windows))]
#[path = "runtime_usage_test.rs"]
mod usage_tests;

#[cfg(all(test, windows))]
mod tests {
    use super::*;
    #[test]
    #[ignore = "requires the explicitly owned Ory/API and saved native Run record"]
    fn live_reconcile_owned_record() {
        let fixture: Value = serde_json::from_slice(
            &fs::read(std::env::var("KOUJOU_CONTEXT_UI_FIXTURE").unwrap()).unwrap(),
        )
        .unwrap();
        let auth = Auth::new(
            fixture["api"].as_str().unwrap(),
            PathBuf::from(std::env::var("KOUJOU_NATIVE_TEST_FILE").unwrap()),
        )
        .unwrap();
        let id = std::env::var("KOUJOU_RUNNER_TEST_ID").expect("select owned Run");
        let path = format!("/runs/workspaces/runner-ui/records/{id}?version=head");
        let before = auth.request(&path, "GET", Value::Null).unwrap();
        let value = reconcile(&auth, "runner-ui", &id).unwrap();
        assert_eq!(value["command_started"], false);
        assert_eq!(value["observation"]["continuation_verified"], false);
        assert_eq!(value["queued_reconciliation"], true);
        assert_eq!(
            auth.request(&path, "GET", Value::Null).unwrap()["version"],
            before["version"]
        );
        fs::write(
            std::env::var("KOUJOU_NATIVE_TEST_RESULT").unwrap(),
            serde_json::to_vec_pretty(&value).unwrap(),
        )
        .unwrap();
        println!(
            "Native reconciliation checked current OAuth/device authorization and saved generation without starting a CLI or changing shared execution facts."
        );
    }
    #[test]
    #[ignore = "requires the explicitly owned native Runner/Ory fixture; used for fixed crash boundary testing"]
    fn live_reconnect_runner() {
        let file = std::env::var("KOUJOU_RUNNER_TEST_BINDING").expect("select owned test binding");
        let binding: RuntimeBinding = serde_json::from_slice(&fs::read(file).unwrap()).unwrap();
        serve(binding).unwrap();
    }
}
