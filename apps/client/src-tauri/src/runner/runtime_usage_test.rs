use super::*;

#[test]
#[ignore = "requires the explicitly owned Ory/API fixture, native OAuth/device namespace and CLI profiles"]
fn live_owned_runner_usage() {
    let fixture: Value = serde_json::from_slice(
        &fs::read(std::env::var("KOUJOU_CONTEXT_UI_FIXTURE").unwrap()).unwrap(),
    )
    .unwrap();
    let auth = Auth::new(
        fixture["api"].as_str().unwrap(),
        PathBuf::from(std::env::var("KOUJOU_NATIVE_TEST_FILE").unwrap()),
    )
    .unwrap();
    let binding = auth.runner_binding("runner-ui").unwrap();
    let me = auth.request("/auth/me", "GET", Value::Null).unwrap();
    let user = me["user"]["id"].as_str().unwrap();
    let device: Value = serde_json::from_slice(
        &fs::read(std::env::var("KOUJOU_RUNNER_METADATA").unwrap()).unwrap(),
    )
    .unwrap();
    let raw_profiles: Value = serde_json::from_slice(
        &fs::read(std::env::var("KOUJOU_USAGE_LIVE_PROFILES").unwrap()).unwrap(),
    )
    .unwrap();
    let profiles = raw_profiles.as_array().unwrap();
    let mut settings = execution::ExecutionSettings::default();
    let mut connections = vec![];
    for profile in profiles {
        let mut adapter: execution::Adapter =
            serde_json::from_value(profile["adapter"].clone()).unwrap();
        adapter.target = format!("usage-{}", adapter.protocol);
        connections.push(json!({"kind":"cli","target":adapter.target,"version":profile["version"],"environment":"windows","subject":"owned-cli-account","confirmed":true,"bindings":["stdin"],"capabilities":[],"users":[user],"repositories":[]}));
        settings.adapters.push(adapter);
    }
    let configuration = Configuration {
        runner_id: "usage-owned-runner".into(),
        connections,
        repositories: BTreeMap::new(),
    };
    let client = Arc::new(DeviceClient::open(binding).unwrap());
    publish(&client, &configuration, &settings).unwrap();
    let journal = Journal::open(&client.binding.root).unwrap();
    let post = |path: &str, mut body: Value| {
        body["operation_id"] = json!(Uuid::new_v4().to_string());
        body["expected_version"] = json!("0");
        let value = auth.request(path, "POST", body).unwrap();
        assert!(value.get("error").is_none(), "owned mutation rejected");
        value
    };
    let mut proof = vec![];
    for (i, adapter) in settings.adapters.iter().enumerate() {
        let reuse = if i == 0 {
            std::env::var("KOUJOU_USAGE_COMPLETED_CODEX").ok()
        } else {
            None
        };
        let id = reuse
            .clone()
            .unwrap_or_else(|| format!("run:usage-live-{i}-{}", Uuid::new_v4()));
        let read_path = format!("/runs/workspaces/runner-ui/records/{id}?version=head");
        if reuse.is_none() {
            let name = if i == 0 {
                "受入: Codexの利用量と費用"
            } else {
                "受入: Claude Codeの利用量と費用"
            };
            let base = format!("/runs/workspaces/runner-ui/executions/{id}");
            let created = post(
                &format!("{base}/create"),
                json!({"spec":{"name":name,"input":{"command":{"kind":"cli","target":adapter.target,"binding":{"prompt":"stdin"}},"prompt":"Metering connection probe only. Reply exactly OK. Do not use tools or inspect files."}}}),
            );
            let assigned=auth.request(&format!("{base}/assign"),"POST",json!({"operation_id":Uuid::new_v4().to_string(),"expected_version":created["data"]["version"],"device_id":device["device_id"]})).unwrap();
            assert!(assigned.get("error").is_none());
            let read_path = format!("/runs/workspaces/runner-ui/records/{id}?version=head");
            let head = auth.request(&read_path, "GET", Value::Null).unwrap();
            let generation = head["value"]["assignment"]["generation"].as_str().unwrap();
            assert!(journal.begin(&id, generation, "usage-v1").unwrap());
            let api = Api {
                client: client.clone(),
                id: id.clone(),
                generation: generation.into(),
            };
            run_one(
                &api,
                journal.clone(),
                configuration.clone(),
                settings.clone(),
            )
            .unwrap();
        }
        let ended = auth.request(&read_path, "GET", Value::Null).unwrap();
        let generation = ended["value"]["assignment"]["generation"].as_str().unwrap();
        assert_eq!(
            ended["value"]["assignment"]["device_id"],
            device["device_id"]
        );
        assert_eq!(journal.execution(&id).unwrap().unwrap().1, "completed");
        assert_eq!(ended["value"]["state"], "succeeded");
        assert_eq!(ended["value"]["exit_code"], 0);
        for _ in 0..16 {
            let pending: bool = journal
                .db()
                .unwrap()
                .query_row(
                    "SELECT EXISTS(SELECT 1 FROM execution_usage WHERE sent=0)",
                    [],
                    |r| r.get(0),
                )
                .unwrap();
            if !pending {
                break;
            }
            usage::drain(&client, &journal).unwrap();
        }
        assert_eq!(
            auth.request(&read_path, "GET", Value::Null).unwrap()["version"],
            ended["version"],
            "metering changed Run result"
        );
        let usage_path = "/usage/workspaces/runner-ui/query";
        let page = auth
            .request(
                usage_path,
                "POST",
                json!({"mode":"run","kind":"usage","attribution_id":id,"limit":50}),
            )
            .unwrap();
        let items = page["data"]["items"].as_array().unwrap();
        assert!(!items.is_empty());
        let mut evaluations = vec![];
        for (n, entry) in items.iter().enumerate() {
            assert!(entry["value"]["actual_model"].is_string());
            assert_eq!(entry["state"], "confirmed");
            assert_eq!(entry["value"]["measurement"]["generation"], generation);
            let price_id = format!("price:usage-live-{i}-{n}");
            let mut rates = json!({});
            for key in ["input", "cache_read", "cache_write", "output", "reasoning"] {
                rates[key] = json!({"amount":"1","per":"1000000","unit":"token"});
            }
            let price = post(
                &format!("/usage/workspaces/runner-ui/records/{price_id}"),
                json!({"value":{"connection":adapter.target,"contract":entry["value"]["contract"],"model":entry["value"]["actual_model"],"currency":"USD","effective_from":"2020-01-01T00:00:00Z","source":"fixture://individual-contract","scale":6,"rounding":"half_up","rates":rates}}),
            );
            let eval_id = format!("evaluation:usage-live-{i}-{n}");
            evaluations.push(post(&format!("/usage/workspaces/runner-ui/records/{eval_id}"),json!({"value":{"coverage_group":entry["group"],"kind":"estimated","price":{"id":price_id,"version":price["data"]["version"]}}})));
        }
        let fees = auth
            .request(
                usage_path,
                "POST",
                json!({"mode":"run","kind":"estimated","attribution_id":id,"limit":50}),
            )
            .unwrap();
        assert_eq!(fees["data"]["items"].as_array().unwrap().len(), items.len());
        proof.push(json!({"run":id,"head":ended["version"],"state":"succeeded","usage":page,"evaluations":evaluations,"fees":fees,"run_unchanged_by_usage":true}));
    }
    fs::write(
        std::env::var("KOUJOU_USAGE_LIVE_RESULT").unwrap(),
        serde_json::to_vec_pretty(&proof).unwrap(),
    )
    .unwrap();
    println!(
        "Owned native Runner: real CLI process -> encrypted journal -> authenticated usage report -> Run page -> fixed individual tariff. Run results unchanged."
    );
}
