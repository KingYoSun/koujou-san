import { ScreenContent } from "./navigation";
import { useState } from "react";
import { invoke } from "@tauri-apps/api/core";
import { Field, jsonError } from "./field";
import { Disclosure } from "./disclosure";

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
  async function work(subject: string, action: () => Promise<void>) {
    setBusy(true);
    setNotice("");
    try {
      await action();
    } catch {
      setNotice(
        `${subject}できませんでした。設定・端末の許可・接続状態を確認してください。`,
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
  const json = (label: string, value: string, set: (v: string) => void) => (
    <Field
      label={label}
      value={value}
      onChange={set}
      rows={2}
      error={jsonError(value)}
    />
  );
  return (
    <ScreenContent>
      <section className="team-details">
        <h3>この端末のCLI実行</h3>
        <fieldset disabled={busy}>
          <Field
            label="実行接続の論理名"
            value={target}
            onChange={setTarget}
            required
          />
          <Field
            label="実行するprogramのpath"
            value={program}
            onChange={setProgram}
            required
          />
          <label>
            出力・対話の接続方式
            <select value={protocol} onChange={(e) => preset(e.target.value)}>
              <option value="plain">通常のCLI</option>
              <option value="jsonl-v1">明示JSONL対話</option>
              <option value="codex-jsonl">Codex exec JSONL</option>
              <option value="claude-jsonl">Claude print JSONL</option>
            </select>
          </label>
          {json("CLI引数の接頭辞（JSON）", argumentsText, setArguments)}
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
          <Disclosure title="入力方式・実行環境">
            {() => (
              <>
                {json(
                  "contextを引数/ファイルで渡す引数（JSON）",
                  contextArgs,
                  setContextArgs,
                )}
                {json(
                  "promptを引数/ファイルで渡す引数（JSON）",
                  promptArgs,
                  setPromptArgs,
                )}
                {json(
                  "結果schemaを渡す引数（JSON）",
                  resultArgs,
                  setResultArgs,
                )}
                {json("版を確認する引数（JSON）", versionArgs, setVersionArgs)}
                <Field
                  label="repositoryを使わない場合の作業場所"
                  value={directory}
                  onChange={setDirectory}
                />
                <Field
                  label="WSL distribution"
                  value={wsl}
                  onChange={(value) => {
                    setWsl(value);
                    if (value) setStop(false);
                  }}
                />
                {json(
                  "WSL内repositoryの論理IDとpath（JSON）",
                  repositories,
                  setRepositories,
                )}
                {json(
                  "子の環境変数名と参照元の変数名（JSON）",
                  envRefs,
                  setEnvRefs,
                )}
              </>
            )}
          </Disclosure>
          <Disclosure title="利用量の取得と契約">
            {() => (
              <>
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
                <Field
                  label="個別契約の識別名"
                  value={contract}
                  onChange={setContract}
                />
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
                  <label>
                    <input
                      type="checkbox"
                      checked={modelCapture}
                      onChange={(e) => setModelCapture(e.target.checked)}
                    />
                    Codexのモデルを端末内で計測する
                  </label>
                )}
              </>
            )}
          </Disclosure>
          <button
            disabled={!target || !program}
            onClick={() =>
              void work("実行接続を保存", async () => {
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
                setNotice("実行接続を保存しました。");
              })
            }
          >
            実行接続を保存
          </button>
          <button
            onClick={() =>
              void work("独立Runnerを起動", async () => {
                await invoke("runner_start", { workspace });
                setNotice("独立Runnerの起動を要求しました。");
              })
            }
          >
            独立Runnerを起動
          </button>
          <button
            data-variant="secondary"
            aria-busy={busy}
            onClick={() =>
              void work("独立Runnerの状態を取得", async () => {
                setStatus(await invoke("runner_status", { workspace }));
              })
            }
          >
            独立Runnerの状態を取得
          </button>
          <button
            data-variant="secondary"
            onClick={() =>
              void work("Runの受付を停止", async () => {
                await invoke("runner_stop_accepting", { workspace });
                setNotice("Runの受付停止を要求しました。");
              })
            }
          >
            Runの受付を停止
          </button>
        </fieldset>
        {status !== undefined && (
          <Disclosure title="独立Runnerの状態" defaultOpen>
            {() => <pre>{JSON.stringify(status, null, 2)}</pre>}
          </Disclosure>
        )}
        {notice && <p role="status">{notice}</p>}
      </section>
    </ScreenContent>
  );
}
