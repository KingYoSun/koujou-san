import { ScreenContent, NavigationLink } from "./navigation";
import {
  sourceKey,
  sourceRoute,
  type GenerationSource,
} from "./context-generation-source";
import { useEffect, useRef, useState } from "react";
import { SyncHTTPError } from "./sync-error";
import { useStore } from "zustand";
import { invoke } from "@tauri-apps/api/core";
import { syncRequest } from "./auth";
import { refKey, refLabel, type UI, type Ref } from "./context-ui";
import { ModelConnectionSettings } from "./model-connection-settings";
import {
  DefinitionProposal,
  DefinitionFixedFields,
  definitionFields,
} from "./definition-proposal";
import {
  operationClientID as clientID,
  operationPort,
} from "./client-operation-port";
import {
  ContextGenerationProposal,
  type ContextProposalDraft,
  type ContextChanges,
  emptyContextChanges,
} from "./context-generation-proposal";
import { fieldSources } from "./workflow-json";
import {
  modelReady,
  modelSnapshot,
  type ModelProfile,
  type VersionRef,
} from "./model-connections";
import {
  runSelection,
  selectionInput,
  type ClientOperation,
  type Saved,
  type SelectionInput,
  type SelectionPolicy,
  type GenerationPolicy,
} from "./client-selection-host";
import { Disclosure } from "./disclosure";
import { Field } from "./field";
import { StatusBadge } from "./status-badge";
const pin = (v: VersionRef): VersionRef => ({ id: v.id, version: v.version });
type Proposal = {
  changes_utf8?: string;
  manual?: boolean;
  definition_utf8?: string;
  reason?: string;
  items: { ref: Ref; reason: string }[];
  questions: string[];
  unknown: string[];
};
type Selection = {
  name: string;
  input: SelectionInput;
  manifest: Ref;
  items: Proposal["items"];
};
export const operationStateLabels: Record<string, string> = {
  waiting: "処理クライアントを待機",
  ready: "選択の準備・継続待ち",
  sending: "モデルへ要求済み",
  question: "依頼者への質問",
  proposal: "確認する案があります",
  applied: "選択を保存・反映済み",
  completed: "処理を完了",
  incomplete: "未確認事項・上限・接続を確認",
  failed: "接続・応答の確認が必要",
  cancelled: "操作を終了",
};
export type OperationUI = Pick<
  UI,
  "native" | "api" | "workspace" | "user" | "store" | "canCreate"
>;
export type DefinitionTarget = {
  manualPaths?: string[];
  request?: string;
  target: VersionRef;
  text: string;
  sources: VersionRef[];
  onApplied: (
    ref: VersionRef,
    fixedPaths: string[],
    isCurrent: () => boolean,
  ) => Promise<boolean>;
  onDraft: (text: string) => void;
};
export type GenerationTarget = {
  target: VersionRef;
  policy: GenerationPolicy;
  initial?: VersionRef;
  onApplied: (ref: VersionRef) => Promise<void>;
  onProfile?: (profile: ModelProfile | undefined) => void;
};
export function ClientSelection({
  ui,
  definition,
  generation,
}: {
  ui: OperationUI;
  definition?: DefinitionTarget;
  generation?: GenerationTarget;
}) {
  const generationCurrent = useRef(generation);
  generationCurrent.current = generation;
  const [generationDraft, setGenerationDraft] = useState<ContextProposalDraft>({
    text: emptyContextChanges,
    chosen: [],
  });
  const definitionCurrent = useRef(definition);
  definitionCurrent.current = definition;
  const [definitionLocks, setDefinitionLocks] = useState<string[]>([]);
  const manualSignature = JSON.stringify(definition?.manualPaths ?? []);
  useEffect(() => {
    const paths = JSON.parse(manualSignature) as string[];
    setDefinitionLocks((current) =>
      Array.from(new Set([...current, ...paths])),
    );
  }, [manualSignature, definition?.text]);
  const [definitionPaths, setDefinitionPaths] = useState<string[]>(
    definitionFields.map(([path]) => path),
  );
  const state = useStore(ui.store),
    actor = ui.api + "\0" + ui.user,
    base = `/client-operations/workspaces/${encodeURIComponent(ui.workspace)}`;
  const [profile, setProfile] = useState<ModelProfile>(),
    [goal, setGoal] = useState(""),
    [mode, setMode] = useState<SelectionPolicy["mode"]>("auto"),
    [purpose, setPurpose] = useState("selection"),
    [scopes, setScopes] = useState("");
  const [models, setModels] = useState(32),
    [mcp, setMcp] = useState(256),
    [seconds, setSeconds] = useState(600),
    [fixed, setFixed] = useState<Ref[]>(state.include),
    [operation, setOperation] = useState<Saved<ClientOperation>>(),
    [proposal, setProposal] = useState<Saved<Proposal>>(),
    [selection, setSelection] = useState<VersionRef>();
  const [operationSource, setOperationSource] = useState<GenerationSource>();
  useEffect(() => {
    if (!generation) return;
    const text = proposal?.value.changes_utf8 ?? emptyContextChanges;
    const value = JSON.parse(text) as ContextChanges;
    setGenerationDraft({
      text,
      chosen: [...value.changes, ...value.grouping].map((v) => v.key),
    });
  }, [operation?.id, proposal?.id, proposal?.version]);
  const generationMatches =
    !generation ||
    sourceKey(operationSource) === sourceKey(generation.policy.source);
  const ownsOperation =
    operation?.value.user === ui.user && operation.value.client === clientID;
  const generationDirty =
    !!generation &&
    generationDraft.text !==
      (proposal?.value.changes_utf8 ?? emptyContextChanges);
  const [records, setRecords] = useState<Saved[]>(),
    [kind, setKind] = useState("operation"),
    [after, setAfter] = useState<{ after: number; after_id: string }>({
      after: 0,
      after_id: "",
    }),
    [history, setHistory] = useState<Saved[]>(),
    [historyItem, setHistoryItem] = useState<Saved>(),
    [historyCursor, setHistoryCursor] = useState({ after: 0, after_id: "" }),
    [fixedPage, setFixedPage] = useState(0),
    [proposalPage, setProposalPage] = useState(0),
    [answer, setAnswer] = useState(""),
    [notice, setNotice] = useState(""),
    [busy, setBusy] = useState(false);
  const epoch = useRef(0),
    previous = useRef(state.include),
    automatic = useRef(""),
    pending = useRef<{ key: string; id: string } | undefined>(undefined);
  useEffect(() => {
    const now = state.include;
    const signature = JSON.stringify([now, state.exclude, state.mode]);
    if (signature !== automatic.current) {
      const old = new Set(previous.current.map(refKey)),
        present = new Set(now.map(refKey));
      setFixed((current) => [
        ...current.filter((r) => present.has(refKey(r))),
        ...now.filter((r) => !old.has(refKey(r))),
      ]);
    }
    previous.current = now;
  }, [state.include, state.exclude, state.mode]);
  useEffect(
    () => () => {
      epoch.current++;
    },
    [ui.api, ui.user, ui.workspace],
  );
  useEffect(() => {
    epoch.current++;
    setGoal("");
    setOperation(undefined);
    setOperationSource(undefined);
    setProposal(undefined);
    setRecords(undefined);
    setHistory(undefined);
    setHistoryItem(undefined);
    setSelection(undefined);
    setFixed([]);
  }, [state.permissionEpoch]);
  const call = async <T,>(path: string, body?: unknown) =>
    syncRequest(base + path, ui.native, {
      method: body === undefined ? "GET" : "POST",
      body,
    }) as Promise<T>;
  const read = async <T,>(ref: VersionRef) =>
    call<Saved<T>>(
      `/records/${encodeURIComponent(ref.id)}?version=${encodeURIComponent(ref.version)}`,
    );
  const inputSignature = () => {
    if (generationCurrent.current)
      return JSON.stringify(generationCurrent.current.policy);
    if (definitionCurrent.current)
      return JSON.stringify({
        text: definitionCurrent.current.text,
        sources: definitionCurrent.current.sources,
        request: definitionCurrent.current.request,
      });
    const { include, exclude, mode } = ui.store.getState();
    return JSON.stringify([include, exclude, mode]);
  };
  const mutate = async (
    path: string,
    version: string,
    value: Record<string, unknown>,
    id?: string,
  ) => {
    const key = JSON.stringify({ path, version, value });
    if (pending.current?.key !== key)
      pending.current = { key, id: id ?? crypto.randomUUID() };
    const result = await call<{ data?: VersionRef; error?: unknown }>(path, {
      operation_id: id ?? pending.current.id,
      expected_version: version,
      ...value,
    });
    if (!result.data || result.error) throw new Error("not_accepted");
    pending.current = undefined;
    return pin(result.data);
  };
  async function apply(ref: VersionRef, expected?: string) {
    if (generationCurrent.current) {
      if (expected !== undefined && expected !== inputSignature()) {
        setNotice(
          "Contextの変更を保存しました。表示中の出力・設定は変わっています。",
        );
        return;
      }
      await generationCurrent.current.onApplied(ref);
      setNotice("Contextの版と所属を保存しました。");
      return;
    }
    if (definitionCurrent.current) {
      const stale = "定義を保存しました。編集中の下書きは変わっています。";
      if (expected !== undefined && expected !== inputSignature()) {
        setNotice(stale);
        return;
      }
      const applied = await definitionCurrent.current.onApplied(
        ref,
        definitionLocks,
        () => expected === undefined || expected === inputSignature(),
      );
      if (!applied) {
        setNotice(stale);
        return;
      }
      setNotice("定義版を保存・採用しました。");
      return;
    }
    const saved = await read<Selection>(ref);
    setSelection(pin(saved));
    if (expected !== undefined && expected !== inputSignature()) {
      setNotice("選択を保存しました。画面の選択は変わっています。");
      return;
    }
    automatic.current = JSON.stringify([
      saved.value.input.include ?? [],
      saved.value.input.exclude ?? [],
      saved.value.input.mode,
    ]);
    ui.store.setState({
      mode: saved.value.input.mode,
      include: saved.value.input.include ?? [],
      exclude: saved.value.input.exclude ?? [],
    });
    setSelection(pin(saved));
    setNotice("選択を保存し、入力プレビューへ反映しました。");
  }
  async function refresh(ref?: VersionRef) {
    const selected = ref ?? operation;
    if (!selected) return;
    const current = await read<ClientOperation>({
      id: selected.id,
      version: "head",
    });
    if (
      (current.value.purpose === "definition") !== !!definition ||
      (current.value.purpose === "context-generation") !== !!generation
    )
      throw new Error("different_operation_kind");
    if (generation) {
      const policy = await read<SelectionPolicy>(current.value.policy);
      setOperationSource(policy.value.generation?.source);
    }
    setOperation(current);
    setSelection(pin(current.value.applied ?? current.value.target));
    if (current.value.proposal)
      setProposal(await read<Proposal>(current.value.proposal));
    return current;
  }
  async function work(action: () => Promise<void>) {
    setBusy(true);
    setNotice("");
    try {
      await action();
    } catch (error) {
      setNotice(
        error instanceof SyncHTTPError && error.status === 409
          ? "操作を更新できませんでした。操作の版と担当クライアントを確認してください。"
          : error instanceof SyncHTTPError && error.status === 400
            ? "入力を受け付けられませんでした。固定参照のIDと版・変更案JSONを確認してください。"
            : "要求を実行できませんでした。入力・現在版・権限・接続を確認してください。",
      );
    } finally {
      setBusy(false);
    }
  }
  async function list(reset = true) {
    const result = await call<{
      items: Saved[];
      after: number;
      after_id: string;
    }>("/query", {
      kind,
      metadata: true,
      limit: 50,
      ...(reset ? { after: 0, after_id: "" } : after),
    });
    setRecords(result.items);
    setAfter({ after: result.after, after_id: result.after_id });
  }
  async function run(
    current: Saved<ClientOperation>,
    policy: SelectionPolicy,
    signature = inputSignature(),
  ) {
    if (!profile || !modelReady(ui.native, actor, profile))
      throw new Error("model_key_required");
    const mine = ++epoch.current;
    const port = operationPort(
      ui,
      profile,
      current,
      () => epoch.current === mine,
    );
    const result = await runSelection(
      current,
      policy,
      profile,
      clientID,
      port,
      (v) => setOperation(v),
    );
    if (epoch.current === mine) {
      await refresh(result);
      if (result.value.applied) await apply(result.value.applied, signature);
    }
  }
  async function loadHistory(reset = true) {
    if (!operation) return;
    const page = await call<{
      items: Saved[];
      after: number;
      after_id: string;
    }>("/query", {
      root: operation.id,
      history: true,
      metadata: true,
      limit: 50,
      ...(reset ? { after: 0, after_id: "" } : historyCursor),
    });
    setHistory(page.items);
    setHistoryItem(undefined);
    setHistoryCursor({ after: page.after, after_id: page.after_id });
  }
  async function start(manual = false) {
    const signature = inputSignature();
    if (!manual && !profile) throw new Error("model_required");
    const values = definition
      ? fieldSources(
          definition.text,
          definitionLocks.map((path) => path.slice(1).split("/")),
        )
      : [];
    const policy: SelectionPolicy = {
      mode: manual ? "manual" : mode,
      ...(generation ? { generation: generation.policy } : {}),
      fixed: selectionInput(state.mode, fixed, state.exclude),
      scopes: scopes.split(/[,\s]+/).filter(Boolean),
      limits: { model_requests: models, mcp_requests: mcp, seconds },
      ...(definition
        ? {
            definition: {
              fixed_paths: definitionLocks,
              fixed_values: Object.fromEntries(
                definitionLocks.map((path, index) => [path, values[index]]),
              ),
              sources: definition.sources,
            },
          }
        : {}),
    };
    const policyRef = await mutate(
      `/policies/${generation ? "context" : definition ? "definition" : "selection"}-policy:${crypto.randomUUID()}`,
      "0",
      { value: policy },
    );
    const ref = await mutate(
      `/operations/client-operation:${crypto.randomUUID()}`,
      "0",
      {
        policy: policyRef,
        target: generation?.target ??
          definition?.target ??
          selection ?? {
            id: `client-selection:${crypto.randomUUID()}`,
            version: "0",
          },
        goal: definition?.request ? goal + "\n" + definition.request : goal,
        purpose: generation
          ? "context-generation"
          : definition
            ? "definition"
            : purpose,
        model: manual ? {} : modelSnapshot(profile!, ui.workspace),
        client: clientID,
      },
    );
    const current = await read<ClientOperation>(ref);
    setOperationSource(policy.generation?.source);
    setOperation(current);
    setProposal(undefined);
    if (!manual) await run(current, policy, signature);
  }
  async function act(action: string) {
    if (!operation) return;
    if (action === "adopt" && generationDirty) {
      setNotice("手動修正したContext案を保存してから採用してください。");
      return;
    }
    if (["adopt", "answer", "resume"].includes(action) && !ownsOperation) {
      setNotice("このクライアントへ引き継いでから操作してください。");
      return;
    }
    if (!generationMatches && action !== "cancel" && action !== "takeover") {
      setNotice(
        "この操作は別の固定出力に対するものです。元の出力を開いてから操作してください。",
      );
      return;
    }
    const signature = inputSignature();
    const value: Record<string, unknown> = { kind: action, client: clientID };
    if (action === "answer") {
      value.answer = definition?.request
        ? answer + "\n" + definition.request
        : answer;
      if (definition) value.definition_sources = definition.sources;
    }
    if (action === "adopt" && generation) {
      if (!generationDraft.chosen.length) throw Error("choose_changes");
      value.paths = generationDraft.chosen;
    }
    if (action === "adopt" && definition) {
      if (!definitionPaths.length) throw new Error("choose_fields");
      value.paths = definitionPaths;
    }
    if (action === "takeover" && profile)
      value.model = modelSnapshot(profile, ui.workspace);
    const ref = await mutate(
      `/operations/${encodeURIComponent(operation.id)}/actions`,
      operation.version,
      value,
    );
    const current = await refresh(ref);
    if (action === "adopt" && current?.value.applied)
      await apply(current.value.applied, signature);
    if ((action === "resume" || action === "answer") && current) {
      const policy = await read<SelectionPolicy>(current.value.policy);
      await run(current, policy.value, signature);
    }
  }
  useEffect(() => {
    if (generation?.initial)
      void work(async () => {
        const current = await refresh(generation.initial);
        if (current?.value.applied) await apply(current.value.applied);
      });
  }, [generation?.initial?.id, generation?.initial?.version]);
  async function manual() {
    const ref = await mutate(
      `/selections/${encodeURIComponent(selection?.id ?? `client-selection:${crypto.randomUUID()}`)}`,
      selection?.version ?? "0",
      {
        name: goal || "手動のContext選択",
        input: selectionInput(state.mode, state.include, state.exclude),
        items: [],
        ...(operation ? { operation: pin(operation) } : {}),
        ...(proposal ? { proposal: pin(proposal) } : {}),
      },
    );
    await apply(ref);
  }
  async function choose(record: Saved) {
    if (record.kind === "selection") {
      await apply(record);
      return;
    }
    const current = await refresh(record);
    if (current) {
      const policy = await read<SelectionPolicy>(current.value.policy);
      setFixed(policy.value.fixed.include ?? []);
      setMode(policy.value.mode);
      setGoal(current.value.goal);
    }
  }
  return (
    <ScreenContent>
      {
        <section
          className="context-preview"
          aria-label={
            generation
              ? "出力からContextを生成"
              : definition
                ? "自由入力からRun定義を生成"
                : "Contextの自動選択と保存"
          }
        >
          <h3>
            {generation
              ? "出力からContextを生成"
              : definition
                ? "自由入力からRun定義を作る"
                : "Contextの選択と保存"}
          </h3>
          <ModelConnectionSettings
            selectionOnly
            native={ui.native}
            actor={actor}
            workspace={ui.workspace}
            selected={profile}
            onSelected={(p) => {
              setProfile(p);
              generation?.onProfile?.(p);
            }}
          />
          <fieldset disabled={!ui.canCreate || busy}>
            <legend>
              {definition || generation
                ? "生成する目的と方針"
                : "選択する目的と方針"}
            </legend>
            <Field
              label={
                generation
                  ? "Contextを作る依頼"
                  : definition
                    ? "Run定義を作る依頼"
                    : "Contextを選ぶ目的"
              }
              value={goal}
              onChange={setGoal}
              rows={3}
            />
            <label>
              反映方式
              <select
                value={mode}
                onChange={(e) =>
                  setMode(e.target.value as SelectionPolicy["mode"])
                }
              >
                <option value="auto">
                  {generation
                    ? "検証済みのContext変更を自動反映"
                    : definition
                      ? "検証済みの定義を自動反映"
                      : "検証済みの選択を自動反映"}
                </option>
                <option value="proposal">案を確認して採用</option>
                <option value="manual">
                  {generation
                    ? "手動でContextを編集"
                    : definition
                      ? "手動で定義を編集"
                      : "手動で選択"}
                </option>
              </select>
            </label>
            {!definition && !generation && (
              <label>
                用途
                <select
                  value={purpose}
                  onChange={(e) => setPurpose(e.target.value)}
                >
                  <option value="selection">作業用の選択</option>
                  <option value="inspection">検品用の選択</option>
                </select>
              </label>
            )}
            {(definition || generation) && (
              <>
                <label>
                  Contextの取得
                  <select
                    value={state.mode}
                    onChange={(e) =>
                      ui.store.setState({
                        mode: e.target.value as "none" | "select",
                      })
                    }
                  >
                    <option value="select">許可範囲を検索して選ぶ</option>
                    <option value="none">
                      {generation
                        ? "既存Contextを参照しない"
                        : "Contextを参照・追加しない"}
                    </option>
                  </select>
                </label>
                {definition && (
                  <DefinitionFixedFields
                    paths={definitionLocks}
                    onPaths={setDefinitionLocks}
                  />
                )}
              </>
            )}
            <Disclosure title="対象Scope・上限・必須項目">
              {() => (
                <>
                  <Field
                    label="絞り込むScope ID"
                    value={scopes}
                    onChange={setScopes}
                  />
                  <label>
                    モデル要求の上限
                    <input
                      type="number"
                      min={1}
                      value={models}
                      onChange={(e) => setModels(Number(e.target.value))}
                    />
                  </label>
                  <label>
                    MCP要求の上限
                    <input
                      type="number"
                      min={1}
                      value={mcp}
                      onChange={(e) => setMcp(Number(e.target.value))}
                    />
                  </label>
                  <label>
                    時間上限（秒）
                    <input
                      type="number"
                      min={1}
                      value={seconds}
                      onChange={(e) => setSeconds(Number(e.target.value))}
                    />
                  </label>
                  {state.include
                    .slice(fixedPage * 50, fixedPage * 50 + 50)
                    .map((r) => (
                      <label key={refKey(r)}>
                        <input
                          type="checkbox"
                          checked={fixed.some((v) => refKey(v) === refKey(r))}
                          onChange={(e) =>
                            setFixed(
                              e.target.checked
                                ? [...fixed, r]
                                : fixed.filter((v) => refKey(v) !== refKey(r)),
                            )
                          }
                        />
                        {refLabel(r)}
                      </label>
                    ))}
                  {state.include.length > 50 && (
                    <>
                      <button
                        data-variant="secondary"
                        disabled={fixedPage === 0}
                        onClick={() => setFixedPage(fixedPage - 1)}
                      >
                        必須項目の前のページ
                      </button>
                      <span>{fixedPage * 50 + 1}件目から表示</span>
                      <button
                        data-variant="secondary"
                        disabled={(fixedPage + 1) * 50 >= state.include.length}
                        onClick={() => setFixedPage(fixedPage + 1)}
                      >
                        必須項目の次のページ
                      </button>
                    </>
                  )}
                </>
              )}
            </Disclosure>
            {!definition && !generation && (
              <button onClick={() => void work(manual)}>
                現在の選択を手動で保存
              </button>
            )}
            <button
              disabled={
                mode === "manual" ||
                !goal ||
                !profile ||
                !modelReady(ui.native, actor, profile)
              }
              onClick={() => void work(() => start())}
            >
              {generation
                ? "出力からContextの生成を依頼"
                : definition
                  ? "Run定義の生成を依頼"
                  : "Contextの選択を依頼"}
            </button>
            {generation && (
              <button
                disabled={!goal}
                onClick={() => void work(() => start(true))}
              >
                出力から手動でContext案を編集
              </button>
            )}
          </fieldset>
          {!ui.canCreate && (
            <p role="status">編集権限がないため依頼できません。</p>
          )}
          <button
            data-variant="secondary"
            disabled={busy}
            onClick={() =>
              void work(async () => {
                if (ui.native) await invoke("mcp_login");
                else
                  window.open(
                    "/auth/mcp/start",
                    "koujou-mcp-login",
                    "popup,width=700,height=760",
                  );
              })
            }
          >
            MCPへ接続
          </button>
          {operation && (
            <section
              aria-label={definition ? "定義生成の状態" : "選択操作の状態"}
            >
              <p>
                <StatusBadge
                  label="操作の状態"
                  value={
                    (definition || generation) &&
                    operation.value.state === "applied"
                      ? generation
                        ? "Contextを保存・採用済み"
                        : "定義を保存・採用済み"
                      : (operationStateLabels[operation.value.state] ??
                        operation.value.state)
                  }
                />
              </p>
              <p>{operation.value.goal}</p>
              <p>{operation.value.reason ?? ""}</p>
              {!ownsOperation && (
                <p role="status">別のクライアントが担当中です。</p>
              )}
              {generation && operationSource && (
                <>
                  <p>
                    この操作の元Run：{operationSource.run.id} ·{" "}
                    {operationSource.run.version} / 出力：
                    {operationSource.output.id} ·{" "}
                    {operationSource.output.version}
                  </p>
                  {!generationMatches && (
                    <p role="status">
                      表示中の出力と、この操作の元出力が異なります。採用・回答・継続は元出力で行ってください。
                    </p>
                  )}
                  <NavigationLink
                    related
                    to={sourceRoute(ui.workspace, operationSource)}
                  >
                    この操作の元出力を開く
                  </NavigationLink>
                </>
              )}
              <fieldset disabled={!generationMatches}>
                {operation.value.user === ui.user && (
                  <>
                    <button
                      data-variant="secondary"
                      disabled={busy}
                      onClick={() => void work(() => act("takeover"))}
                    >
                      このクライアントへ引き継ぐ
                    </button>
                    {["failed", "incomplete"].includes(
                      operation.value.state,
                    ) && (
                      <button
                        disabled={busy || !profile || !ownsOperation}
                        onClick={() => void work(() => act("resume"))}
                      >
                        {definition || generation
                          ? "接続・条件を確認して生成を再開"
                          : "接続・条件を確認して選択を再開"}
                      </button>
                    )}
                    {operation.value.state === "ready" && (
                      <button
                        disabled={busy || !profile || !ownsOperation}
                        onClick={() =>
                          void work(async () => {
                            const signature = inputSignature();
                            const p = await read<SelectionPolicy>(
                              operation.value.policy,
                            );
                            await run(operation, p.value, signature);
                          })
                        }
                      >
                        {definition || generation
                          ? "このクライアントで生成を続ける"
                          : "このクライアントで選択を続ける"}
                      </button>
                    )}
                    {operation.value.state === "question" && (
                      <>
                        <Field
                          label="質問への回答"
                          value={answer}
                          onChange={setAnswer}
                          rows={3}
                        />
                        <button
                          disabled={busy || !answer || !ownsOperation}
                          onClick={() => void work(() => act("answer"))}
                        >
                          {generation
                            ? "回答してContext生成を再開"
                            : definition
                              ? "回答して定義生成を再開"
                              : "回答して選択を再開"}
                        </button>
                      </>
                    )}
                    {operation.value.state === "proposal" && (
                      <button
                        disabled={
                          busy ||
                          !ownsOperation ||
                          generationDirty ||
                          (!!generation && !generationDraft.chosen.length)
                        }
                        onClick={() => void work(() => act("adopt"))}
                      >
                        この案を採用
                      </button>
                    )}
                    <button
                      data-variant="secondary"
                      onClick={() => {
                        epoch.current++;
                        void work(() => act("cancel"));
                      }}
                    >
                      {generation
                        ? "このContext生成を終了"
                        : definition
                          ? "この定義生成を終了"
                          : "この選択操作を終了"}
                    </button>
                  </>
                )}
                <button
                  data-variant="secondary"
                  onClick={() =>
                    void work(async () => {
                      await refresh();
                    })
                  }
                >
                  処理状況を再取得
                </button>
                <button
                  data-variant="secondary"
                  aria-busy={busy}
                  onClick={() => void work(() => loadHistory())}
                >
                  {busy ? "履歴を取得中…" : "この操作の履歴を取得"}
                </button>
                {generation && (
                  <ContextGenerationProposal
                    draft={generationDraft}
                    goal={operation.value.goal}
                    onDraft={setGenerationDraft}
                    editable={
                      ui.canCreate &&
                      !busy &&
                      generationMatches &&
                      ownsOperation &&
                      operation.value.state !== "applied" &&
                      operation.value.state !== "cancelled"
                    }
                    onRevise={(text) =>
                      void work(async () => {
                        const ref = await mutate(
                          `/operations/${encodeURIComponent(operation.id)}/actions`,
                          operation.version,
                          {
                            kind: "revise",
                            client: clientID,
                            changes_utf8: text,
                          },
                        );
                        await refresh(ref);
                      })
                    }
                  />
                )}
                {proposal && (
                  <>
                    <h4>
                      {generation
                        ? "Context案・未確認事項"
                        : definition
                          ? "定義案・未確認事項"
                          : "選択案・未確認事項"}
                    </h4>
                    {definition && proposal.value.definition_utf8 && (
                      <>
                        <p>{proposal.value.reason}</p>
                        <pre aria-label="原依頼">{operation.value.goal}</pre>
                        <textarea
                          aria-label="生成した定義JSON"
                          readOnly
                          value={proposal.value.definition_utf8}
                        />
                        <DefinitionProposal
                          before={definition.text}
                          after={proposal.value.definition_utf8}
                          paths={definitionPaths}
                          onPaths={setDefinitionPaths}
                        />
                        <button
                          data-variant="secondary"
                          onClick={() =>
                            definition.onDraft(proposal.value.definition_utf8!)
                          }
                        >
                          この案を下書きで編集
                        </button>
                      </>
                    )}
                    {proposal.value.questions.length > 0 && (
                      <ul aria-label="質問">
                        {proposal.value.questions.map((q, i) => (
                          <li key={i}>{q}</li>
                        ))}
                      </ul>
                    )}
                    {proposal.value.unknown.length > 0 && (
                      <ul aria-label="未確認事項">
                        {proposal.value.unknown.map((q, i) => (
                          <li key={i}>{q}</li>
                        ))}
                      </ul>
                    )}
                    {!definition && !generation && (
                      <>
                        <ul>
                          {proposal.value.items
                            .slice(proposalPage * 50, proposalPage * 50 + 50)
                            .map((item, i) => (
                              <li key={i}>
                                <label>
                                  <input
                                    type="checkbox"
                                    checked={state.include.some(
                                      (r) => refKey(r) === refKey(item.ref),
                                    )}
                                    onChange={(e) =>
                                      ui.store.setState({
                                        mode: "select",
                                        include: e.target.checked
                                          ? [...state.include, item.ref]
                                          : state.include.filter(
                                              (r) =>
                                                refKey(r) !== refKey(item.ref),
                                            ),
                                      })
                                    }
                                  />
                                  {refLabel(item.ref)} · {item.reason}
                                </label>
                              </li>
                            ))}
                        </ul>
                        {proposal.value.items.length > 50 && (
                          <>
                            <button
                              data-variant="secondary"
                              disabled={proposalPage === 0}
                              onClick={() => setProposalPage(proposalPage - 1)}
                            >
                              案の前のページ
                            </button>
                            <span>{proposalPage * 50 + 1}件目から表示</span>
                            <button
                              data-variant="secondary"
                              disabled={
                                (proposalPage + 1) * 50 >=
                                proposal.value.items.length
                              }
                              onClick={() => setProposalPage(proposalPage + 1)}
                            >
                              案の次のページ
                            </button>
                          </>
                        )}
                      </>
                    )}
                  </>
                )}
                <Disclosure title="固定版と原応答の履歴">
                  {() => (
                    <>
                      {history && (
                        <p>
                          {history.length
                            ? `${history.length}件を表示`
                            : "履歴はありません。"}
                        </p>
                      )}
                      <ul>
                        {history?.map((r) => (
                          <li key={r.id + "/" + r.version}>
                            <button
                              data-variant="secondary"
                              onClick={() =>
                                void work(async () =>
                                  setHistoryItem(await read(r)),
                                )
                              }
                            >
                              {r.kind} · 版 {r.version}
                            </button>
                          </li>
                        ))}
                      </ul>
                      {history?.length === 50 && (
                        <button
                          data-variant="secondary"
                          onClick={() => void work(() => loadHistory(false))}
                        >
                          履歴の次のページ
                        </button>
                      )}
                      {historyItem && (
                        <pre>{JSON.stringify(historyItem, null, 2)}</pre>
                      )}
                    </>
                  )}
                </Disclosure>
              </fieldset>
            </section>
          )}
          <Disclosure
            title={
              definition ? "共通の操作履歴を探す" : "保存済みの選択・操作を探す"
            }
          >
            {() => (
              <>
                <label>
                  種類
                  <select
                    value={kind}
                    onChange={(e) => setKind(e.target.value)}
                  >
                    <option value="operation">選択操作</option>
                    <option value="selection">保存済み選択</option>
                  </select>
                </label>
                <button
                  data-variant="secondary"
                  aria-busy={busy}
                  onClick={() => void work(() => list())}
                >
                  {busy ? "一覧を取得中…" : "一覧を取得"}
                </button>
                {records && (
                  <p>
                    {records.length
                      ? `${records.length}件を表示`
                      : "該当する記録はありません。"}
                  </p>
                )}
                <ul>
                  {records?.map((r) => (
                    <li key={r.id}>
                      <button
                        data-variant="secondary"
                        onClick={() => void work(() => choose(r))}
                      >
                        {(r.value as { goal?: string; name?: string }).goal ??
                          (r.value as { name?: string }).name ??
                          r.id}
                      </button>
                    </li>
                  ))}
                </ul>
                {records?.length === 50 && (
                  <button
                    data-variant="secondary"
                    onClick={() => void work(() => list(false))}
                  >
                    一覧の次のページ
                  </button>
                )}
              </>
            )}
          </Disclosure>
          <p role="status">{notice}</p>
        </section>
      }
    </ScreenContent>
  );
}
