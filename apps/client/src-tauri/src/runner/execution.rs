use super::*;
use std::process::Stdio;

#[derive(Clone, Default, Deserialize, Serialize)]
pub(super) struct ExecutionSettings {
    #[serde(default)]
    pub adapters: Vec<Adapter>,
    #[serde(default)]
    pub revision: String,
}
#[derive(Clone, Default, Deserialize, Serialize)]
pub(super) struct Adapter {
    pub kind: String,
    pub target: String,
    pub program: String,
    #[serde(default)]
    pub arguments: Vec<String>,
    #[serde(default)]
    pub context_args: Vec<String>,
    #[serde(default)]
    pub prompt_args: Vec<String>,
    #[serde(default)]
    pub result_args: Vec<String>,
    #[serde(default)]
    pub protocol: String,
    #[serde(default)]
    pub stdin_json: bool,
    #[serde(default)]
    pub stop_supported: bool,
    #[serde(default)]
    pub directory: String,
    #[serde(default)]
    pub wsl: String,
    #[serde(default)]
    pub repositories: BTreeMap<String, String>,
    #[serde(default)]
    pub env_refs: BTreeMap<String, String>,
    #[serde(default)]
    pub version_arguments: Vec<String>,
}
pub(super) fn settings(root: &Path) -> Result<ExecutionSettings, String> {
    let file = root.join("execution.json");
    if !file.exists() {
        return Ok(ExecutionSettings::default());
    }
    serde_json::from_slice(&fs::read(file).map_err(|_| "execution_configuration_unavailable")?)
        .map_err(|_| "invalid_execution_configuration".into())
}
pub(super) fn selected<'a>(
    settings: &'a ExecutionSettings,
    command: &Value,
) -> Option<&'a Adapter> {
    settings.adapters.iter().find(|a| {
        a.kind == command["kind"].as_str().unwrap_or("")
            && a.target == command["target"].as_str().unwrap_or("")
    })
}
fn wsl_command(distro: &str, program: &str, args: &[&str]) -> Result<String, String> {
    let output = Command::new("wsl.exe")
        .args(["-d", distro, "--exec", program])
        .args(args)
        .output()
        .map_err(|_| "wsl_unavailable")?;
    if !output.status.success() {
        return Err("wsl_preparation_failed".into());
    }
    Ok(String::from_utf8_lossy(&output.stdout).trim().to_owned())
}
fn wsl_path(distro: &str, value: &str) -> Result<String, String> {
    if value.starts_with('/') {
        return Ok(value.to_owned());
    }
    // Windows packaged-app redirection/junctions are not followed by DrvFs.
    // Resolve the existing parent when preparing a not-yet-created worktree.
    let path = Path::new(value);
    let physical = path
        .canonicalize()
        .or_else(|_| {
            path.parent()
                .ok_or_else(|| std::io::Error::other("missing parent"))?
                .canonicalize()
                .map(|parent| parent.join(path.file_name().unwrap_or_default()))
        })
        .map_err(|_| "wsl_preparation_failed")?;
    wsl_command(distro, "wslpath", &["-u", &git_path(&physical)?])
}
fn wsl_git(distro: &str, repo: &str, args: &[&str]) -> Result<String, String> {
    let mut all = vec!["-C", repo];
    all.extend(args);
    wsl_command(distro, "git", &all)
}
pub(super) fn repository(
    assigned: &Value,
    config: &Configuration,
    adapter: Option<&Adapter>,
    root: &Path,
) -> Result<Option<PathBuf>, String> {
    let a = &assigned["assignment"];
    if a["repository"].is_null() {
        return Ok(None);
    }
    let id = a["repository"]["id"].as_str().ok_or("invalid_assignment")?;
    let area: Area = serde_json::from_value(a["area"].clone()).map_err(|_| "invalid_assignment")?;
    if !hex(&area.id, 32)
        || !hex(&area.commit, 40)
        || area.branch != format!("koujou/run-{}", area.id)
    {
        return Err("invalid_run_area".into());
    }
    let windows_repo = config.repositories.get(id);
    let Some(adapter) = adapter.filter(|a| !a.wsl.is_empty()) else {
        return prepare_area(
            windows_repo.ok_or("repository_not_configured")?,
            &root.join("worktrees"),
            &area,
        )
        .map(Some);
    };
    let source = if let Some(path) = adapter.repositories.get(id) {
        path.clone()
    } else {
        wsl_path(
            &adapter.wsl,
            &git_path(windows_repo.ok_or("repository_not_configured")?)?,
        )?
    };
    let target = root.join("worktrees").join(&area.id);
    fs::create_dir_all(root.join("worktrees")).map_err(|_| "worktree_directory_failed")?;
    let target_wsl = wsl_path(&adapter.wsl, &git_path(&target)?)?;
    let common = wsl_git(
        &adapter.wsl,
        &source,
        &["rev-parse", "--path-format=absolute", "--git-common-dir"],
    )?;
    if target.exists() {
        if wsl_git(
            &adapter.wsl,
            &target_wsl,
            &["rev-parse", "--path-format=absolute", "--git-common-dir"],
        )? != common
            || wsl_git(
                &adapter.wsl,
                &target_wsl,
                &["symbolic-ref", "--short", "HEAD"],
            )? != area.branch
        {
            return Err("run_area_conflict".into());
        }
    } else {
        let tip = wsl_git(
            &adapter.wsl,
            &source,
            &[
                "for-each-ref",
                "--format=%(objectname)",
                &format!("refs/heads/{}", area.branch),
            ],
        )?;
        if tip.is_empty() {
            wsl_git(
                &adapter.wsl,
                &source,
                &[
                    "worktree",
                    "add",
                    "-b",
                    &area.branch,
                    &target_wsl,
                    &area.commit,
                ],
            )?;
        } else if tip == area.commit {
            wsl_git(
                &adapter.wsl,
                &source,
                &["worktree", "add", &target_wsl, &area.branch],
            )?;
        } else {
            return Err("run_area_conflict".into());
        }
    }
    Ok(Some(target))
}
pub(super) struct Prepared {
    pub command: Command,
    version_command: Option<Command>,
    pub input: Vec<u8>,
    pub protocol: String,
    pub stop_supported: bool,
}
impl Prepared {
    pub fn verify_version(&mut self, expected: &str) -> Result<bool, String> {
        let Some(probe) = self.version_command.as_mut() else {
            return Ok(false);
        };
        let output = probe.output().map_err(|_| "local_configuration_missing")?;
        if !output.status.success()
            || expected.is_empty()
            || !String::from_utf8_lossy(&output.stdout).contains(expected)
        {
            return Err("local_configuration_missing".into());
        }
        Ok(true)
    }
}
fn bytes(value: &Value) -> Vec<u8> {
    if let Some(s) = value.as_str() {
        s.as_bytes().to_vec()
    } else {
        serde_json::to_vec(value).unwrap()
    }
}
// Expand original template tokens once. Input containing another token stays literal.
fn expand(text: &str, values: &BTreeMap<String, String>) -> Result<String, String> {
    let mut out = String::new();
    let mut rest = text;
    while let Some(at) = rest.find('{') {
        out.push_str(&rest[..at]);
        let candidate = &rest[at..];
        if let Some(end) = candidate.find('}') {
            let name = &candidate[1..end];
            if matches!(
                name,
                "context"
                    | "prompt"
                    | "context_file"
                    | "prompt_file"
                    | "result_schema"
                    | "result_schema_file"
            ) {
                out.push_str(values.get(name).ok_or("input_binding_unsupported")?);
                rest = &candidate[end + 1..];
                continue;
            }
        }
        out.push('{');
        rest = &candidate[1..];
    }
    out.push_str(rest);
    Ok(out)
}
pub(super) fn prepare(
    root: &Path,
    assigned: &Value,
    adapter: &Adapter,
    area: Option<&Path>,
) -> Result<Prepared, String> {
    if adapter.program.is_empty()
        || !matches!(
            adapter.protocol.as_str(),
            "" | "plain" | "jsonl-v1" | "codex-jsonl" | "claude-jsonl"
        )
    {
        return Err("local_configuration_missing".into());
    }
    let spec = &assigned["spec"];
    let command = &spec["input"]["command"];
    let mut inputs = BTreeMap::new();
    for name in ["context", "prompt"] {
        let value = if name == "context" && !assigned["manifest"].is_null() {
            let manifest = &assigned["manifest"]["value"];
            if manifest["state"] == "none" {
                continue;
            }
            let text = manifest["input_utf8"]
                .as_str()
                .ok_or("input_binding_unsupported")?;
            Some(Value::String(text.to_owned()))
        } else {
            spec["input"].get(name).cloned()
        };
        if let Some(value) = value {
            inputs.insert(name.to_owned(), value);
        }
    }
    let mut args = adapter.arguments.clone();
    if let Some(params) = command.get("parameters") {
        if let Some(items) = params.as_array() {
            for item in items {
                args.push(item.as_str().ok_or("input_binding_unsupported")?.to_owned())
            }
        } else if !params.is_null() && adapter.protocol != "jsonl-v1" {
            return Err("input_binding_unsupported".into());
        }
    }
    let key = assigned["assignment"]["area"]["id"]
        .as_str()
        .map(str::to_owned)
        .unwrap_or_else(|| {
            use sha2::{Digest, Sha256};
            format!(
                "{:x}",
                Sha256::digest(assigned["run"]["id"].as_str().unwrap_or("").as_bytes())
            )
        });
    let input_root = root.join("inputs").join(&key);
    let schema = &spec["derivation"]["result_schema"];
    let mut values = BTreeMap::new();
    if !schema.is_null() {
        values.insert(
            "result_schema".to_owned(),
            serde_json::to_string(schema).map_err(|_| "input_binding_unsupported")?,
        );
        if adapter
            .result_args
            .iter()
            .any(|arg| arg.contains("{result_schema_file}"))
        {
            fs::create_dir_all(&input_root).map_err(|_| "input_storage_failed")?;
            let path = input_root.join("result-schema.json");
            fs::write(&path, serde_json::to_vec(schema).unwrap())
                .map_err(|_| "input_storage_failed")?;
            let path = git_path(&path)?;
            let path = if adapter.wsl.is_empty() {
                path
            } else {
                wsl_path(&adapter.wsl, &path)?
            };
            values.insert("result_schema_file".to_owned(), path);
        }
        args.extend(adapter.result_args.clone());
    }
    let mut stdin = serde_json::Map::new();
    let mut raw_stdin = None;
    for (name, value) in &inputs {
        match command["binding"][name]
            .as_str()
            .ok_or("input_binding_unsupported")?
        {
            "stdin" => {
                stdin.insert(name.clone(), value.clone());
                raw_stdin = Some(bytes(value));
            }
            "argument" => {
                values.insert(
                    name.clone(),
                    String::from_utf8(bytes(value)).map_err(|_| "input_binding_unsupported")?,
                );
                args.extend(if name == "context" {
                    adapter.context_args.clone()
                } else {
                    adapter.prompt_args.clone()
                });
            }
            "file" => {
                fs::create_dir_all(&input_root).map_err(|_| "input_storage_failed")?;
                let path = input_root.join(format!("{name}.utf8"));
                let data = bytes(value);
                fs::write(&path, data).map_err(|_| "input_storage_failed")?;
                let path = git_path(&path)?;
                let path = if adapter.wsl.is_empty() {
                    path
                } else {
                    wsl_path(&adapter.wsl, &path)?
                };
                values.insert(format!("{name}_file"), path);
                args.extend(if name == "context" {
                    adapter.context_args.clone()
                } else {
                    adapter.prompt_args.clone()
                });
            }
            _ => return Err("input_binding_unsupported".into()),
        }
    }
    for name in values.keys().filter(|n| !n.starts_with("result_schema")) {
        if !args.iter().any(|a| a.contains(&format!("{{{name}}}"))) {
            return Err("input_binding_unsupported".into());
        }
    }
    let args: Vec<String> = args
        .iter()
        .map(|a| expand(a, &values))
        .collect::<Result<_, _>>()?;
    if args.iter().any(|a| a.contains('\0')) {
        return Err("input_binding_unsupported".into());
    }
    let input = if adapter.protocol == "jsonl-v1" {
        let mut data = serde_json::to_vec(
            &json!({"type":"input","input":stdin,"parameters":command["parameters"]}),
        )
        .unwrap();
        data.push(b'\n');
        data
    } else if adapter.stdin_json {
        serde_json::to_vec(&stdin).unwrap()
    } else {
        if stdin.len() > 1 {
            return Err("input_binding_unsupported".into());
        };
        raw_stdin.unwrap_or_default()
    };
    let cwd = if let Some(path) = area {
        git_path(path)?
    } else if !adapter.directory.is_empty() {
        adapter.directory.clone()
    } else {
        let directory = root.join("working").join(&key);
        fs::create_dir_all(&directory).map_err(|_| "input_storage_failed")?;
        git_path(&directory)?
    };
    let child = process_command(adapter, &cwd, &args)?;
    let version_command = if adapter.version_arguments.is_empty() {
        None
    } else {
        let mut probe = process_command(adapter, &cwd, &adapter.version_arguments)?;
        probe.stdin(Stdio::null());
        Some(probe)
    };
    Ok(Prepared {
        command: child,
        version_command,
        input,
        protocol: adapter.protocol.clone(),
        stop_supported: adapter.stop_supported,
    })
}

fn process_command(adapter: &Adapter, cwd: &str, args: &[String]) -> Result<Command, String> {
    let mut child = if adapter.wsl.is_empty() {
        let mut cmd = Command::new(&adapter.program);
        if !cwd.is_empty() {
            cmd.current_dir(&cwd);
        }
        cmd
    } else {
        let mut cmd = Command::new("wsl.exe");
        cmd.args(["-d", &adapter.wsl]);
        if !cwd.is_empty() {
            cmd.args(["--cd", &wsl_path(&adapter.wsl, &cwd)?]);
        }
        cmd.args(["--exec", &adapter.program]);
        cmd
    };
    child.env_clear();
    for name in [
        "PATH",
        "SystemRoot",
        "WINDIR",
        "COMSPEC",
        "PATHEXT",
        "USERPROFILE",
        "HOMEDRIVE",
        "HOMEPATH",
        "APPDATA",
        "LOCALAPPDATA",
        "TEMP",
        "TMP",
        "HOME",
        "LANG",
        "TERM",
    ] {
        if let Some(v) = std::env::var_os(name) {
            child.env(name, v);
        }
    }
    for (name, source) in &adapter.env_refs {
        child.env(
            name,
            std::env::var_os(source).ok_or("local_configuration_missing")?,
        );
    }
    if !adapter.wsl.is_empty() && !adapter.env_refs.is_empty() {
        child.env(
            "WSLENV",
            adapter
                .env_refs
                .keys()
                .cloned()
                .collect::<Vec<_>>()
                .join(":"),
        );
    }
    child
        .args(args)
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .stderr(Stdio::piped());
    #[cfg(windows)]
    {
        use std::os::windows::process::CommandExt;
        child.creation_flags(0x08000000);
    }
    Ok(child)
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    #[ignore = "requires the owned PostgreSQL inspection handoff"]
    fn fixed_inspection_handoff_keeps_commits_and_inputs_separate() {
        use std::io::Write;
        let fixture = std::env::var("KOUJOU_TEST_INSPECTION_FIXTURE").unwrap();
        let handoff: Value = serde_json::from_slice(&fs::read(fixture).unwrap()).unwrap();
        let original = PathBuf::from(handoff["source"].as_str().unwrap());
        let before = git(&original, &["status", "--porcelain"]).unwrap();
        let commit = git(&original, &["rev-parse", "HEAD"]).unwrap();
        let root = std::env::temp_dir().join(format!("koujou-inspection-{}", Uuid::new_v4()));
        fs::create_dir_all(&root).unwrap();
        let script = r#"const fs=require('node:fs');let input='';process.stdin.on('data',b=>input+=b);process.stdin.on('end',()=>{const value=JSON.parse(input);const artifact=fs.readFileSync('artifact.txt','utf8');if(artifact!=='FIXED-INSPECTION-ARTIFACT\n')process.exit(2);for(const name of ['uncommitted.txt','cli-auth.txt','implementation-history.txt'])if(fs.existsSync(name))process.exit(3);console.log(JSON.stringify({observed_input:value,artifact,report:{inspection:value.prompt.inspection,manifest_digest:value.prompt.manifest_digest,verdict:'PASS',criteria_results:value.prompt.criteria.map(c=>({criterion_id:c.id,verdict:'PASS',executed:true,evidence_refs:[],unverified:[]})),unverified:[]}}));});"#;
        let adapter = Adapter {
            program: "node".into(),
            arguments: vec!["-e".into(), script.into()],
            stdin_json: true,
            protocol: "plain".into(),
            ..Default::default()
        };
        let mut observed = Vec::new();
        for (index, assigned) in handoff["assignments"]
            .as_array()
            .unwrap()
            .iter()
            .enumerate()
        {
            let storage = root.join(format!("device-{index}"));
            fs::create_dir_all(&storage).unwrap();
            let repo = storage.join("repository");
            git(
                &storage,
                &[
                    "clone",
                    "--no-local",
                    &git_path(&original).unwrap(),
                    &git_path(&repo).unwrap(),
                ],
            )
            .unwrap();
            let area = Area {
                id: assigned["assignment"]["area"]["id"]
                    .as_str()
                    .unwrap()
                    .into(),
                branch: assigned["assignment"]["area"]["branch"]
                    .as_str()
                    .unwrap()
                    .into(),
                commit: assigned["assignment"]["area"]["commit"]
                    .as_str()
                    .unwrap()
                    .into(),
            };
            assert_eq!(area.commit, commit);
            let worktree = prepare_area(&repo, &storage.join("areas"), &area).unwrap();
            let mut prepared = prepare(&storage, assigned, &adapter, Some(&worktree)).unwrap();
            let mut child = prepared.command.spawn().unwrap();
            child
                .stdin
                .take()
                .unwrap()
                .write_all(&prepared.input)
                .unwrap();
            let result = child.wait_with_output().unwrap();
            assert!(result.status.success(), "owned inspector failed");
            let result: Value = serde_json::from_slice(&result.stdout).unwrap();
            assert_eq!(
                result["observed_input"],
                Value::Object(
                    assigned["spec"]["input"]
                        .as_object()
                        .unwrap()
                        .iter()
                        .filter(|(key, _)| key.as_str() != "command")
                        .map(|(k, v)| (k.clone(), v.clone()))
                        .collect::<serde_json::Map<_, _>>()
                )
            );
            assert_eq!(git(&worktree, &["rev-parse", "HEAD"]).unwrap(), commit);
            assert!(
                git(&worktree, &["status", "--porcelain"])
                    .unwrap()
                    .is_empty()
            );
            observed.push(result);
        }
        assert!(observed[0]["observed_input"].get("context").is_none());
        assert!(observed[1]["observed_input"].get("context").is_some());
        assert_eq!(git(&original, &["status", "--porcelain"]).unwrap(), before);
        assert_eq!(git(&original, &["rev-parse", "HEAD"]).unwrap(), commit);
        assert!(root.starts_with(std::env::temp_dir()));
        fs::remove_dir_all(root).unwrap();
    }
    #[test]
    fn declared_inputs_keep_values_and_reject_unsupported_routes() {
        let root = std::env::temp_dir().join(format!("koujou-input-{}", Uuid::new_v4()));
        let base = json!({"run":{"id":"run:fixed"},"assignment":{},"spec":{"input":{"command":{"binding":{"context":"stdin","prompt":"stdin"}},"context":{"order":[2,1]},"prompt":"value {context} 日本語"}}});
        let mut adapter = Adapter {
            program: "unused-program".into(),
            protocol: "plain".into(),
            stdin_json: true,
            ..Default::default()
        };
        let prepared = prepare(&root, &base, &adapter, None).unwrap();
        let value: Value = serde_json::from_slice(&prepared.input).unwrap();
        assert_eq!(
            value,
            json!({"context":{"order":[2,1]},"prompt":"value {context} 日本語"})
        );
        let mut precise = base.clone();
        precise["spec"]["input"]["context"] =
            serde_json::from_str("123456789012345678901234567890").unwrap();
        let precise_input = prepare(&root, &precise, &adapter, None).unwrap().input;
        assert!(
            String::from_utf8(precise_input)
                .unwrap()
                .contains("123456789012345678901234567890")
        );
        let mut manifest = base.clone();
        let original = " {\"text\":\"固定本文\", \"order\":[2, 1]}\n";
        manifest["spec"]["input"]
            .as_object_mut()
            .unwrap()
            .remove("context");
        manifest["manifest"] = json!({"value":{"input_utf8":original}});
        let manifest_input = prepare(&root, &manifest, &adapter, None).unwrap().input;
        let manifest_input: Value = serde_json::from_slice(&manifest_input).unwrap();
        assert_eq!(manifest_input["context"], original);
        manifest["manifest"]["value"] = json!({"state":"none"});
        let none_input = prepare(&root, &manifest, &adapter, None).unwrap().input;
        let none_input: Value = serde_json::from_slice(&none_input).unwrap();
        assert_eq!(none_input, json!({"prompt":"value {context} 日本語"}));
        adapter.stdin_json = false;
        assert!(prepare(&root, &base, &adapter, None).is_err());
        let mut args = base.clone();
        args["spec"]["input"]["command"]["binding"] = json!({"context":"file","prompt":"argument"});
        adapter.context_args = vec!["--context".into(), "{context_file}".into()];
        adapter.prompt_args = vec!["--prompt".into(), "{prompt}".into()];
        let p = prepare(&root, &args, &adapter, None).unwrap();
        let argv: Vec<_> = p
            .command
            .get_args()
            .map(|s| s.to_string_lossy().into_owned())
            .collect();
        assert_eq!(argv.last().unwrap(), "value {context} 日本語");
        let path = PathBuf::from(&argv[1]);
        assert_eq!(
            serde_json::from_slice::<Value>(&fs::read(path).unwrap()).unwrap(),
            json!({"order":[2,1]})
        );
        let mut omitted = base.clone();
        omitted["spec"]["input"] = json!({"command":{}});
        assert!(
            prepare(&root, &omitted, &adapter, None)
                .unwrap()
                .input
                .is_empty()
        );
        assert!(root.starts_with(std::env::temp_dir()));
        fs::remove_dir_all(root).unwrap();
    }
}
