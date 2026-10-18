import { useEffect, useRef, useState } from "react";
import { useStore } from "zustand";
import { invoke } from "@tauri-apps/api/core";
import { mcpRequest, syncRequest } from "./auth";
import { refKey, refLabel, type UI, type Ref } from "./context-ui";
import { ModelConnectionSettings } from "./model-connection-settings";
import { DefinitionProposal, definitionFields } from "./definition-proposal";
import { allowAnswerScope, answerScopeEpoch } from "./sync-store";
import { fieldSources } from "./workflow-json";
import {
  modelReady,
  modelRequest,
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
  type HostPort,
} from "./client-selection-host";
const clientID = crypto.randomUUID();
const pin = (v: VersionRef): VersionRef => ({ id: v.id, version: v.version });
type Proposal = {
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
const labels: Record<string, string> = {
  waiting: "処理クライアントを待機",
  ready: "選択の準備・継続待ち",
  sending: "モデルへ要求済み",
  question: "依頼者への質問",
  proposal: "確認する案があります",
  applied: "選択を保存・反映済み",
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
  onApplied: (ref: VersionRef) => Promise<void>;
  onDraft: (text: string) => void;
};
export function ClientSelection({
  ui,
  definition,
}: {
  ui: OperationUI;
  definition?: DefinitionTarget;
}) {
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
  const [records, setRecords] = useState<Saved[]>([]),
    [kind, setKind] = useState("operation"),
    [after, setAfter] = useState<{ after: number; after_id: string }>({
      after: 0,
      after_id: "",
    }),
    [history, setHistory] = useState<Saved[]>([]),
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
    setProposal(undefined);
    setRecords([]);
    setHistory([]);
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
    if (definitionCurrent.current) {
      if (expected !== undefined && expected !== inputSignature()) {
        setNotice(
          "定義を保存しました。編集中の下書きは変更されているため、現在版を取得して比較してください。",
        );
        return;
      }
      await definitionCurrent.current.onApplied(ref);
      setNotice(
        "検証した定義版を保存・採用しました。Runの作成・実行は始めていません。",
      );
      return;
    }
    const saved = await read<Selection>(ref);
    setSelection(pin(saved));
    if (expected !== undefined && expected !== inputSignature()) {
      setNotice(
        "選択を保存しました。画面の選択は変更されているため、差分を確認してから反映してください。",
      );
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
    setNotice(
      "固定した選択を保存し、入力プレビューへ反映しました。Runは開始していません。",
    );
  }
  async function refresh(ref?: VersionRef) {
    const selected = ref ?? operation;
    if (!selected) return;
    const current = await read<ClientOperation>({
      id: selected.id,
      version: "head",
    });
    if ((current.value.purpose === "definition") !== !!definition)
      throw new Error("different_operation_kind");
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
    } catch {
      setNotice(
        definition
          ? "入力・現在の定義版・権限・接続を確認してください。未確認の要求は自動再送しません。定義の下書きから手動編集もできます。"
          : "入力・現在の版・権限・接続を確認してください。未確認のモデル要求は自動再送しません。手動で選択・保存することもできます。",
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
  async function run(current: Saved<ClientOperation>, policy: SelectionPolicy) {
    if (!profile || !modelReady(ui.native, actor, profile))
      throw new Error("model_key_required");
    const mine = ++epoch.current;
    const namespace = { api: ui.api, user: ui.user, workspace: ui.workspace };
    const permission = answerScopeEpoch(namespace);
    const signature = inputSignature();
    const port: HostPort = {
      read,
      query: async (root, kind) => {
        const rows: Saved[] = [];
        let cursor = { after: 0, after_id: "" };
        for (;;) {
          const page = await call<{
            items: Saved[];
            after: number;
            after_id: string;
          }>("/query", { root, kind, limit: 100, ...cursor });
          rows.push(...page.items);
          if (page.items.length < 100) break;
          cursor = { after: page.after, after_id: page.after_id };
        }
        return rows;
      },
      write: (id, action, version, value, mutation) =>
        mutate(
          `/operations/${encodeURIComponent(id)}/${action}`,
          version,
          value,
          mutation,
        ),
      rpc: (body) =>
        mcpRequest(ui.native, ui.workspace, transport.current!, body, clientID),
      model: (body, timeout) =>
        modelRequest(ui.native, actor, profile, body, timeout),
      active: () =>
        epoch.current === mine && allowAnswerScope(namespace, permission),
    };
    if (policy.fixed.mode !== "none" || policy.definition) {
      const info = await call<{ handle: string }>(
        `/operations/${encodeURIComponent(current.id)}/transport`,
      );
      transport.current = info.handle;
    }
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
  const transport = useRef<string | undefined>(undefined);
  async function start() {
    if (!profile) throw new Error("model_required");
    const values = definition
      ? fieldSources(
          definition.text,
          definitionLocks.map((path) => path.slice(1).split("/")),
        )
      : [];
    const policy: SelectionPolicy = {
      mode,
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
      `/policies/${definition ? "definition" : "selection"}-policy:${crypto.randomUUID()}`,
      "0",
      { value: policy },
    );
    const ref = await mutate(
      `/operations/client-operation:${crypto.randomUUID()}`,
      "0",
      {
        policy: policyRef,
        target: definition?.target ??
          selection ?? {
            id: `client-selection:${crypto.randomUUID()}`,
            version: "0",
          },
        goal: definition?.request ? goal + "\n" + definition.request : goal,
        purpose: definition ? "definition" : purpose,
        model: modelSnapshot(profile, ui.workspace),
        client: clientID,
      },
    );
    const current = await read<ClientOperation>(ref);
    setOperation(current);
    setProposal(undefined);
    await run(current, policy);
  }
  async function act(action: string) {
    if (!operation) return;
    const signature = inputSignature();
    const value: Record<string, unknown> = { kind: action, client: clientID };
    if (action === "answer") {
      value.answer = definition?.request
        ? answer + "\n" + definition.request
        : answer;
      if (definition) value.definition_sources = definition.sources;
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
      await run(current, policy.value);
    }
  }
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
    <section
      className="context-preview"
      aria-label={
        definition ? "自由入力からRun定義を生成" : "Contextの自動選択と保存"
      }
    >
      <h3>
        {definition ? "自由入力からRun定義を作る" : "Contextの選択と保存"}
      </h3>
      <p>
        手動の必須・除外を保ち、許された候補から選びます。取得範囲・未確認事項・固定版は履歴で確認できます。
      </p>
      <ModelConnectionSettings
        native={ui.native}
        actor={actor}
        workspace={ui.workspace}
        selected={profile}
        onSelected={setProfile}
      />
      <fieldset disabled={!ui.canCreate || busy}>
        <legend>
          {definition ? "生成する目的と方針" : "選択する目的と方針"}
        </legend>
        <label>
          作業の目的
          <textarea
            aria-label={definition ? "Run定義を作る依頼" : "Contextを選ぶ目的"}
            value={goal}
            onChange={(e) => setGoal(e.target.value)}
          />
        </label>
        <label>
          反映方式
          <select
            value={mode}
            onChange={(e) => setMode(e.target.value as SelectionPolicy["mode"])}
          >
            <option value="auto">
              {definition
                ? "検証済みの定義を自動反映"
                : "検証済みの選択を自動反映"}
            </option>
            <option value="proposal">案を確認して採用</option>
            <option value="manual">
              {definition ? "手動で定義を編集" : "手動で選択"}
            </option>
          </select>
        </label>
        {!definition && (
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
        {definition && (
          <>
            <p>
              作業場所は接続設定とRun専用worktreeを使い、リポジトリ・commitはRun割当時に指定します。
            </p>
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
                <option value="none">Contextを参照・追加しない</option>
              </select>
            </label>
            <details>
              <summary>変更させない定義の項目</summary>
              {definitionFields.map(([path, label]) => (
                <label key={path}>
                  <input
                    type="checkbox"
                    checked={definitionLocks.includes(path)}
                    onChange={(e) =>
                      setDefinitionLocks(
                        e.target.checked
                          ? [...definitionLocks, path]
                          : definitionLocks.filter((p) => p !== path),
                      )
                    }
                  />
                  {label}を固定
                </label>
              ))}
            </details>
          </>
        )}
        <details>
          <summary>対象Scope・上限・必須項目</summary>
          <label>
            絞り込むScope ID（空欄はWorkspaceの許可範囲）
            <input value={scopes} onChange={(e) => setScopes(e.target.value)} />
          </label>
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
          <p>
            手動で追加した項目は必須として固定します。自動選択した項目も、必要に応じて固定できます。
          </p>
          {state.include.slice(fixedPage * 50, fixedPage * 50 + 50).map((r) => (
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
                disabled={fixedPage === 0}
                onClick={() => setFixedPage(fixedPage - 1)}
              >
                必須項目の前の50件
              </button>
              <span>{fixedPage * 50 + 1}件目から表示</span>
              <button
                disabled={(fixedPage + 1) * 50 >= state.include.length}
                onClick={() => setFixedPage(fixedPage + 1)}
              >
                必須項目の次の50件
              </button>
            </>
          )}
        </details>
        {!definition && (
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
          onClick={() => void work(start)}
        >
          {definition ? "Run定義の生成を依頼" : "Contextの選択を依頼"}
        </button>
      </fieldset>
      <button
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
        <section aria-label={definition ? "定義生成の状態" : "選択操作の状態"}>
          <h4>
            {definition && operation.value.state === "applied"
              ? "定義を保存・採用済み"
              : (labels[operation.value.state] ?? operation.value.state)}
          </h4>
          <p>{operation.value.goal}</p>
          <p>{operation.value.reason ?? ""}</p>
          {operation.value.user === ui.user && (
            <>
              <button
                disabled={busy}
                onClick={() => void work(() => act("takeover"))}
              >
                このクライアントへ明示して引き継ぐ
              </button>
              {["failed", "incomplete"].includes(operation.value.state) && (
                <button
                  disabled={busy || !profile}
                  onClick={() => void work(() => act("resume"))}
                >
                  {definition
                    ? "接続・条件を確認して生成を再開"
                    : "接続・条件を確認して選択を再開"}
                </button>
              )}
              {operation.value.state === "ready" && (
                <button
                  disabled={busy || !profile}
                  onClick={() =>
                    void work(async () => {
                      const p = await read<SelectionPolicy>(
                        operation.value.policy,
                      );
                      await run(operation, p.value);
                    })
                  }
                >
                  {definition
                    ? "このクライアントで生成を続ける"
                    : "このクライアントで選択を続ける"}
                </button>
              )}
              {operation.value.state === "question" && (
                <>
                  <label>
                    質問への回答
                    <textarea
                      value={answer}
                      onChange={(e) => setAnswer(e.target.value)}
                    />
                  </label>
                  <button
                    disabled={busy || !answer}
                    onClick={() => void work(() => act("answer"))}
                  >
                    {definition
                      ? "回答して定義生成を再開"
                      : "回答して選択を再開"}
                  </button>
                </>
              )}
              {operation.value.state === "proposal" && (
                <button
                  disabled={busy}
                  onClick={() => void work(() => act("adopt"))}
                >
                  この案を採用
                </button>
              )}
              <button
                onClick={() => {
                  epoch.current++;
                  void work(() => act("cancel"));
                }}
              >
                {definition ? "この定義生成を終了" : "この選択操作を終了"}
              </button>
            </>
          )}
          <button
            onClick={() =>
              void work(async () => {
                await refresh();
              })
            }
          >
            処理状況を再取得
          </button>
          <button onClick={() => void work(() => loadHistory())}>
            この操作の履歴を50件取得
          </button>
          {proposal && (
            <>
              <h4>
                {definition ? "定義案・未確認事項" : "選択案・未確認事項"}
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
                    onClick={() =>
                      definition.onDraft(proposal.value.definition_utf8!)
                    }
                  >
                    この案を下書きで編集
                  </button>
                </>
              )}
              {proposal.value.questions.map((q, i) => (
                <p key={i}>{q}</p>
              ))}
              {proposal.value.unknown.map((q, i) => (
                <p key={i}>{q}</p>
              ))}
              {!definition && (
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
                                        (r) => refKey(r) !== refKey(item.ref),
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
                        disabled={proposalPage === 0}
                        onClick={() => setProposalPage(proposalPage - 1)}
                      >
                        案の前の50件
                      </button>
                      <span>{proposalPage * 50 + 1}件目から表示</span>
                      <button
                        disabled={
                          (proposalPage + 1) * 50 >= proposal.value.items.length
                        }
                        onClick={() => setProposalPage(proposalPage + 1)}
                      >
                        案の次の50件
                      </button>
                    </>
                  )}
                  <p>
                    項目を選び直したら「現在の選択を手動で保存」で反映できます。
                  </p>
                </>
              )}
            </>
          )}
          <details>
            <summary>固定版と原応答の履歴</summary>
            {history.length === 50 && (
              <button onClick={() => void work(() => loadHistory(false))}>
                履歴の次の50件
              </button>
            )}
            <ul>
              {history.map((r) => (
                <li key={r.id + "/" + r.version}>
                  <button
                    onClick={() =>
                      void work(async () => setHistoryItem(await read(r)))
                    }
                  >
                    {r.kind} · 版 {r.version}
                  </button>
                </li>
              ))}
            </ul>
            {historyItem && <pre>{JSON.stringify(historyItem, null, 2)}</pre>}
          </details>
        </section>
      )}
      <details>
        <summary>
          {definition ? "共通の操作履歴を探す" : "保存済みの選択・操作を探す"}
        </summary>
        <select
          aria-label="選択履歴の種類"
          value={kind}
          onChange={(e) => setKind(e.target.value)}
        >
          <option value="operation">選択操作</option>
          <option value="selection">保存済み選択</option>
        </select>
        <button onClick={() => void work(() => list())}>一覧を50件取得</button>
        <ul>
          {records.map((r) => (
            <li key={r.id}>
              <button onClick={() => void work(() => choose(r))}>
                {(r.value as { goal?: string; name?: string }).goal ??
                  (r.value as { name?: string }).name ??
                  r.id}
              </button>
            </li>
          ))}
        </ul>
        {records.length === 50 && (
          <button onClick={() => void work(() => list(false))}>次の50件</button>
        )}
      </details>
      <p role="status">{notice}</p>
      <p>
        画面を離れた後は、新しいモデル要求を自動で始めません。送信済みで結果が未確認の要求は履歴に残ります。
      </p>
    </section>
  );
}
