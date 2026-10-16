use crate::auth::Auth;
use serde::{Deserialize, Serialize};
use serde_json::{Value, json};
use std::{
    collections::BTreeMap,
    fs,
    path::{Path, PathBuf},
    process::Command,
};
use tauri::Manager;
use uuid::Uuid;

#[derive(Clone, Default, Deserialize, Serialize)]
struct Configuration {
    runner_id: String,
    connections: Vec<Value>,
    repositories: BTreeMap<String, PathBuf>,
}
fn config(root: &Path) -> Result<Configuration, String> {
    let file = root.join("configuration.json");
    if !file.exists() {
        return Ok(Configuration {
            runner_id: Uuid::new_v4().to_string(),
            ..Default::default()
        });
    }
    serde_json::from_slice(&fs::read(file).map_err(|_| "runner_configuration_unavailable")?)
        .map_err(|_| "invalid_runner_configuration".into())
}
fn git(repo: &Path, args: &[&str]) -> Result<String, String> {
    let output = Command::new("git")
        .arg("-C")
        .arg(git_path(repo)?)
        .args(args)
        .output()
        .map_err(|_| "git_unavailable")?;
    if !output.status.success() {
        return Err("git_preparation_failed".into());
    }
    Ok(String::from_utf8_lossy(&output.stdout).trim().to_owned())
}
fn git_path(path: &Path) -> Result<String, String> {
    let text = path.to_str().ok_or("invalid_repository_path")?;
    // canonicalize() returns verbatim Windows paths; Git for Windows rejects
    // those as the destination of `worktree add`.
    #[cfg(windows)]
    if let Some(tail) = text.strip_prefix(r"\\?\") {
        return Ok(if let Some(unc) = tail.strip_prefix(r"UNC\") {
            format!(r"\\{unc}")
        } else {
            tail.to_owned()
        });
    }
    Ok(text.to_owned())
}
#[derive(Clone, Deserialize, Serialize, PartialEq, Debug)]
struct Area {
    id: String,
    branch: String,
    commit: String,
}
fn hex(value: &str, n: usize) -> bool {
    value.len() == n
        && value
            .bytes()
            .all(|c| c.is_ascii_digit() || (b'a'..=b'f').contains(&c))
}
fn prepare_area(repo: &Path, root: &Path, area: &Area) -> Result<PathBuf, String> {
    if !hex(&area.id, 32)
        || !hex(&area.commit, 40)
        || area.branch != format!("koujou/run-{}", area.id)
    {
        return Err("invalid_run_area".into());
    }
    let repo = repo.canonicalize().map_err(|_| "repository_unavailable")?;
    let common = git(
        &repo,
        &["rev-parse", "--path-format=absolute", "--git-common-dir"],
    )?;
    fs::create_dir_all(root).map_err(|_| "worktree_directory_failed")?;
    // Keep the application's configured path. Resolving Windows' packaged-app
    // junction expands it past Git's explicit GIT_DIR limit.
    let target = PathBuf::from(git_path(root)?).join(&area.id);
    if target.exists() {
        if git(
            &target,
            &["rev-parse", "--path-format=absolute", "--git-common-dir"],
        )? != common
            || git(&target, &["symbolic-ref", "--short", "HEAD"])? != area.branch
        {
            return Err("run_area_conflict".into());
        }
    } else {
        let target_text = target.to_str().ok_or("invalid_repository_path")?;
        let tip = git(
            &repo,
            &[
                "for-each-ref",
                "--format=%(objectname)",
                &format!("refs/heads/{}", area.branch),
            ],
        )?;
        if tip.is_empty() {
            git(
                &repo,
                &[
                    "worktree",
                    "add",
                    "-b",
                    &area.branch,
                    target_text,
                    &area.commit,
                ],
            )?;
        } else {
            // A failed Git preparation may have created the branch already.
            // Reuse only the expected base; never reset an existing branch.
            if tip != area.commit {
                return Err("run_area_conflict".into());
            }
            git(&repo, &["worktree", "add", target_text, &area.branch])?;
        }
    }
    Ok(target)
}
fn publish(auth: &Auth, workspace: &str, configuration: &Configuration) -> Result<Value, String> {
    let device = auth.runner_request(
        workspace,
        &["access", "workspaces", workspace, "device-state"],
        "GET",
        Value::Null,
        &[],
    )?;
    let id = device["device"]["id"]
        .as_str()
        .ok_or("invalid_device_response")?;
    let current = auth.request(
        &format!("/runs/workspaces/{workspace}/runner/{id}"),
        "GET",
        Value::Null,
    )?;
    auth.runner_request(workspace,&["runs","workspaces",workspace,"runner"],"POST",json!({"runner_id":configuration.runner_id,"connections":configuration.connections,"operation_id":Uuid::new_v4().to_string(),"expected_version":current["version"]}),&[])
}
#[tauri::command]
pub async fn runner_configure(
    app: tauri::AppHandle,
    workspace: String,
    connections: Vec<Value>,
    repositories: BTreeMap<String, PathBuf>,
) -> Result<Value, String> {
    tauri::async_runtime::spawn_blocking(move || {
        let auth = app.state::<Auth>();
        let root = auth.runner_storage(&workspace)?;
        for path in repositories.values() {
            git(path, &["rev-parse", "--show-toplevel"])?;
        }
        let mut configuration = config(&root)?;
        for connection in connections {
            configuration
                .connections
                .retain(|c| c["kind"] != connection["kind"] || c["target"] != connection["target"]);
            configuration.connections.push(connection);
        }
        configuration.repositories.extend(repositories);
        // Save local paths only on this device. A failed publication is retryable.
        fs::create_dir_all(&root).map_err(|_| "runner_configuration_unavailable")?;
        fs::write(
            root.join("configuration.json"),
            serde_json::to_vec(&configuration).map_err(|_| "invalid_runner_configuration")?,
        )
        .map_err(|_| "runner_configuration_unavailable")?;
        publish(&auth, &workspace, &configuration)
    })
    .await
    .map_err(|_| "runner_task_failed")?
}
#[tauri::command]
pub async fn runner_contact(app: tauri::AppHandle, workspace: String) -> Result<Value, String> {
    tauri::async_runtime::spawn_blocking(move || {
        let auth = app.state::<Auth>();
        let root = auth.runner_storage(&workspace)?;
        publish(&auth, &workspace, &config(&root)?)
    })
    .await
    .map_err(|_| "runner_task_failed")?
}
#[tauri::command]
pub async fn prepare_run(
    app: tauri::AppHandle,
    workspace: String,
    id: String,
    generation: String,
) -> Result<Value, String> {
    tauri::async_runtime::spawn_blocking(move||{
        let auth=app.state::<Auth>();
        let root=auth.runner_storage(&workspace)?;let configuration=config(&root)?;
        publish(&auth,&workspace,&configuration)?;
        let head=auth.request(&format!("/runs/workspaces/{workspace}/records/{id}?version=head"),"GET",Value::Null)?;
        let sequence=head["value"]["reported_through"].as_str().ok_or("invalid_assignment")?.parse::<u64>().map_err(|_|"invalid_assignment")?+1;
        auth.runner_request(&workspace,&["runs","workspaces",&workspace,"executions",&id,"report"],"POST",json!({"operation_id":Uuid::new_v4().to_string(),"expected_version":head["version"],"generation":generation,"sequence":sequence.to_string(),"type":"renew"}),&[])?;
        let assigned=auth.runner_request(&workspace,&["runs","workspaces",&workspace,"executions",&id,"assigned-input"],"GET",Value::Null,&[("generation",&generation)])?;
        if assigned["assignment"]["runner_id"]!=configuration.runner_id{return Err("runner_identity_mismatch".into())}
        let a=&assigned["assignment"];
        let profiles=execution::settings(&root)?;
        let adapter=execution::selected(&profiles,&assigned["spec"]["input"]["command"]);
        let path=execution::repository(&assigned,&configuration,adapter,&root)?;
        // Recheck current permission and generation after Git I/O, before reporting readiness.
        let current=auth.runner_request(&workspace,&["runs","workspaces",&workspace,"executions",&id,"assigned-input"],"GET",Value::Null,&[("generation",&generation)])?;
        if current["assignment"]["input"]!=a["input"]||current["assignment"]["area"]!=a["area"]{return Err("assignment_changed".into())}
        if current["assignment"]["prepared_at"].is_null(){
            let sequence=current["reported_through"].as_str().ok_or("invalid_assignment")?.parse::<u64>().map_err(|_|"invalid_assignment")?+1;
            auth.runner_request(&workspace,&["runs","workspaces",&workspace,"executions",&id,"report"],"POST",json!({"operation_id":Uuid::new_v4().to_string(),"expected_version":current["run"]["version"],"generation":generation,"sequence":sequence.to_string(),"type":"prepared","prepared_area":a["area"]}),&[])?;
        }
        Ok(json!({"prepared":true,"path":path,"process_started":false}))
    }).await.map_err(|_|"runner_task_failed")?
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn run_worktrees_and_branches_are_distinct_and_replay_preserves_work() {
        let root = std::env::temp_dir().join(format!("koujou-worktrees-{}", Uuid::new_v4()));
        let repo = root.join("original");
        fs::create_dir_all(&repo).unwrap();
        git(&repo, &["init"]).unwrap();
        fs::write(repo.join("source.txt"), "original").unwrap();
        git(&repo, &["add", "source.txt"]).unwrap();
        git(
            &repo,
            &[
                "-c",
                "user.name=Fixture",
                "-c",
                "user.email=fixture@koujou.test",
                "commit",
                "-m",
                "base",
            ],
        )
        .unwrap();
        let commit = git(&repo, &["rev-parse", "HEAD"]).unwrap();
        let original_branch = git(&repo, &["symbolic-ref", "--short", "HEAD"]).unwrap();
        let a = Area {
            id: "a".repeat(32),
            branch: format!("koujou/run-{}", "a".repeat(32)),
            commit: commit.clone(),
        };
        let b = Area {
            id: "b".repeat(32),
            branch: format!("koujou/run-{}", "b".repeat(32)),
            commit: commit.clone(),
        };
        // The app-data junction has a longer canonical path on packaged Windows.
        // A previous failure can leave the expected branch without a worktree.
        git(&repo, &["branch", &a.branch, &a.commit]).unwrap();
        let first = prepare_area(
            &repo,
            &root.join("native-application-runner").join("areas"),
            &a,
        )
        .unwrap();
        let second = prepare_area(
            &repo,
            &root.join("native-application-runner").join("areas"),
            &b,
        )
        .unwrap();
        assert_ne!(first, second);
        fs::write(first.join("source.txt"), "run one edited").unwrap();
        assert_eq!(
            prepare_area(
                &repo,
                &root.join("native-application-runner").join("areas"),
                &a
            )
            .unwrap(),
            first
        );
        assert_eq!(
            fs::read_to_string(first.join("source.txt")).unwrap(),
            "run one edited"
        );
        assert_eq!(
            fs::read_to_string(second.join("source.txt")).unwrap(),
            "original"
        );
        assert_eq!(
            fs::read_to_string(repo.join("source.txt")).unwrap(),
            "original"
        );
        assert_eq!(git(&repo, &["rev-parse", "HEAD"]).unwrap(), commit);
        assert_eq!(
            git(&repo, &["symbolic-ref", "--short", "HEAD"]).unwrap(),
            original_branch
        );
        let mut invalid = a.clone();
        invalid.id = "../outside".into();
        assert!(
            prepare_area(
                &repo,
                &root.join("native-application-runner").join("areas"),
                &invalid
            )
            .is_err()
        );
        // Only this test's freshly generated UUID directory is disposable.
        assert!(root.starts_with(std::env::temp_dir()));
        fs::remove_dir_all(root).unwrap();
    }
}

mod execution;

#[tauri::command]
pub async fn runner_execution_save(
    app: tauri::AppHandle,
    workspace: String,
    adapter: Value,
) -> Result<Value, String> {
    tauri::async_runtime::spawn_blocking(move || {
        let auth = app.state::<Auth>();
        let root = auth.runner_storage(&workspace)?;
        let adapter: execution::Adapter =
            serde_json::from_value(adapter).map_err(|_| "invalid_execution_configuration")?;
        if adapter.kind.is_empty() || adapter.target.is_empty() || adapter.program.is_empty() {
            return Err("invalid_execution_configuration".into());
        }
        let mut settings = execution::settings(&root)?;
        settings
            .adapters
            .retain(|a| a.kind != adapter.kind || a.target != adapter.target);
        settings.adapters.push(adapter);
        settings.revision = Uuid::new_v4().to_string();
        fs::create_dir_all(&root).map_err(|_| "execution_configuration_unavailable")?;
        let temp = root.join(format!("execution-{}.tmp", Uuid::new_v4()));
        fs::write(
            &temp,
            serde_json::to_vec(&settings).map_err(|_| "invalid_execution_configuration")?,
        )
        .map_err(|_| "execution_configuration_unavailable")?;
        fs::rename(temp, root.join("execution.json"))
            .map_err(|_| "execution_configuration_unavailable")?;
        Ok(json!({"configured":true,"process_started":false}))
    })
    .await
    .map_err(|_| "runner_task_failed")?
}

mod journal;
mod protocol;
mod runtime;
pub(crate) fn background_entry() {
    runtime::main_entry();
}
#[tauri::command]
pub async fn runner_start(app: tauri::AppHandle, workspace: String) -> Result<Value, String> {
    tauri::async_runtime::spawn_blocking(move || runtime::start(&app.state::<Auth>(), &workspace))
        .await
        .map_err(|_| "runner_task_failed")?
}
#[tauri::command]
pub async fn runner_status(app: tauri::AppHandle, workspace: String) -> Result<Value, String> {
    tauri::async_runtime::spawn_blocking(move || runtime::status(&app.state::<Auth>(), &workspace))
        .await
        .map_err(|_| "runner_task_failed")?
}
#[tauri::command]
pub async fn runner_stop_accepting(
    app: tauri::AppHandle,
    workspace: String,
) -> Result<Value, String> {
    tauri::async_runtime::spawn_blocking(move || {
        let root = app.state::<Auth>().runner_storage(&workspace)?;
        fs::create_dir_all(&root).map_err(|_| "runner_directory_failed")?;
        fs::write(root.join("stop-accepting"), b"stop accepting new Runs")
            .map_err(|_| "runner_status_failed")?;
        Ok(json!({"accepting":false,"running_commands_stopped":false}))
    })
    .await
    .map_err(|_| "runner_task_failed")?
}
