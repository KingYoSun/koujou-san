use super::*;

fn fixture(protocol: &str, version: &str, settings: Value) -> (PathBuf, Journal, Meter) {
    let root = std::env::temp_dir().join(format!("koujou-usage-{}", Uuid::new_v4()));
    let journal = Journal::open(&root).unwrap();
    let adapter:execution::Adapter=serde_json::from_value(json!({"kind":"cli","target":"metered","program":"fixture","protocol":protocol,"usage":settings})).unwrap();
    let assigned = json!({"run":{"id":"run:metered"},"spec":{"input":{"command":{"target":"metered"}}},"assignment":{"generation":"generation","device_id":"device","connection":{"version":version}}});
    let mut meter = Meter::new(&assigned, &adapter, true);
    meter.process(42);
    (root, journal, meter)
}
fn rows(journal: &Journal) -> Vec<Value> {
    let db = journal.db().unwrap();
    let mut q = db
        .prepare("SELECT payload FROM execution_usage ORDER BY rowid")
        .unwrap();
    q.query_map([], |r| r.get::<_, Vec<u8>>(0))
        .unwrap()
        .map(|r| serde_json::from_slice(&unprotect(&r.unwrap()).unwrap()).unwrap())
        .collect()
}
fn cleanup(root: PathBuf) {
    assert!(root.starts_with(std::env::temp_dir()));
    assert!(
        root.file_name()
            .unwrap()
            .to_string_lossy()
            .starts_with("koujou-usage-")
    );
    fs::remove_dir_all(root).unwrap();
}

#[test]
fn models_provisional_final_and_duplicate_messages_keep_machine_boundaries() {
    let (root, journal, mut meter) = fixture(
        "claude-jsonl",
        "2.1.300",
        json!({"mode":"live","billing":"subscription"}),
    );
    meter.observe(&journal,1,br#"{"type":"system","subtype":"init","session_id":"owned","apiKeySource":"SYNTHETIC_SECRET"}"#).unwrap();
    let step=br#"{"type":"assistant","message":{"id":"request-one","model":"model-a","usage":{"input_tokens":10,"cache_read_input_tokens":2,"cache_creation_input_tokens":3,"output_tokens":1},"content":[{"text":"SYNTHETIC_SECRET usage=999999"}]}}"#;
    meter.observe(&journal, 2, step).unwrap();
    meter.observe(&journal, 3, step).unwrap();
    assert_eq!(rows(&journal).len(), 1);
    assert_eq!(
        rows(&journal)[0]["value"]["quantities"]["output"]["value"],
        Value::Null
    );
    meter.observe(&journal,4,br#"{"type":"assistant","parent_tool_use_id":"child-job","message":{"id":"child-request","model":"model-b","usage":{"input_tokens":4,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}"#).unwrap();
    meter.observe(&journal,5,br#"{"type":"assistant","message":{"id":"retry-request","model":"model-a","usage":{"input_tokens":7,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}"#).unwrap();
    meter.observe(&journal,6,br#"{"type":"result","uuid":"result-one","modelUsage":{"model-a":{"inputTokens":17,"cacheReadInputTokens":2,"cacheCreationInputTokens":3,"outputTokens":8,"thinkingTokens":2,"costUSD":0.04},"model-b":{"inputTokens":4,"cacheReadInputTokens":0,"cacheCreationInputTokens":0,"outputTokens":2,"thinkingTokens":0,"costUSD":5e-7}}}"#).unwrap();
    meter.finish(&journal).unwrap();
    let data = rows(&journal);
    let final_a = &data[data.len() - 2]["value"];
    assert_eq!(final_a["measurement"]["state"], "final");
    assert_eq!(final_a["quantities"]["input"]["value"], "17");
    assert_eq!(final_a["quantities"]["output"]["value"], "8");
    assert_eq!(final_a["measurement"]["billing"], "subscription");
    assert_eq!(
        data.last().unwrap()["value"]["measurement"]["estimate_usd"],
        "0.0000005"
    );
    let serialized = serde_json::to_string(&data).unwrap();
    assert!(!serialized.contains("SYNTHETIC_SECRET"));
    assert!(!serialized.contains("999999"));
    assert!(!serialized.contains("content"));
    cleanup(root);
}

#[test]
fn resumed_totals_are_review_candidates_not_this_runs_measured_consumption() {
    let (root, journal, mut meter) = fixture("claude-jsonl", "2.1.300", json!({}));
    meter.resumed = true;
    meter
        .observe(
            &journal,
            1,
            br#"{"type":"system","subtype":"init","session_id":"old-session"}"#,
        )
        .unwrap();
    meter.observe(&journal,2,br#"{"type":"result","modelUsage":{"model-a":{"inputTokens":100000,"outputTokens":500,"costUSD":20.0}}}"#).unwrap();
    meter.finish(&journal).unwrap();
    let value = &rows(&journal)[0]["value"];
    assert_eq!(value["coverage"], "unknown");
    assert_eq!(value["measurement"]["state"], "partial");
    assert_eq!(value["measurement"]["estimate_usd"], Value::Null);
    assert_eq!(
        value["measurement"]["reason"],
        "session_boundary_unconfirmed"
    );
    cleanup(root);
}

#[test]
fn unsupported_non_llm_and_interrupted_are_distinct_without_execution_events() {
    for (protocol, version, settings, state) in [
        ("plain", "v1", json!({"non_llm":true}), "not_applicable"),
        ("claude-jsonl", "not-adopted", json!({}), "unavailable"),
        ("claude-jsonl", "2.1.300", json!({}), "partial"),
    ] {
        let (root, journal, mut meter) = fixture(protocol, version, settings);
        meter.observe(&journal,1,br#"{"type":"assistant","message":{"id":"one","model":"a","usage":{"input_tokens":10}}}"#).unwrap();
        meter.finish(&journal).unwrap();
        let data = rows(&journal);
        assert_eq!(data[0]["value"]["measurement"]["state"], state);
        let events: i64 = journal
            .db()
            .unwrap()
            .query_row("SELECT count(*) FROM execution_frames", [], |r| r.get(0))
            .unwrap();
        assert_eq!(events, 0);
        cleanup(root);
    }
}

#[test]
fn unsent_encrypted_usage_survives_reopen_and_lost_ack_with_same_operation() {
    let (root, journal, mut meter) = fixture("plain", "v1", json!({}));
    meter.finish(&journal).unwrap();
    let mut first = Value::Null;
    assert!(
        send_pending(&journal, |run, body| {
            assert_eq!(run, "run:metered");
            first = body;
            Err("response_lost".into())
        })
        .is_err()
    );
    let disk = fs::read(root.join("executions.sqlite")).unwrap();
    assert!(
        !disk
            .windows(b"provider_did_not_report".len())
            .any(|w| w == b"provider_did_not_report")
    );
    let reopened = Journal::open(&root).unwrap();
    meter.finish(&reopened).unwrap();
    assert_eq!(rows(&reopened).len(), 1);
    send_pending(&reopened, |_, body| {
        assert_eq!(body, first);
        Ok(json!({"data":{"version":"accepted"}}))
    })
    .unwrap();
    let n: i64 = reopened
        .db()
        .unwrap()
        .query_row(
            "SELECT count(*) FROM execution_usage WHERE sent=0",
            [],
            |r| r.get(0),
        )
        .unwrap();
    assert_eq!(n, 0);
    assert_eq!(rows(&reopened)[0]["expected_version"], "0");
    cleanup(root);
}

#[test]
fn lost_provisional_ack_is_recovered_before_sending_the_final_cumulative_value() {
    let (root, journal, mut meter) = fixture("claude-jsonl", "2.1.300", json!({"mode":"live"}));
    meter
        .observe(
            &journal,
            1,
            br#"{"type":"system","subtype":"init","session_id":"owned"}"#,
        )
        .unwrap();
    meter.observe(&journal,2,br#"{"type":"assistant","message":{"id":"request","model":"a","usage":{"input_tokens":10}}}"#).unwrap();
    meter
        .observe(
            &journal,
            3,
            br#"{"type":"result","modelUsage":{"a":{"inputTokens":10,"outputTokens":8}}}"#,
        )
        .unwrap();
    meter.finish(&journal).unwrap();
    assert_eq!(rows(&journal).len(), 3);
    let mut accepted = Value::Null;
    assert!(
        send_pending(&journal, |_, body| {
            assert_eq!(body["expected_version"], "0");
            assert_eq!(body["value"]["measurement"]["state"], "provisional");
            accepted = body;
            Err("accepted_but_response_lost".into())
        })
        .is_err()
    );
    send_pending(&journal, |_, body| {
        assert_eq!(body, accepted);
        Ok(json!({"data":{"version":"v1"}}))
    })
    .unwrap();
    send_pending(&journal, |_, body| {
        assert_eq!(body["expected_version"], "v1");
        assert_ne!(body["operation_id"], accepted["operation_id"]);
        assert_eq!(body["value"]["measurement"]["state"], "provisional");
        Ok(json!({"data":{"version":"v2"}}))
    })
    .unwrap();
    send_pending(&journal, |_, body| {
        assert_eq!(body["expected_version"], "v2");
        assert_eq!(body["value"]["measurement"]["state"], "final");
        assert_eq!(body["value"]["quantities"]["output"]["value"], "8");
        Ok(json!({"data":{"version":"v3"}}))
    })
    .unwrap();
    assert_eq!(
        journal
            .db()
            .unwrap()
            .query_row(
                "SELECT count(*) FROM execution_usage WHERE sent=0",
                [],
                |r| r.get::<_, i64>(0)
            )
            .unwrap(),
        0
    );
    assert_eq!(
        journal
            .db()
            .unwrap()
            .query_row("SELECT count(*) FROM execution_frames", [], |r| r
                .get::<_, i64>(0))
            .unwrap(),
        0
    );
    cleanup(root);
}

#[test]
fn codex_without_a_model_source_keeps_quantities_and_marks_the_model_missing() {
    let (root, journal, mut meter) = fixture("codex-jsonl", "0.162.1", json!({}));
    meter
        .observe(
            &journal,
            1,
            br#"{"type":"thread.started","thread_id":"owned"}"#,
        )
        .unwrap();
    meter.observe(&journal,2,br#"{"type":"turn.completed","usage":{"input_tokens":100,"cached_input_tokens":60,"cache_write_input_tokens":0,"output_tokens":10,"reasoning_output_tokens":2}}"#).unwrap();
    meter.finish(&journal).unwrap();
    assert_eq!(rows(&journal)[0]["value"]["actual_model"], Value::Null);
    assert_eq!(rows(&journal)[0]["value"]["coverage"], "confirmed");
    assert_eq!(
        rows(&journal)[0]["value"]["measurement"]["state"],
        "partial"
    );
    assert_eq!(
        rows(&journal)[0]["value"]["quantities"]["input"]["includes"],
        json!(["cache_read", "cache_write"])
    );
    cleanup(root);
}

#[test]
fn one_denied_report_stays_pending_without_blocking_another_authorized_run() {
    let (root, journal, mut meter) = fixture("plain", "v1", json!({}));
    meter.run = "run:denied".into();
    meter.finish(&journal).unwrap();
    meter.run = "run:allowed".into();
    meter.finish(&journal).unwrap();
    assert!(
        send_pending(&journal, |run, _| {
            assert_eq!(run, "run:denied");
            Err("forbidden".into())
        })
        .is_err()
    );
    send_pending(&journal, |run, _| {
        assert_eq!(run, "run:allowed");
        Ok(json!({"data":{"version":"accepted"}}))
    })
    .unwrap();
    let pending: String = journal
        .db()
        .unwrap()
        .query_row("SELECT run_id FROM execution_usage WHERE sent=0", [], |r| {
            r.get(0)
        })
        .unwrap();
    assert_eq!(pending, "run:denied");
    cleanup(root);
}

#[test]
fn continuation_flags_follow_each_providers_cli_contract() {
    for (protocol, args, resumed) in [
        (
            "codex-jsonl",
            vec!["exec", "-c", "model_reasoning_effort=low"],
            false,
        ),
        ("codex-jsonl", vec!["exec", "resume", "owned"], true),
        ("claude-jsonl", vec!["--continue"], true),
    ] {
        let adapter:execution::Adapter=serde_json::from_value(json!({"kind":"cli","target":"test","program":"fixture","protocol":protocol,"arguments":args})).unwrap();
        let meter = Meter::new(&json!({}), &adapter, true);
        assert_eq!(meter.resumed, resumed);
    }
}
