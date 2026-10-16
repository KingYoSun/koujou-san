import { useRef, useState } from "react";
import { invoke } from "@tauri-apps/api/core";
import { syncRequest } from "./auth";
import { SyncHTTPError } from "./sync-error";
import { RunnerSettings } from "./runner-settings";
import { ExecutionSettings } from "./execution-settings";

type Ref = { id: string; version: string };
type RecordVersion = Ref & { run_id: string; kind: string; value: unknown };
type Page = {
  run?: Ref;
  items: RecordVersion[];
  next: string;
  snapshot?: string;
};
type Assignment = {
  generation: string;
  device_id: string;
  runner_id: string;
  requested_by: string;
  connection: {
    target: string;
    version: string;
    environment: string;
    subject: string;
  };
  input: Ref;
  repository?: { id: string; commit: string };
  area?: { id: string; branch: string; commit: string };
  expires_at: string;
  prepared_at?: string;
};
type Run = {
  active_pause?: Ref;
  execution?: {
    environment: string;
    version: string;
    version_observed: boolean;
    pause: boolean;
    stop: boolean;
  };
  assignment?: Assignment;
  preparation?: string;
  name?: string;
  input: Ref;
  previous?: Ref;
  state: string;
  observation: string;
  reported_through: string;
  execution_requested_at?: string;
  started_at?: string;
  finished_at?: string;
  cancel_requested: boolean;
  exit_code?: number;
  assessment?: { state: string; verdict?: string; report?: unknown };
};
type InputSpec = {
  input: { context?: unknown; prompt?: unknown; command: unknown };
  manifest?: { id: string; revision: string };
  derivation?: unknown;
  sources?: Ref[];
  artifacts?: unknown[];
};
type Pause = {
  response_kind?: string;
  response_origin?: { kind: string; id: string; source?: string };
  id: string;
  reason: string;
  request: unknown;
  state: string;
  response?: unknown;
  answered_by?: string;
  answered_at?: string;
  delivered_at?: string;
  resumed_at?: string;
};
const stateNames: Record<string, string> = {
  queued: "起動前",
  running: "実行中",
  paused: "質問・条件待ち",
  succeeded: "正常終了",
  failed: "実行失敗",
  cancelled: "取消確認済み",
  waiting: "未回答",
  received: "回答受領済み",
  delivered: "配送確認済み",
  resolved: "再開確認済み",
};
const show = (value: unknown) => JSON.stringify(value, null, 2);

export function RunStatus({ value }: { value: Run }) {
  return (
    <>
      <p>
        実行: {stateNames[value.state] ?? value.state} / 観測:{" "}
        {value.observation === "unknown"
          ? "不明（最後に確認した実行状態を表示）"
          : "確認済み"}
      </p>
      {value.exit_code !== undefined && <p>終了コード: {value.exit_code}</p>}
      {value.cancel_requested && value.state !== "cancelled" && (
        <p>取消要求あり。取消の完了は確認していません。</p>
      )}
      {value.execution && (
        <p>
          実行環境: {value.execution.environment} / 接続版:{" "}
          {value.execution.version}（
          {value.execution.version_observed ? "起動前に確認" : "接続の申告値"}
          ）/ 対話: {value.execution.pause ? "対応" : "未対応"} / 停止:{" "}
          {value.execution.stop ? "対応" : "未対応"}
        </p>
      )}
      {value.cancel_requested &&
        value.execution &&
        !value.execution.stop &&
        ["queued", "running", "paused"].includes(value.state) && (
          <p>この接続は停止に対応していません。CLIの実終了を待ちます。</p>
        )}
      {value.assessment && (
        <p>
          派生契約の確認:{" "}
          {value.assessment.state === "invalid"
            ? "形式不正・判定を利用できません"
            : "適合"}{" "}
          / 判定: {value.assessment.verdict ?? "指定なし"}
        </p>
      )}
      <p>
        最後に受理した報告: {value.reported_through} / 起動確認:{" "}
        {value.started_at ?? "未確認"}
      </p>
    </>
  );
}

export function RunRecords({
  native,
  workspace,
  user,
  role,
  canExecute,
}: {
  native: boolean;
  workspace: string;
  user: string;
  role: string;
  canExecute: boolean;
}) {
  const [deviceID, setDeviceID] = useState(""),
    [repoID, setRepoID] = useState("");
  const [commit, setCommit] = useState(""),
    [required, setRequired] = useState("");
  const base = `/runs/workspaces/${encodeURIComponent(workspace)}`;
  const [busy, setBusy] = useState(false),
    [notice, setNotice] = useState("");
  const [id, setID] = useState(""),
    [current, setCurrent] = useState<RecordVersion | null>(null);
  const [list, setList] = useState<Page | null>(null),
    [page, setPage] = useState<Page | null>(null);
  const [kind, setKind] = useState("pause"),
    [input, setInput] = useState<InputSpec | null>(null);
  const [contextText, setContextText] = useState<string | undefined>();
  const [answer, setAnswer] = useState(""),
    [jsonAnswer, setJsonAnswer] = useState(false);
  const [name, setName] = useState(""),
    [commandKind, setCommandKind] = useState("cli");
  const [target, setTarget] = useState(""),
    [parameters, setParameters] = useState("[]");
  const [prompt, setPrompt] = useState(""),
    [promptBinding, setPromptBinding] = useState("stdin");
  const [manifestID, setManifestID] = useState(""),
    [manifestVersion, setManifestVersion] = useState("");
  const [contextBinding, setContextBinding] = useState("stdin"),
    [previous, setPrevious] = useState<Ref | null>(null);
  const [newID, setNewID] = useState<string>(() => crypto.randomUUID());
  const pending = useRef<{ key: string; id: string } | null>(null);
  const runValue = current?.value as Run | undefined;
  const canEdit = role !== "viewer";
  const call = (path: string, body?: unknown) =>
    syncRequest(`${base}${path}`, native, {
      method: body === undefined ? "GET" : "POST",
      body,
    });
  const query = async (request: unknown) =>
    ((await call("/query", request)) as { data: Page }).data;
  const read = async (ref: Ref) =>
    (await call(
      `/records/${encodeURIComponent(ref.id)}?version=${encodeURIComponent(ref.version)}`,
    )) as RecordVersion;
  const run = async (work: () => Promise<void>) => {
    setBusy(true);
    setNotice("");
    try {
      await work();
    } catch (error) {
      setPage(null);
      setList(null);
      setCurrent(null);
      setInput(null);
      setContextText(undefined);
      if (
        (error instanceof SyncHTTPError &&
          (error.status === 401 || error.status === 403)) ||
        /forbidden|authentication_required/.test(String(error))
      ) {
        setAnswer("");
        pending.current = null;
        setNotice("現在の閲覧・編集・実行依頼の権限を確認してください。");
      } else
        setNotice(
          "操作を完了できませんでした。入力・接続・現在の版を確認してください。",
        );
    } finally {
      setBusy(false);
    }
  };
  const mutate = async (
    runID: string,
    action: string,
    version: string,
    value: object = {},
  ) => {
    const path = `/executions/${encodeURIComponent(runID)}/${action}`;
    const key = show({ path, version, value });
    if (pending.current?.key !== key)
      pending.current = { key, id: crypto.randomUUID() };
    return (await call(path, {
      ...value,
      operation_id: pending.current.id,
      expected_version: version,
    })) as { data: Ref };
  };
  const load = async (runID: string) => {
    const record = await read({
      id: runID.startsWith("run:") ? runID : `run:${runID}`,
      version: "head",
    });
    setID(record.id);
    setCurrent(record);
    setPage(null);
    setInput(null);
    setContextText(undefined);
    setAnswer("");
  };
  const records = async (after = "") => {
    if (!current) return;
    setPage(
      await query({
        run_id: current.id,
        version: current.version,
        kind,
        after,
        limit: 50,
        snapshot: after ? page?.snapshot : undefined,
      }),
    );
  };
  const loadInput = async (ref: Ref) => {
    const spec = (await read(ref)).value as InputSpec;
    let text =
      spec.input.context === undefined
        ? undefined
        : typeof spec.input.context === "string"
          ? spec.input.context
          : show(spec.input.context);
    if (spec.manifest) {
      const manifest = (await syncRequest(
        `/contexts/workspaces/${encodeURIComponent(workspace)}/manifest/${encodeURIComponent(spec.manifest.id)}?revision=${encodeURIComponent(spec.manifest.revision)}`,
        native,
        { method: "GET" },
      )) as { data: { value: { input_utf8?: string } } };
      text = manifest.data.value.input_utf8;
    }
    setInput(spec);
    setContextText(text);
  };
  const create = async () => {
    let runID = newID;
    let manifest: { kind: string; id: string; revision: string } | undefined;
    if (manifestID || manifestVersion) {
      if (!manifestID || !manifestVersion)
        throw Error("fixed Manifest required");
      const resolved = (await syncRequest(
        `/contexts/workspaces/${encodeURIComponent(workspace)}/manifest/${encodeURIComponent(manifestID)}?revision=${encodeURIComponent(manifestVersion)}`,
        native,
        { method: "GET" },
      )) as { data: { value: { run_id: string } } };
      runID = resolved.data.value.run_id;
      manifest = {
        kind: "manifest",
        id: manifestID,
        revision: manifestVersion,
      };
    }
    const binding: Record<string, string> = {};
    if (manifest) binding.context = contextBinding;
    if (prompt) binding.prompt = promptBinding;
    const result = await mutate(runID, "create", "0", {
      spec: {
        name,
        input: {
          ...(prompt ? { prompt } : {}),
          command: {
            kind: commandKind,
            target,
            parameters: JSON.parse(parameters),
            binding,
          },
        },
        ...(manifest ? { manifest } : {}),
        ...(previous ? { previous } : {}),
      },
    });
    await load(result.data.id);
    setNewID(crypto.randomUUID());
    setNotice("Runを作成しました。開始は依頼していません。");
  };
  return (
    <details className="team-details">
      <summary>Run・入力・Pause・出力</summary>
      <p>
        入力・回答・割当と、準備完了・プロセス開始を分けて記録します。実CLIは有効な割当と端末内の接続設定を照合して起動します。
      </p>
      <div className="team-controls">
        <button
          disabled={busy}
          onClick={() =>
            void run(async () =>
              setList(await query({ kind: "run", limit: 50 })),
            )
          }
        >
          Run一覧を取得
        </button>
        <label>
          Run ID
          <input
            value={id}
            onChange={(e) => {
              setID(e.target.value);
              setCurrent(null);
              setPage(null);
              setInput(null);
              setContextText(undefined);
            }}
          />
        </label>
        <button disabled={busy || !id} onClick={() => void run(() => load(id))}>
          Runを取得・更新
        </button>
      </div>
      {list && (
        <>
          <ul>
            {list.items.map((row) => (
              <li key={row.id}>
                <button
                  disabled={busy}
                  onClick={() => void run(() => load(row.id))}
                >
                  {(row.value as Run).name || row.id} ·{" "}
                  {stateNames[(row.value as Run).state] ??
                    (row.value as Run).state}
                </button>
              </li>
            ))}
          </ul>
          {list.next && (
            <button
              disabled={busy}
              onClick={() =>
                void run(async () =>
                  setList(
                    await query({ kind: "run", after: list.next, limit: 50 }),
                  ),
                )
              }
            >
              Run一覧の次のページ
            </button>
          )}
        </>
      )}
      {current && runValue && (
        <>
          <h3>{runValue.name || current.id}</h3>
          <p>
            {current.id} · {current.version}
          </p>
          <RunStatus value={runValue} />
          {runValue.preparation && (
            <p role="status">
              準備待ち・設定不足:{" "}
              {(
                {
                  device_absent: "端末の接続未確認",
                  connection_missing: "接続設定なし",
                  capability_unconfirmed: "能力未確認",
                  capability_missing: "必要能力が不足",
                  binding_missing: "入力方式が未対応",
                  local_configuration_missing:
                    "端末内のprogram・接続版・作業場所を確認してください",
                  input_binding_unsupported: "端末の接続が入力方式を扱えません",
                  process_start_failed: "端末でプロセスを起動できませんでした",
                } as Record<string, string>
              )[runValue.preparation] ?? runValue.preparation}
            </p>
          )}
          {runValue.assignment && (
            <section aria-label="Runの割当">
              <h4>実行担当</h4>
              <p>
                依頼者: {runValue.assignment.requested_by} / 端末:{" "}
                {runValue.assignment.device_id} / Runner:{" "}
                {runValue.assignment.runner_id}
              </p>
              <p>
                接続: {runValue.assignment.connection.target} ·{" "}
                {runValue.assignment.connection.version} / 環境:{" "}
                {runValue.assignment.connection.environment} / CLI主体:{" "}
                {runValue.assignment.connection.subject}
              </p>
              <p>
                割当世代: {runValue.assignment.generation} / 有効期限:{" "}
                {runValue.assignment.expires_at}
                。期限切れはCLIの停止確認を意味しません。
              </p>
              <p>準備完了: {runValue.assignment.prepared_at ?? "未確認"}</p>
              {runValue.assignment.area && (
                <p>
                  Run専用branch: {runValue.assignment.area.branch} / 領域:{" "}
                  {runValue.assignment.area.id} / 固定commit:{" "}
                  {runValue.assignment.area.commit}
                </p>
              )}
              {native && (
                <button
                  disabled={busy}
                  onClick={() =>
                    void run(async () => {
                      const result = await invoke<{ path?: string }>(
                        "prepare_run",
                        {
                          workspace,
                          id: current.id,
                          generation: runValue.assignment!.generation,
                        },
                      );
                      await load(current.id);
                      setNotice(
                        `この端末の準備を確認しました。プロセスは起動していません。${result.path ? ` worktree: ${result.path}` : ""}`,
                      );
                    })
                  }
                >
                  この端末で割当を確認・準備
                </button>
              )}
            </section>
          )}
          {canExecute &&
            runValue.state === "queued" &&
            !runValue.assignment && (
              <details>
                <summary>担当端末へ割り当てる</summary>
                <label>
                  担当端末ID
                  <input
                    value={deviceID}
                    onChange={(e) => setDeviceID(e.target.value)}
                  />
                </label>
                <label>
                  repositoryの論理ID（任意）
                  <input
                    value={repoID}
                    onChange={(e) => setRepoID(e.target.value)}
                  />
                </label>
                {repoID && (
                  <label>
                    作成元の固定commit
                    <input
                      value={commit}
                      onChange={(e) => setCommit(e.target.value)}
                    />
                  </label>
                )}
                <label>
                  必要な接続能力（空白区切り、任意）
                  <input
                    value={required}
                    onChange={(e) => setRequired(e.target.value)}
                  />
                </label>
                <p>
                  端末の現在の許可と確認済み能力を照合します。repositoryを使うRunには別worktree・branchを自動作成します。割当だけではCLIは起動しません。
                </p>
                <button
                  disabled={busy || !deviceID || (!!repoID && !commit)}
                  onClick={() =>
                    void run(async () => {
                      await mutate(current.id, "assign", current.version, {
                        device_id: deviceID,
                        required: required.split(/\s+/).filter(Boolean),
                        ...(repoID
                          ? { repository: { id: repoID, commit } }
                          : {}),
                      });
                      await load(current.id);
                      setNotice(
                        "割当要求を確認しました。準備状況と担当端末を確認してください。",
                      );
                    })
                  }
                >
                  端末能力を確認して割当を依頼
                </button>
              </details>
            )}
          {runValue.previous && (
            <p>
              関連元: {runValue.previous.id} · {runValue.previous.version}
            </p>
          )}
          <div className="team-controls">
            <button
              disabled={busy}
              onClick={() => void run(() => loadInput(runValue.input))}
            >
              固定したinput・派生契約を取得
            </button>
            {canEdit && (
              <button
                disabled={busy}
                onClick={() =>
                  setPrevious({ id: current.id, version: current.version })
                }
              >
                現在のRunを新しいRunの関連元にする
              </button>
            )}
            {canExecute && !runValue.execution_requested_at && (
              <button
                disabled={busy}
                onClick={() =>
                  void run(async () => {
                    await mutate(current.id, "request-start", current.version);
                    await load(current.id);
                    setNotice(
                      "入力確定・開始依頼を保存しました。起動はまだ確認していません。",
                    );
                  })
                }
              >
                入力確定・開始依頼
              </button>
            )}
            {canExecute &&
              !["succeeded", "failed", "cancelled"].includes(
                runValue.state,
              ) && (
                <button
                  disabled={busy || runValue.cancel_requested}
                  onClick={() =>
                    void run(async () => {
                      await mutate(current.id, "cancel", current.version);
                      await load(current.id);
                      setNotice("取消を要求しました。完了の確認を待ちます。");
                    })
                  }
                >
                  取消を要求
                </button>
              )}
            {native ? (
              <button
                disabled={busy}
                onClick={() =>
                  void run(async () => {
                    const saved = await invoke<{
                      complete: boolean;
                      path: string;
                      reason?: string;
                    }>("export_run", {
                      workspace,
                      id: current.id,
                      version: current.version,
                    });
                    if (
                      !saved.complete &&
                      /forbidden|authentication_required/.test(
                        saved.reason ?? "",
                      )
                    )
                      throw new Error(saved.reason);
                    setNotice(
                      saved.complete
                        ? `実行記録をダウンロードフォルダーへ保存しました: ${saved.path}`
                        : `書き出しは未完了です。部分ファイル: ${saved.path}`,
                    );
                  })
                }
              >
                この固定版の実行記録を書き出す
              </button>
            ) : (
              <a
                href={`${base}/executions/${encodeURIComponent(current.id)}/export?version=${encodeURIComponent(current.version)}`}
                download
              >
                この固定版の実行記録を書き出す
              </a>
            )}
          </div>
          {input && (
            <section aria-label="確定したRun入力">
              <h4>input</h4>
              <pre>{show(input.input.command)}</pre>
              {contextText !== undefined ? (
                <details>
                  <summary>注入したContext本文</summary>
                  <textarea
                    aria-label="注入したContext本文"
                    readOnly
                    value={contextText}
                    rows={8}
                  />
                </details>
              ) : (
                <p>Contextなし</p>
              )}
              {input.input.prompt !== undefined ? (
                <details>
                  <summary>prompt</summary>
                  <textarea
                    aria-label="Runのprompt"
                    readOnly
                    value={
                      typeof input.input.prompt === "string"
                        ? input.input.prompt
                        : show(input.input.prompt)
                    }
                    rows={5}
                  />
                </details>
              ) : (
                <p>promptなし</p>
              )}
              {input.manifest && (
                <p>
                  Manifest: {input.manifest.id} · {input.manifest.revision}
                </p>
              )}
              <details>
                <summary>派生契約・固定成果物・由来</summary>
                <pre>
                  {show({
                    derivation: input.derivation,
                    artifacts: input.artifacts,
                    sources: input.sources,
                  })}
                </pre>
              </details>
            </section>
          )}
          <div className="team-controls">
            <label>
              Runの記録
              <select
                value={kind}
                onChange={(e) => {
                  setKind(e.target.value);
                  setPage(null);
                }}
              >
                <option value="pause">質問と回答</option>
                <option value="pause-history">質問・回答の版履歴</option>
                <option value="output">原出力</option>
                <option value="event">報告履歴</option>
                <option value="result">確定結果</option>
                <option value="related">後続Runとの関連</option>
              </select>
            </label>
            <button disabled={busy} onClick={() => void run(() => records())}>
              Runの記録を取得
            </button>
          </div>
          {page && (
            <>
              <ul>
                {page.items.map((record) => {
                  const pause = record.value as Pause;
                  return (
                    <li key={`${record.id}:${record.version}`}>
                      {record.kind === "pause" ? (
                        <>
                          <p>
                            {pause.reason} /{" "}
                            {pause.response_kind === "event" &&
                            pause.state === "waiting"
                              ? "外部イベント待ち"
                              : (stateNames[pause.state] ?? pause.state)}
                          </p>
                          <pre>{show(pause.request)}</pre>
                          {pause.response !== undefined && (
                            <>
                              {pause.response_origin?.kind === "event" && (
                                <p>
                                  観測元: {pause.response_origin.source} /{" "}
                                  {pause.response_origin.id}
                                </p>
                              )}
                              <p>
                                回答者: {pause.answered_by} /{" "}
                                {pause.answered_at}
                              </p>
                              <pre>{show(pause.response)}</pre>
                              <p>
                                配送確認: {pause.delivered_at ?? "未確認"} /
                                再開確認: {pause.resumed_at ?? "未確認"}
                              </p>
                            </>
                          )}
                          {canEdit &&
                            kind === "pause" &&
                            pause.state === "waiting" &&
                            pause.response_kind !== "event" && (
                              <fieldset disabled={busy}>
                                <label>
                                  質問への回答
                                  <textarea
                                    value={answer}
                                    onChange={(e) => setAnswer(e.target.value)}
                                    rows={3}
                                  />
                                </label>
                                <label>
                                  <input
                                    type="checkbox"
                                    checked={jsonAnswer}
                                    onChange={(e) =>
                                      setJsonAnswer(e.target.checked)
                                    }
                                  />
                                  構造化JSONで回答
                                </label>
                                <button
                                  disabled={!answer}
                                  onClick={() =>
                                    void run(async () => {
                                      const latest = await read({
                                        id: current.id,
                                        version: "head",
                                      });
                                      const latestRun = latest.value as Run;
                                      if (
                                        latestRun.state !== "paused" ||
                                        latestRun.active_pause?.id !== record.id
                                      )
                                        throw Error("pause_changed");
                                      await mutate(
                                        current.id,
                                        "answer",
                                        latest.version,
                                        {
                                          pause_id: pause.id,
                                          value: jsonAnswer
                                            ? JSON.parse(answer)
                                            : answer,
                                        },
                                      );
                                      await load(current.id);
                                      setNotice(
                                        "回答を受領しました。配送と実行再開はまだ確認していません。",
                                      );
                                    })
                                  }
                                >
                                  回答を送信
                                </button>
                              </fieldset>
                            )}
                        </>
                      ) : (
                        <details>
                          <summary>
                            {record.kind} · {record.id} · {record.version}
                          </summary>
                          <pre>{show(record.value)}</pre>
                        </details>
                      )}
                    </li>
                  );
                })}
              </ul>
              {page.next && (
                <button
                  disabled={busy}
                  onClick={() => void run(() => records(page.next))}
                >
                  Run記録の次のページ
                </button>
              )}
            </>
          )}
        </>
      )}
      {native && canEdit && (
        <ExecutionSettings
          key={`execution/${workspace}`}
          workspace={workspace}
        />
      )}
      {native && canEdit && (
        <RunnerSettings key={workspace} workspace={workspace} user={user} />
      )}
      {canEdit && (
        <details>
          <summary>新しいRunを作成</summary>
          <fieldset disabled={busy}>
            <label>
              名前（任意）
              <input value={name} onChange={(e) => setName(e.target.value)} />
            </label>
            <label>
              接続の種類
              <input
                value={commandKind}
                onChange={(e) => setCommandKind(e.target.value)}
              />
            </label>
            <label>
              command・接続先
              <input
                value={target}
                onChange={(e) => setTarget(e.target.value)}
              />
            </label>
            <label>
              commandの引数・要求（JSON）
              <textarea
                value={parameters}
                onChange={(e) => setParameters(e.target.value)}
                rows={3}
              />
            </label>
            <label>
              prompt（任意）
              <textarea
                value={prompt}
                onChange={(e) => setPrompt(e.target.value)}
                rows={3}
              />
            </label>
            {prompt && (
              <label>
                promptの受け渡し方法
                <input
                  value={promptBinding}
                  onChange={(e) => setPromptBinding(e.target.value)}
                />
              </label>
            )}
            <label>
              確定済みManifest ID（任意）
              <input
                value={manifestID}
                onChange={(e) => setManifestID(e.target.value)}
              />
            </label>
            {manifestID && (
              <>
                <label>
                  Manifestの固定版
                  <input
                    value={manifestVersion}
                    onChange={(e) => setManifestVersion(e.target.value)}
                  />
                </label>
                <label>
                  Contextの受け渡し方法
                  <input
                    value={contextBinding}
                    onChange={(e) => setContextBinding(e.target.value)}
                  />
                </label>
              </>
            )}
            {previous && (
              <p>
                関連元: {previous.id} · {previous.version}
                <button onClick={() => setPrevious(null)}>関連元を外す</button>
              </p>
            )}
            <p>
              元Runの会話・履歴は自動で取り込みません。Context・promptを省略し、commandだけでも作成できます。
            </p>
            <button
              disabled={!target.trim() || !commandKind.trim()}
              onClick={() => void run(create)}
            >
              Runを作成（起動しない）
            </button>
          </fieldset>
        </details>
      )}
      {notice && <p role="status">{notice}</p>}
    </details>
  );
}
