use super::{Profile, Stored, rig_auth};
use rig_agent::run::{AgentRun, AgentRunStep, ModelTurn, ModelTurnOutcome, TurnPolicy};
use rig_core::{
    completion::{CompletionRequest, CompletionResponse, ToolDefinition},
    message::{AssistantContent, AssistantMessage, CallId, Message, ToolName},
    providers::{
        chatgpt, copilot,
        openai::{OpenAIConfig, Route, wire::Auth},
    },
};
use serde_json::{Value, json};
use std::{
    collections::{BTreeSet, HashMap},
    fs,
    time::Duration,
};
#[path = "rig_transport.rs"]
mod transport;
use transport::Transport;

fn text(value: &Value) -> Result<&str, String> {
    value.as_str().ok_or_else(|| "invalid_model_request".into())
}
fn allowed(name: &str) -> bool {
    matches!(
        name,
        "context_search" | "context_graph" | "context_get" | "context_resolve"
    )
}

fn messages(body: &Value, profile: &Profile) -> Result<Vec<Message>, String> {
    let mut names = HashMap::<String, String>::new();
    body["messages"]
        .as_array()
        .ok_or("invalid_model_request")?
        .iter()
        .map(|m| match text(&m["role"])? {
            "system" => Ok(Message::system(text(&m["content"])?)),
            "user" => Ok(Message::user(text(&m["content"])?)),
            "assistant" => {
                let mut content = vec![];
                if let Some(t) = m["content"].as_str() {
                    content.push(AssistantContent::text(t));
                }
                for call in m["tool_calls"].as_array().into_iter().flatten() {
                    let id = text(&call["id"])?;
                    let name = text(&call["function"]["name"])?;
                    if id.is_empty() || !allowed(name) {
                        return Err("tool_not_allowed".into());
                    }
                    names.insert(id.into(), name.into());
                    content.push(AssistantContent::tool_call(
                        id,
                        ToolName::new(name).map_err(|_| "invalid_tool_name")?,
                        serde_json::from_str(text(&call["function"]["arguments"])?)
                            .map_err(|_| "invalid_tool_arguments")?,
                    ));
                }
                if !m["rig_message"].is_null()
                    && m["rig_connection"] == json!([profile.id, profile.revision])
                {
                    let original: Message = serde_json::from_value(m["rig_message"].clone())
                        .map_err(|_| "invalid_native_model_history")?;
                    if !matches!(original, Message::Assistant(_)) {
                        return Err("invalid_native_model_history".into());
                    }
                    Ok(original)
                } else {
                    Ok(Message::Assistant(AssistantMessage::new(content)))
                }
            }
            "tool" => {
                let id = text(&m["tool_call_id"])?;
                let name = names.get(id).ok_or("unknown_tool_result")?;
                Ok(Message::tool_result(
                    CallId::from_wire(id),
                    ToolName::new(name).map_err(|_| "invalid_tool_name")?,
                    text(&m["content"])?,
                ))
            }
            _ => Err("invalid_model_message".into()),
        })
        .collect()
}

fn tools(body: &Value) -> Result<Vec<ToolDefinition>, String> {
    body["tools"]
        .as_array()
        .into_iter()
        .flatten()
        .map(|tool| {
            let f = &tool["function"];
            let name = text(&f["name"])?;
            if tool["type"] != "function" || !allowed(name) {
                return Err("tool_not_allowed".into());
            }
            Ok(ToolDefinition::new(
                ToolName::new(name).map_err(|_| "invalid_tool_name")?,
                f["description"].as_str().unwrap_or_default(),
                f["parameters"].clone(),
            ))
        })
        .collect()
}

fn output(response: &CompletionResponse, observed: Value, profile: &Profile) -> Value {
    let head = response.head().with_content(response.choice.clone());
    let native = Message::Assistant(head.clone());
    let mut value = observed["body"].clone();
    if value["choices"].is_array() {
        value["choices"][0]["message"]["rig_message"] = json!(native);
    } else {
        let calls:Vec<Value>=head.tool_calls().map(|c|json!({"id":c.id.to_string(),"type":"function","function":{"name":c.function.name.as_str(),"arguments":serde_json::to_string(&c.function.arguments).unwrap()}})).collect();
        let u = response.usage;
        let finish = response
            .finish_reason()
            .map(|v| serde_json::to_value(v).unwrap())
            .unwrap_or_else(|| {
                json!(if calls.is_empty() {
                    "stop"
                } else {
                    "tool_calls"
                })
            });
        value = json!({"id":response.response_id(),"model":response.model(),"choices":[{"message":{"role":"assistant","content":head.text(),"tool_calls":calls,"rig_message":native},"finish_reason":finish}],"usage":{"prompt_tokens":u.input_tokens,"completion_tokens":u.output_tokens,"prompt_tokens_details":{"cached_tokens":u.cached_input_tokens},"completion_tokens_details":{"reasoning_tokens":u.reasoning_tokens}},"provider_raw":observed["body"]});
    }
    value["choices"][0]["message"]["rig_connection"] = json!([profile.id, profile.revision]);
    value["rig_source"] = json!("c73622dfca02bfe55d29a6a0612e181119cab9b8");
    value["rig_provider"] = json!(profile.provider);
    value["rig_endpoint"] = json!(profile.endpoint);
    value["rig_request_id"] = json!(response.provider_request_id);
    json!({"status":observed["status"],"body":value})
}

#[cfg(test)]
#[path = "rig_model_tests.rs"]
mod tests;

pub(super) fn http(profile: &Profile, timeout_ms: u64) -> Result<Transport, String> {
    let target = super::endpoint(&profile.endpoint)?;
    let mut builder = reqwest::Client::builder()
        .redirect(reqwest::redirect::Policy::none())
        .timeout(Duration::from_millis(timeout_ms));
    if matches!(target.host_str(), Some("localhost" | "127.0.0.1")) {
        if let Ok(file) = std::env::var("KOUJOU_MODEL_CA_FILE") {
            builder = builder.add_root_certificate(
                reqwest::Certificate::from_pem(&fs::read(file).map_err(|_| "invalid_model_ca")?)
                    .map_err(|_| "invalid_model_ca")?,
            );
        }
    }
    Ok(Transport::new(
        builder
            .build()
            .map_err(|_| "model_connection_unavailable")?,
        target,
    ))
}

pub(super) async fn request(
    stored: &Stored,
    body: Value,
    timeout_ms: u64,
    store: rig_auth::Store,
) -> Result<Value, String> {
    let started = std::time::Instant::now();
    let config = if stored.profile.provider == "compatible" {
        OpenAIConfig::new(stored.key.clone())
            .with_base_url(&stored.profile.endpoint)
            .with_route(Route::Chat)
            .with_auth(Auth::OptionalBearer)
    } else {
        let auth_http = http(&stored.profile, timeout_ms)?;
        let auth = rig_auth::context(&stored.profile.provider, store, &auth_http).await?;
        let dialect = match stored.profile.provider.as_str() {
            "chatgpt" => &chatgpt::DIALECT,
            "copilot" => &copilot::DIALECT,
            _ => return Err("invalid_model_provider".into()),
        };
        let mut config =
            OpenAIConfig::with_key(dialect, auth.api_key).with_base_url(&stored.profile.endpoint);
        config.account_id = auth.account_id;
        if let Some(base) = auth.api_base {
            let u = super::endpoint(&base)?;
            if u.scheme() != "https"
                || !u
                    .host_str()
                    .is_some_and(|h| h == "githubcopilot.com" || h.ends_with(".githubcopilot.com"))
            {
                return Err("invalid_model_endpoint".into());
            }
            config = config.with_base_url(base);
        }
        config
    };
    let remaining = timeout_ms.saturating_sub(started.elapsed().as_millis() as u64);
    if remaining == 0 {
        return Err("operation_limit".into());
    }
    let mut route = stored.profile.clone();
    route.endpoint = config.base_url.clone();
    let http = http(&route, remaining)?;
    let model = config
        .connect(http.clone())
        .completion(&stored.profile.model);
    let mut history = messages(&body, &stored.profile)?;
    let prompt = history.pop().ok_or("invalid_model_request")?;
    let definitions = tools(&body)?;
    let names: BTreeSet<String> = definitions.iter().map(|t| t.name.as_str().into()).collect();
    // A single shared sending intent authorizes exactly one model effect. The
    // common Host performs MCP and records the next intent before continuing.
    let mut agent = AgentRun::new(prompt).with_history(history).max_turns(1);
    let AgentRunStep::CallModel {
        prompt, history, ..
    } = agent.next_step().map_err(|_| "invalid_model_history")?
    else {
        return Err("invalid_agent_step".into());
    };
    let mut request = CompletionRequest::new(prompt)
        .messages(history)
        .tools(definitions);
    if let Some(schema) = body.pointer("/response_format/json_schema/schema") {
        request = request.output_schema(Some(
            rig_core::schemars::Schema::try_from(schema.clone())
                .map_err(|_| "invalid_model_schema")?,
        ));
    }
    let result = model.call(request).await;
    let observed = http.observed();
    match result {
        Ok(response) => {
            let policy = TurnPolicy::new(names, None, None).map_err(|_| "invalid_tool_policy")?;
            match agent.model_response(ModelTurn::from_policy(&response, policy)) {
                Ok(ModelTurnOutcome::NeedsResolution(_)) | Err(_) => Ok(
                    json!({"status":200,"body":{"error":"invalid_model_turn","provider_raw":observed.map(|v|v["body"].clone())}}),
                ),
                _ => Ok(output(
                    &response,
                    observed.ok_or("missing_model_response")?,
                    &route,
                )),
            }
        }
        Err(_) => observed.ok_or_else(|| "model_connection_unavailable".into()),
    }
}
