//! Explicitly authorized, synthetic-only subscription trial. No CLI cache is read.
use super::*;
use rig_core::providers::chatgpt::auth::TokenStore;

#[test]
#[ignore = "requires explicit authorization for a new provider OAuth sign-in and at most two synthetic model calls"]
fn live_subscription_two_request_trial() {
    assert_eq!(
        std::env::var("KOUJOU_RIG_LIVE_AUTHORIZED").as_deref(),
        Ok("yes")
    );
    let provider = std::env::var("KOUJOU_RIG_LIVE_PROVIDER").unwrap();
    let model = std::env::var("KOUJOU_RIG_LIVE_MODEL").unwrap();
    let endpoint = match provider.as_str() {
        "chatgpt" => "https://chatgpt.com/backend-api/codex",
        "copilot" => "https://api.githubcopilot.com",
        _ => panic!("select an approved provider"),
    };
    let root = Path::new(env!("CARGO_MANIFEST_DIR"))
        .parent()
        .unwrap()
        .parent()
        .unwrap()
        .parent()
        .unwrap()
        .join(".cache")
        .join(format!("rig-live-{}", Uuid::new_v4()));
    let stored:Stored=serde_json::from_value(json!({"profile":{"id":"owned-live","name":"Synthetic subscription trial","provider":provider,"auth":"oauth","endpoint":endpoint,"model":model,"allowed_models":[model],"revision":"live-trial"},"key":""})).unwrap();
    let store = rig_auth::Store::new(&root.join("models.dpapi"), "owned-live");
    tauri::async_runtime::block_on(async {
        let http = rig::http(&stored.profile, 15 * 60 * 1000).unwrap();
        let prompt = tauri::ipc::Channel::<Value>::new(|body| {
            if let tauri::ipc::InvokeResponseBody::Json(text) = body {
                let value: Value = serde_json::from_str(&text).unwrap();
                if let Some(url) = value["url"].as_str() {
                    println!("Provider authorization URL: {url}");
                }
                if let Some(code) = value["code"].as_str() {
                    println!("Provider one-time device code: {code}");
                }
            }
            Ok(())
        });
        let mut calls = 0;
        let outcome:Result<Value,String>=async {
            rig_auth::sign_in(&provider,store.clone(),&http,prompt).await?;
            let schema=json!({"type":"object","properties":{"items":{"type":"array","items":{"type":"string"}},"questions":{"type":"array","items":{"type":"string"}},"unknown":{"type":"array","items":{"type":"string"}}},"required":["items","questions","unknown"],"additionalProperties":false});
            let mut body=json!({"model":model,"messages":[{"role":"system","content":"This is a synthetic connection test. Call context_search once, then return the required JSON object. Do not request any other tools."},{"role":"user","content":"Select the synthetic owned-sentence from the owned candidate page."}],"tools":[{"type":"function","function":{"name":"context_search","description":"Read one synthetic owned candidate. No real workspace data.","parameters":{"type":"object","properties":{},"additionalProperties":false}}}],"response_format":{"type":"json_schema","json_schema":{"name":"owned_trial","strict":true,"schema":schema}}});
            let mut observations=vec![];
            for turn in 0..2 {
                calls+=1;
                let response=rig::request(&stored,body.clone(),180000,store.clone()).await?;
                if response["status"]!=200 {return Err("provider_model_http_failure".into())}
                let reply=&response["body"];
                observations.push(json!({"actual_model":reply["model"],"usage":reply["usage"],"request_id":reply["rig_request_id"]}));
                if reply["model"].as_str().is_none_or(str::is_empty) {return Err("actual_model_missing".into())}
                let message=&reply["choices"][0]["message"];
                let tools=message["tool_calls"].as_array().filter(|v|!v.is_empty());
                if let Some(tools)=tools {
                    if turn!=0||tools.len()>4 {return Err("trial_tool_limit".into())}
                    let mut results=vec![];
                    for tool in tools {
                        if tool["function"]["name"]!="context_search" {return Err("unexpected_trial_tool".into())}
                        results.push(json!({"role":"tool","tool_call_id":tool["id"],"content":"{\"items\":[\"owned-sentence\"],\"complete\":true}"}));
                    }
                    body["messages"].as_array_mut().unwrap().push(message.clone());
                    body["messages"].as_array_mut().unwrap().extend(results);
                    body.as_object_mut().unwrap().remove("tools");
                } else {
                    let _:Value=serde_json::from_str(message["content"].as_str().ok_or("trial_content_missing")?).map_err(|_|"trial_structure_invalid")?;
                    return Ok(json!({"provider":provider,"requested_model":model,"model_calls":calls,"tool_roundtrip":turn==1,"observations":observations}));
                }
            }
            Err("trial_request_limit".into())
        }.await;
        let cleanup = rig_auth::sign_out(&provider, store.clone(), &http).await;
        store.delete(&provider).await.unwrap();
        let directory = &store.dir;
        let lock = directory.join(format!("{provider}.lock"));
        if lock.exists() {
            fs::remove_file(lock).unwrap();
        }
        if directory.exists() {
            fs::remove_dir(directory).unwrap();
            fs::remove_dir(root.join("rig-oauth")).unwrap();
            fs::remove_dir(&root).unwrap();
        }
        let report = match &outcome {
            Ok(value) => json!({"trial":value,"cleanup":cleanup.ok()}),
            Err(code) => {
                json!({"provider":provider,"model_calls":calls,"failure":code,"cleanup":cleanup.ok()})
            }
        };
        let destination = Path::new(env!("CARGO_MANIFEST_DIR"))
            .join("../../../test-results/issue26-rig-live.json");
        fs::write(destination, serde_json::to_vec_pretty(&report).unwrap()).unwrap();
        assert!(
            outcome.is_ok(),
            "synthetic provider trial did not complete; see sanitized report"
        );
    });
}
