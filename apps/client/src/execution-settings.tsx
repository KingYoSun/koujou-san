import { ScreenContent } from "./navigation";
import { useState } from "react";
import { invoke } from "@tauri-apps/api/core";

export function ExecutionSettings({ workspace }: { workspace: string }) {
  const [target, setTarget] = useState("");
  const [program, setProgram] = useState("");
  const [protocol, setProtocol] = useState("plain"),
    [argumentsText, setArguments] = useState("[]");
  const [contextArgs, setContextArgs] = useState("[]"),
    [promptArgs, setPromptArgs] = useState("[]");
  const [resultArgs, setResultArgs] = useState("[]"),
    [versionArgs, setVersionArgs] = useState("[]");
  const [directory, setDirectory] = useState(""),
    [wsl, setWsl] = useState("");
  const [repositories, setRepositories] = useState("{}"),
    [envRefs, setEnvRefs] = useState("{}");
  const [stdinJSON, setStdinJSON] = useState(false),
    [stop, setStop] = useState(false);
  const [busy, setBusy] = useState(false),
    [notice, setNotice] = useState("");
  const [status, setStatus] = useState<unknown>();
  const [billing, setBilling] = useState("unknown");
  const [contract, setContract] = useState("");
  const [usageMode, setUsageMode] = useState("end");
  const [modelCapture, setModelCapture] = useState(false);
  const [nonLLM, setNonLLM] = useState(false);
  async function work(action: () => Promise<void>) {
    setBusy(true);
    setNotice("");
    try {
      await action();
    } catch {
      setNotice(
        "設定・端末の許可・接続状態を確認してください。操作の完了を確認できませんでした。",
      );
    } finally {
      setBusy(false);
    }
  }
  function preset(value: string) {
    setProtocol(value);
    setUsageMode("end");
    setModelCapture(false);
    if (value === "codex-jsonl") {
      setArguments('["exec","--json","--ephemeral","-"]');
      setVersionArgs('["--version"]');
      setResultArgs('["--output-schema","{result_schema_file}"]');
      setStdinJSON(true);
    }
    if (value === "claude-jsonl") {
      setArguments(
        '["--print","--output-format","stream-json","--verbose","--no-session-persistence"]',
      );
      setVersionArgs('["--version"]');
      setResultArgs('["--json-schema","{result_schema}"]');
      setStdinJSON(true);
    }
  }
  return (
    <ScreenContent>
      {
        <details className="team-details">
          <summary>この端末のCLI実行と独立Runner</summary>
          <fieldset disabled={busy}>
            <label>
              実行接続の論理名
              <input
                value={target}
                onChange={(e) => setTarget(e.target.value)}
              />
            </label>
            <label>
              実行するprogramのpath
              <input
                value={program}
                onChange={(e) => setProgram(e.target.value)}
              />
            </label>
            <label>
              出力・対話の接続方式
              <select value={protocol} onChange={(e) => preset(e.target.value)}>
                <option value="plain">通常のCLI</option>
                <option value="jsonl-v1">明示JSONL対話</option>
                <option value="codex-jsonl">Codex exec JSONL</option>
                <option value="claude-jsonl">Claude print JSONL</option>
              </select>
            </label>
            <label>
              CLI引数の接頭辞（JSON配列）
              <textarea
                value={argumentsText}
                onChange={(e) => setArguments(e.target.value)}
              />
            </label>
            <label>
              <input
                type="checkbox"
                checked={stdinJSON}
                onChange={(e) => setStdinJSON(e.target.checked)}
              />
              stdinへcontext/promptのJSON objectを渡す
            </label>
            <label>
              <input
                type="checkbox"
                checked={stop}
                disabled={!!wsl}
                onChange={(e) => setStop(e.target.checked)}
              />
              Windows childの停止を確認済み
            </label>
            <details>
              <summary>入力方式・実行環境</summary>
              <label>
                contextを引数/ファイルで渡す引数（JSON配列）
                <textarea
                  value={contextArgs}
                  onChange={(e) => setContextArgs(e.target.value)}
                />
              </label>
              <label>
                promptを引数/ファイルで渡す引数（JSON配列）
                <textarea
                  value={promptArgs}
                  onChange={(e) => setPromptArgs(e.target.value)}
                />
              </label>
              <label>
                結果schemaを渡す引数（JSON配列）
                <textarea
                  value={resultArgs}
                  onChange={(e) => setResultArgs(e.target.value)}
                />
              </label>
              <label>
                版を確認する引数（JSON配列、任意）
                <textarea
                  value={versionArgs}
                  onChange={(e) => setVersionArgs(e.target.value)}
                />
              </label>
              <label>
                repositoryを使わない場合の作業場所
                <input
                  value={directory}
                  onChange={(e) => setDirectory(e.target.value)}
                />
              </label>
              <label>
                WSL distribution（使う場合）
                <input
                  value={wsl}
                  onChange={(e) => {
                    setWsl(e.target.value);
                    if (e.target.value) setStop(false);
                  }}
                />
              </label>
              <label>
                WSL内repositoryの論理IDとpath（JSON object）
                <textarea
                  value={repositories}
                  onChange={(e) => setRepositories(e.target.value)}
                />
              </label>
              <label>
                子の環境変数名と参照元の変数名（JSON object）
                <textarea
                  value={envRefs}
                  onChange={(e) => setEnvRefs(e.target.value)}
                />
              </label>
            </details>
            <details>
              <summary>利用量の取得と契約</summary>
              <label>
                課金・契約方式
                <select
                  value={billing}
                  onChange={(e) => setBilling(e.target.value)}
                >
                  <option value="unknown">未確認</option>
                  <option value="api">API課金</option>
                  <option value="subscription">定額契約</option>
                  <option value="local">ローカル実行</option>
                </select>
              </label>
              <label>
                個別契約の識別名（秘密値は入力しない）
                <input
                  value={contract}
                  onChange={(e) => setContract(e.target.value)}
                />
              </label>
              <label>
                取得タイミング
                <select
                  value={usageMode}
                  onChange={(e) => setUsageMode(e.target.value)}
                >
                  <option value="end">終了時に取得</option>
                  <option value="live" disabled={protocol !== "claude-jsonl"}>
                    途中更新と終了時（Claude Code）
                  </option>
                </select>
              </label>
              <label>
                <input
                  type="checkbox"
                  checked={nonLLM}
                  onChange={(e) => setNonLLM(e.target.checked)}
                />
                この接続はLLMを使わない
              </label>
              {protocol === "codex-jsonl" && (
                <>
                  <label>
                    <input
                      type="checkbox"
                      checked={modelCapture}
                      onChange={(e) => setModelCapture(e.target.checked)}
                    />
                    Codexのモデルを端末内で計測する
                  </label>
                </>
              )}
            </details>
            <button
              disabled={!target || !program}
              onClick={() =>
                void work(async () => {
                  await invoke("runner_execution_save", {
                    workspace,
                    adapter: {
                      kind: "cli",
                      target,
                      program,
                      protocol,
                      arguments: JSON.parse(argumentsText),
                      context_args: JSON.parse(contextArgs),
                      prompt_args: JSON.parse(promptArgs),
                      result_args: JSON.parse(resultArgs),
                      version_arguments: JSON.parse(versionArgs),
                      stdin_json: stdinJSON,
                      stop_supported: stop,
                      directory,
                      wsl,
                      repositories: JSON.parse(repositories),
                      env_refs: JSON.parse(envRefs),
                      usage: {
                        billing,
                        contract,
                        mode: usageMode,
                        model_capture: modelCapture,
                        non_llm: nonLLM,
                      },
                    },
                  });
                  setNotice(
                    "この端末の実行接続を保存しました。Runは起動していません。",
                  );
                })
              }
            >
              この端末の実行接続を保存
            </button>
            <button
              onClick={() =>
                void work(async () => {
                  await invoke("runner_start", { workspace });
                  setNotice(
                    "独立Runnerの起動を要求しました。状態を更新して確認してください。画面を閉じても許可済みの実行は継続します。",
                  );
                })
              }
            >
              独立Runnerを起動
            </button>
            <button
              onClick={() =>
                void work(async () => {
                  setStatus(await invoke("runner_status", { workspace }));
                })
              }
            >
              独立Runnerの状態を更新
            </button>
            <button
              onClick={() =>
                void work(async () => {
                  await invoke("runner_stop_accepting", { workspace });
                  setNotice(
                    "新しいRunの受付停止を要求しました。実行中のCLIは継続し、未送信の記録を保持します。",
                  );
                })
              }
            >
              新しいRunの受付を停止
            </button>
          </fieldset>
          {status !== undefined && <pre>{JSON.stringify(status, null, 2)}</pre>}
          {notice && <p role="status">{notice}</p>}
        </details>
      }
    </ScreenContent>
  );
}
