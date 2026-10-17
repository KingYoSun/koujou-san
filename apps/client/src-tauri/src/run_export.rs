use crate::auth::Auth;
use serde_json::{Value, json};
use std::{
    fs::OpenOptions,
    io::{Read, Write},
};
use tauri::Manager;

// Memory depends on the copy buffer and the requested identifiers, never the
// total output. Only the server's final fixed-version frame confirms completion.
#[cfg(test)]
fn copy_export(
    source: impl Read,
    destination: impl Write,
    id: &str,
    version: &str,
) -> Result<bool, String> {
    copy_fixed_export(source, destination, id, version, "run")
}
fn copy_fixed_export(
    mut source: impl Read,
    mut destination: impl Write,
    id: &str,
    version: &str,
    root: &str,
) -> Result<bool, String> {
    let mut buffer = [0u8; 65536];
    let mut tail = Vec::new();
    let limit = 4096 + 6 * (id.len() + version.len());
    loop {
        let count = source
            .read(&mut buffer)
            .map_err(|_| "export_stream_failed")?;
        if count == 0 {
            break;
        }
        destination
            .write_all(&buffer[..count])
            .map_err(|_| "export_storage_failed")?;
        tail.extend_from_slice(&buffer[..count]);
        if tail.len() > limit {
            tail.drain(..tail.len() - limit);
        }
    }
    destination.flush().map_err(|_| "export_storage_failed")?;
    let last = tail
        .split(|b| *b == b'\n')
        .rev()
        .find(|line| !line.is_empty())
        .unwrap_or_default();
    let frame: Value = serde_json::from_slice(last).unwrap_or(Value::Null);
    if frame["type"] == "error" && frame["code"] == "forbidden" {
        return Err("forbidden".into());
    }
    Ok(frame["type"] == "complete"
        && frame[root]["id"] == id
        && frame[root]["version"] == version
        && (root == "execution"
            || frame["records"]
                .as_str()
                .is_some_and(|s| s.parse::<u64>().is_ok())))
}

#[tauri::command]
pub async fn export_run(
    app: tauri::AppHandle,
    workspace: String,
    id: String,
    version: String,
) -> Result<Value, String> {
    export_file(app, workspace, id, version, false).await
}
#[tauri::command]
pub async fn export_workflow(
    app: tauri::AppHandle,
    workspace: String,
    id: String,
    version: String,
) -> Result<Value, String> {
    export_file(app, workspace, id, version, true).await
}
async fn export_file(
    app: tauri::AppHandle,
    workspace: String,
    id: String,
    version: String,
    workflow: bool,
) -> Result<Value, String> {
    tauri::async_runtime::spawn_blocking(move || {
        let response=if workflow{app.state::<Auth>().export_response_for(&workspace,&id,&version,true)?}else{app.state::<Auth>().export_response(&workspace,&id,&version)?};
        let directory=app.path().download_dir().map_err(|_|"download_directory_unavailable")?;
        let name=format!("koujou-{}-{}",if workflow{"workflow"}else{"run"},uuid::Uuid::new_v4());
        let partial=directory.join(format!("{name}.partial.ndjson"));
        let complete=directory.join(format!("{name}.ndjson"));
        let file=OpenOptions::new().write(true).create_new(true).open(&partial).map_err(|_|"export_storage_failed")?;
        match copy_fixed_export(response,file,&id,&version,if workflow{"execution"}else{"run"}) {
            Ok(true)=>{std::fs::rename(&partial,&complete).map_err(|_|"export_storage_failed")?;Ok(json!({"complete":true,"path":complete}))},
            result=>Ok(json!({"complete":false,"path":partial,"reason":result.err().unwrap_or_else(||"export_incomplete".into())})),
        }
    }).await.map_err(|_|"export_task_failed")?
}

#[tauri::command]
pub async fn export_definition(
    app: tauri::AppHandle,
    workspace: String,
    id: String,
    version: String,
) -> Result<Value, String> {
    tauri::async_runtime::spawn_blocking(move || {
        if workspace.is_empty()
            || id.is_empty()
            || version.is_empty()
            || version == "head"
            || version == "0"
        {
            return Err("fixed_definition_required".into());
        }
        let mut source_url = reqwest::Url::parse("https://localhost/").unwrap();
        source_url.path_segments_mut().unwrap().clear().extend([
            "workflows",
            "workspaces",
            &workspace,
            "records",
            &id,
        ]);
        source_url
            .query_pairs_mut()
            .append_pair("version", &version)
            .append_pair("format", "source");
        let path = format!("{}?{}", source_url.path(), source_url.query().unwrap());
        let response = app.state::<Auth>().request(&path, "GET", Value::Null)?;
        if response["id"] != id
            || response["version"] != version
            || !matches!(
                response["kind"].as_str(),
                Some("run-definition" | "workflow-definition")
            )
        {
            return Err("invalid_export_response".into());
        }
        let source = response["value_utf8"]
            .as_str()
            .ok_or("invalid_export_response")?;
        let path = app
            .path()
            .download_dir()
            .map_err(|_| "download_directory_unavailable")?
            .join(format!("koujou-definition-{}.json", uuid::Uuid::new_v4()));
        let mut file = OpenOptions::new()
            .write(true)
            .create_new(true)
            .open(&path)
            .map_err(|_| "export_storage_failed")?;
        file.write_all(source.as_bytes())
            .and_then(|_| file.flush())
            .map_err(|_| "export_storage_failed")?;
        Ok(json!({"complete":true,"path":path}))
    })
    .await
    .map_err(|_| "export_task_failed")?
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn streaming_export_requires_final_marker_for_the_selected_version() {
        let workflow =
            br#"{"type":"complete","execution":{"id":"workflow-execution:one","version":"fixed"}}"#;
        assert!(
            copy_fixed_export(
                workflow.as_slice(),
                Vec::new(),
                "workflow-execution:one",
                "fixed",
                "execution"
            )
            .unwrap()
        );
        assert!(
            !copy_fixed_export(
                workflow.as_slice(),
                Vec::new(),
                "workflow-execution:one",
                "other",
                "execution"
            )
            .unwrap()
        );
        let begin = b"{\"type\":\"begin\"}\n";
        let end=b"{\"type\":\"complete\",\"run\":{\"id\":\"run:one\",\"version\":\"fixed\"},\"records\":\"5\"}\n";
        let mut stream = begin.to_vec();
        stream.extend_from_slice(&vec![b' '; 150000]);
        stream.push(b'\n');
        stream.extend_from_slice(end);
        let mut file = Vec::new();
        assert!(copy_export(stream.as_slice(), &mut file, "run:one", "fixed").unwrap());
        assert_eq!(file, stream);
        assert!(!copy_export(begin.as_slice(), Vec::new(), "run:one", "fixed").unwrap());
        assert!(!copy_export(end.as_slice(), Vec::new(), "run:one", "changed").unwrap());
        assert_eq!(
            copy_export(
                b"{\"type\":\"error\",\"code\":\"forbidden\"}\n".as_slice(),
                Vec::new(),
                "run:one",
                "fixed"
            )
            .unwrap_err(),
            "forbidden"
        );
    }
}
