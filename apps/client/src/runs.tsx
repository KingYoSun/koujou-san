import { Field, jsonError } from "./field";
import { NavigationLink, useNavigation } from "./navigation";
import type { ReactNode } from "react";
import { useEffect, useRef, useState } from "react";
import { invoke } from "@tauri-apps/api/core";
import { syncRequest, workspaceAccessLost } from "./auth";
import { SyncHTTPError } from "./sync-error";
import { allowAnswerScope, answerScopeEpoch, syncStorage } from "./sync-store";
import { PauseResponse, QuestionView } from "./run-pause";
import { RunOutputRecord, handoffOutput } from "./run-output-record";
import type { OutputContextSource } from "./context-generation";
import type { Pause } from "./run-pause";

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
  handoff?: {
    source: Ref;
    decision?: { source: Ref; judgment: string; by: string; at: string };
  };
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
const stateNames: Record<string, string> = {
  queued: "未実行",
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
  api,
  workspace,
  user,
  role,
  canExecute,
  selected,
  onSelect,
  onPrepare,
  children,
  view,
  onGenerate,
}: {
  native: boolean;
  api: string;
  workspace: string;
  user: string;
  role: string;
  canExecute: boolean;
  selected?: Omit<RecordVersion, "run_id">;
  onSelect: (ref: Ref) => Promise<void>;
  onPrepare: (id: string) => void;
  view?: string;
  children: (parts: {
    overview: ReactNode;
    input: ReactNode;
    records: ReactNode;
    prepare: ReactNode;
    preparation: Ref;
  }) => ReactNode;
  onGenerate?: (source: OutputContextSource) => void;
}) {
  const [deviceID, setDeviceID] = useState(""),
    [repoID, setRepoID] = useState("");
  const [commit, setCommit] = useState(""),
    [required, setRequired] = useState("");
  const base = `/runs/workspaces/${encodeURIComponent(workspace)}`;
  const [busy, setBusy] = useState(false),
    [notice, setNotice] = useState("");
  const current = selected;
  const navigation = useNavigation();
  const [page, setPage] = useState<Page | null>(null);
  const [kind, setKind] = useState("pause"),
    [input, setInput] = useState<InputSpec | null>(null);
  const [contextText, setContextText] = useState<string | undefined>();
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
  useEffect(() => {
    const route = navigation?.route;
    if (
      route?.screen === "work" &&
      route.view === "prepare" &&
      route.origin?.kind === "run-preparation" &&
      route.origin.id === newID &&
      route.input?.kind === "manifest"
    ) {
      setManifestID(route.input.id);
      setManifestVersion(route.input.version);
    }
  }, [navigation?.route.input?.id, navigation?.route.input?.version, newID]);
  const [handoff, setHandoff] = useState(false);
  const [outputs, setOutputs] = useState<RecordVersion[]>([]);
  const [handoffSource, setHandoffSource] = useState<RecordVersion>();
  const [allowDuplicate, setAllowDuplicate] = useState(false);
  const pending = useRef<{ key: string; id: string } | null>(null);
  const recordEpoch = useRef(0);
  const runValue = current?.value as Run | undefined;
  const canEdit = role !== "viewer";
  const namespace = { api, user, workspace };
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
      if (
        (error instanceof SyncHTTPError &&
          (error.status === 401 || error.status === 403)) ||
        /forbidden|authentication_required/.test(String(error))
      ) {
        setPage(null);

        setInput(null);
        setContextText(undefined);
        // A denied execution action alone does not revoke Workspace reading.
        if (await workspaceAccessLost(native, workspace, error))
          await syncStorage(native).remove(namespace);
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
  const load = (runID: string) => onSelect({ id: runID, version: "head" });
  const records = async (after = "", recordKind = kind) => {
    if (!current) return;
    const request = ++recordEpoch.current;
    const epoch = answerScopeEpoch(namespace);
    const fetched = await query({
      run_id: current.id,
      version: current.version,
      kind: recordKind,
      after,
      limit: 50,
      snapshot: after ? page?.snapshot : undefined,
    });
    if (request === recordEpoch.current && allowAnswerScope(namespace, epoch))
      setPage(fetched);
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
    let fixedBase: { manifest: Ref; input_utf8: string } | undefined;
    if (manifestID || manifestVersion) {
      if (!manifestID || !manifestVersion)
        throw Error("fixed Manifest required");
      const resolved = (await syncRequest(
        `/contexts/workspaces/${encodeURIComponent(workspace)}/manifest/${encodeURIComponent(manifestID)}?revision=${encodeURIComponent(manifestVersion)}`,
        native,
        { method: "GET" },
      )) as { data: { value: { run_id: string; input_utf8?: string } } };
      if (handoff) {
        if (resolved.data.value.input_utf8 !== undefined)
          fixedBase = {
            manifest: {
              id: `manifest:${manifestID}`,
              version: manifestVersion,
            },
            input_utf8: resolved.data.value.input_utf8,
          };
      } else {
        runID = resolved.data.value.run_id;
        manifest = {
          kind: "manifest",
          id: manifestID,
          revision: manifestVersion,
        };
      }
    }
    const context =
      handoff && (fixedBase || outputs.length)
        ? {
            ...(fixedBase ? { base: fixedBase } : {}),
            outputs: outputs.map((r) => ({
              ref: { id: r.id, version: r.version },
              output: r.value,
            })),
          }
        : undefined;
    const binding: Record<string, string> = {};
    if (manifest || context) binding.context = contextBinding;
    if (prompt) binding.prompt = promptBinding;
    const result = await mutate(runID, "create", "0", {
      ...(handoff ? { handoff: true } : {}),
      spec: {
        name,
        input: {
          ...(context ? { context } : {}),
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
        ...(handoff
          ? {
              sources: [
                ...(manifestID
                  ? [{ id: `manifest:${manifestID}`, version: manifestVersion }]
                  : []),
                ...outputs.map((r) => ({ id: r.id, version: r.version })),
              ],
              artifacts: outputs.flatMap(
                (r) => (r.value as { artifacts?: unknown[] }).artifacts ?? [],
              ),
            }
          : {}),
      },
    });
    await load(result.data.id);
    setNewID(crypto.randomUUID());
    setHandoff(false);
    setOutputs([]);
    setPrevious(null);
    setManifestID("");
    setManifestVersion("");
    setNotice("Runを作成しました。開始は依頼していません。");
  };
  useEffect(() => {
    let active = true;
    setHandoffSource(undefined);
    setAllowDuplicate(false);
    setPage(null);
    setInput(null);
    setContextText(undefined);
    const source = runValue?.handoff?.source;
    if (source)
      void run(async () => {
        const record = await read({ id: source.id, version: "head" });
        if (active) setHandoffSource(record);
      });
    return () => {
      active = false;
    };
  }, [current?.id, current?.version]);
  useEffect(() => {
    recordEpoch.current++;
    setPage(null);
    if (view === "evidence" && current) {
      setKind("output");
      void run(() => records("", "output"));
    }
    return () => {
      recordEpoch.current++;
    };
  }, [view, current?.id, current?.version]);
  const overview = (
    <>
      <NavigationLink to={{ workspace, screen: "execution" }} related>
        実行先・Runnerを設定
      </NavigationLink>
      {!canExecute && <p>開始・割当・取消には実行依頼の許可が必要です。</p>}
      {current && runValue && (
        <>
          <h4>実行の確認・割当</h4>
          <p>
            {current.id} · {current.version}
          </p>
          <RunStatus value={runValue} />
          {runValue.handoff?.decision && (
            <p>
              引継ぎ判断:{" "}
              {runValue.handoff.decision.judgment === "duplicate_possible"
                ? "元実行との重複可能性を了承"
                : "元実行の終了を確認"}{" "}
              / {runValue.handoff.decision.by} / {runValue.handoff.decision.at}{" "}
              / 元Run版: {runValue.handoff.decision.source.version}
            </p>
          )}
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
                      const result = await invoke<{
                        observation: { direct_process: string };
                        queued_reconciliation: boolean;
                      }>("reconcile_run", { workspace, id: current.id });
                      await load(current.id);
                      setNotice(
                        `この端末の記録を照合しました。プロセス: ${result.observation.direct_process === "running" ? "同じ起動を観測" : "継続を確認できません"}。${result.queued_reconciliation ? "Runnerの報告照合を待ちます。" : ""}セッションの再接続は未確認です。新しいCLIは起動していません。`,
                      );
                    })
                  }
                >
                  この端末の実行記録を照合
                </button>
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
                <Field
                  label="担当端末ID"
                  value={deviceID}
                  onChange={setDeviceID}
                />
                <Field
                  label="repositoryの論理ID（任意）"
                  value={repoID}
                  onChange={setRepoID}
                />
                {repoID && (
                  <Field
                    label="作成元の固定commit"
                    value={commit}
                    onChange={setCommit}
                  />
                )}
                <Field
                  label="必要な接続能力（空白区切り、任意）"
                  value={required}
                  onChange={setRequired}
                />
                <p>
                  端末の現在の許可と確認済み能力を照合します。repositoryを使うRunには別worktree・branchを自動作成します。割当だけではCLIは起動しません。
                </p>
                {runValue.handoff && handoffSource && (
                  <>
                    <p>
                      引継ぎ元の現在の状態:{" "}
                      {stateNames[(handoffSource.value as Run).state]} /{" "}
                      {(handoffSource.value as Run).observation === "unknown"
                        ? "不明"
                        : "確認済み"}{" "}
                      · {handoffSource.version}
                    </p>
                    {(handoffSource.value as Run).observation === "unknown" && (
                      <label>
                        <input
                          type="checkbox"
                          checked={allowDuplicate}
                          onChange={(e) => setAllowDuplicate(e.target.checked)}
                        />
                        元の実行が継続し、重複する可能性を認識して新Runの割当を依頼する
                      </label>
                    )}
                  </>
                )}
                <button
                  disabled={busy || !deviceID || (!!repoID && !commit)}
                  onClick={() =>
                    void run(async () => {
                      await mutate(current.id, "assign", current.version, {
                        ...(runValue.handoff && handoffSource
                          ? {
                              handoff_source: {
                                id: handoffSource.id,
                                version: handoffSource.version,
                              },
                              allow_duplicate: allowDuplicate,
                            }
                          : {}),
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
            {canEdit && (
              <button
                disabled={busy}
                onClick={() => {
                  setPrevious({ id: current.id, version: current.version });
                  setHandoff(false);
                  setOutputs([]);
                  onPrepare(newID);
                }}
              >
                現在のRunを新しいRunの関連元にする
              </button>
            )}
            {canEdit && (
              <button
                disabled={busy}
                onClick={() => {
                  setPrevious({ id: current.id, version: current.version });
                  setHandoff(true);
                  setOutputs([]);
                  onPrepare(newID);
                  setNotice(
                    "別Runへの引継ぎを準備します。必要な固定出力を選び、新しい入力を指定してください。開始は依頼していません。",
                  );
                }}
              >
                別Runへの引継ぎを準備
              </button>
            )}
            {canExecute &&
              !runValue.handoff &&
              !runValue.execution_requested_at && (
                <button
                  disabled={busy}
                  onClick={() =>
                    void run(async () => {
                      await mutate(
                        current.id,
                        "request-start",
                        current.version,
                      );
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
        </>
      )}
    </>
  );
  const inputPanel = (
    <>
      {current && runValue && (
        <button
          disabled={busy}
          onClick={() => void run(() => loadInput(runValue.input))}
        >
          固定したinput・派生契約を取得
        </button>
      )}
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
    </>
  );
  const recordPanel = current && runValue && (
    <>
      {view === "evidence" && <h4>原出力・固定参照</h4>}
      <div className="team-controls">
        {view !== "evidence" && (
          <label>
            Runの記録
            <select
              value={kind}
              onChange={(e) => {
                setKind(e.target.value);
                recordEpoch.current++;
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
        )}
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
                      <QuestionView value={pause.request} />
                      {pause.response !== undefined && (
                        <>
                          {pause.response_origin?.kind === "event" && (
                            <p>
                              観測元: {pause.response_origin.source} /{" "}
                              {pause.response_origin.id}
                            </p>
                          )}
                          <p>
                            回答者: {pause.answered_by} / {pause.answered_at}
                          </p>
                          <PauseResponse
                            value={pause.response}
                            request={pause.request}
                          />
                          <p>
                            配送確認: {pause.delivered_at ?? "未確認"} /
                            再開確認: {pause.resumed_at ?? "未確認"}
                          </p>
                        </>
                      )}
                    </>
                  ) : (
                    <RunOutputRecord
                      record={record}
                      current={current}
                      workspace={workspace}
                      editable={canEdit}
                      busy={busy}
                      onGenerate={onGenerate}
                      onHandoff={() => {
                        const next = handoffOutput(
                          current,
                          record,
                          previous,
                          handoff,
                          outputs,
                        );
                        setPrevious(next.previous);
                        setHandoff(true);
                        setOutputs(next.outputs);
                        setNotice(
                          "この固定出力を引継ぎ入力へ追加しました。新Runの入力欄で確認してください。",
                        );
                      }}
                    />
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
  );
  const preparePanel = (
    <>
      <NavigationLink to={{ workspace, screen: "execution" }} related>
        実行先・Runnerを設定
      </NavigationLink>
      <NavigationLink
        to={{
          workspace,
          screen: "context",
          view: "input",
          origin: { kind: "run-preparation", id: newID, version: "0" },
        }}
        related
      >
        Contextを選択・入力をプレビュー
      </NavigationLink>
      <NavigationLink
        to={{ workspace, screen: "workflows", view: "run-definitions" }}
        related
      >
        採用済みのRun定義から準備
      </NavigationLink>
      {canEdit ? (
        <section aria-label="新しいRunを作成">
          <h3>新しいRunを作成</h3>
          <fieldset disabled={busy}>
            <Field label="名前（任意）" value={name} onChange={setName} />
            <h4>実行するcommand</h4>
            <Field
              label="接続の種類"
              value={commandKind}
              onChange={setCommandKind}
              required
            />
            <Field
              label="command・接続先"
              value={target}
              onChange={setTarget}
              required
              hint="端末の接続設定にある論理名を指定します。実パスは端末で解決します。"
            />
            <Field
              label="commandの引数・要求（JSON）"
              value={parameters}
              onChange={setParameters}
              rows={3}
              error={jsonError(parameters)}
            />
            <h4>入力（任意）</h4>
            <Field
              label="prompt（任意）"
              value={prompt}
              onChange={setPrompt}
              rows={3}
            />
            {prompt && (
              <Field
                label="promptの受け渡し方法"
                value={promptBinding}
                onChange={setPromptBinding}
              />
            )}
            <Field
              label="確定済みManifest ID（任意）"
              value={manifestID}
              onChange={setManifestID}
            />
            {manifestID && (
              <>
                <Field
                  label="Manifestの固定版"
                  value={manifestVersion}
                  onChange={setManifestVersion}
                />
                <Field
                  label="Contextの受け渡し方法"
                  value={contextBinding}
                  onChange={setContextBinding}
                />
              </>
            )}
            {previous && (
              <p>
                {handoff ? "引継ぎ元" : "関連元"}: {previous.id} ·{" "}
                {previous.version}
                <button
                  onClick={() => {
                    setPrevious(null);
                    setHandoff(false);
                    setOutputs([]);
                  }}
                >
                  関連元を外す
                </button>
              </p>
            )}
            {handoff && (
              <>
                <p>
                  選択した固定出力: {outputs.length}
                  件。元Runの全履歴や作業領域は取り込みません。開始時に元実行の終了・重複可能性を確認します。
                </p>
                <ul>
                  {outputs.map((r) => (
                    <li key={`${r.id}:${r.version}`}>
                      {r.id} · {r.version}
                      <button
                        onClick={() =>
                          setOutputs((old) => old.filter((v) => v !== r))
                        }
                      >
                        引継ぎから外す
                      </button>
                    </li>
                  ))}
                </ul>
                {!!outputs.length && !manifestID && (
                  <Field
                    label="Contextの受け渡し方法"
                    value={contextBinding}
                    onChange={setContextBinding}
                  />
                )}
              </>
            )}
            <p>
              元Runの会話・履歴は自動で取り込みません。Context・promptを省略し、commandだけでも作成できます。
            </p>
            <button
              disabled={
                !target.trim() || !commandKind.trim() || !!jsonError(parameters)
              }
              onClick={() => void run(create)}
            >
              Runを作成（起動しない）
            </button>
          </fieldset>
        </section>
      ) : (
        <p>Runの保存には編集権限が必要です。</p>
      )}
    </>
  );
  return (
    <>
      {notice && <p role="status">{notice}</p>}
      {children({
        overview,
        input: inputPanel,
        records: recordPanel,
        prepare: preparePanel,
        preparation: { id: newID, version: "0" },
      })}
    </>
  );
}
