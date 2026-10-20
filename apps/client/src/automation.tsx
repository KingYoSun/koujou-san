import {
  ScreenContent,
  NavigationLink,
  useNavigation,
  useScreenActive,
} from "./navigation";
import {
  TriageModelSettings,
  TriageAudit,
  TriageIssue,
  TriageBatch,
  type TriageModel,
} from "./triage-panels";
import { useEffect, useRef, useState, type ReactNode } from "react";
import { syncRequest } from "./auth";
import { SyncHTTPError } from "./sync-error";
import { Tabs } from "./tabs";
import { StatusBadge } from "./status-badge";
import { Disclosure } from "./disclosure";

type Ref = { id: string; version: string };
type Level = { id: string; name: string; meaning: string };
type Policy = {
  model?: TriageModel;
  external?: { connection: string; labels: Record<string, string> };
  name: string;
  scopes: string[];
  precedence: number;
  levels: Level[];
  criteria: string;
  context: Ref[];
};
type Version = Ref & {
  kind: string;
  value: unknown;
  title?: string;
  deadline?: string;
  received_at?: string;
  next_due?: string;
  policy_group?: string;
  observed_at?: string;
};
type Job = {
  id: string;
  kind: string;
  state: string;
  reason: string;
  target_id: string;
  attempts: number;
  control_version: string;
  has_checkpoint: boolean;
};
type Page = { data: { items: unknown[]; next: string } };
type Priority = {
  job?: string;
  basis?: string;
  adopted_policy?: Ref;
  target: Ref;
  policy: Ref;
  adopted: string | null;
  adopted_reason: string;
  fixed: boolean;
  recommendation: string | null;
  recommendation_reason: string;
  recommendation_policy?: Ref;
  recommendation_target?: Ref;
  state: string;
  reason: string;
};
type Control = {
  kind: string;
  controller: string;
  enabled: boolean;
  can_read: boolean;
  can_commit: boolean;
  slots: number;
  max_attempts: number;
  lease_seconds: number;
  timeout_seconds: number;
  retry_seconds: number;
  max_requests: number;
  period_seconds: number;
  budget: { amount: string; currency: string; valuation: string } | null;
};
const blankPolicy = (): Policy => ({
  name: "",
  scopes: [],
  precedence: 0,
  levels: [],
  criteria: "",
  context: [],
});
const blankControl = (kind: string): Control => ({
  kind,
  controller: "",
  enabled: false,
  can_read: false,
  can_commit: false,
  slots: 2,
  max_attempts: 2,
  lease_seconds: 180,
  timeout_seconds: 120,
  retry_seconds: 5,
  max_requests: 100,
  period_seconds: 3600,
  budget: null,
});
const states: Record<string, string> = {
  synced: "同期済み",
  observing: "外部を取得中",
  reconciling: "書込み結果を照合中",
  conflict: "競合・判断待ち",
  waiting: "待機",
  unknown: "判断不能",
  input_waiting: "入力待ち",
  superseded: "新しい入力で再評価",
  blocked: "保留",
  running: "処理中",
  retry: "再試行待ち",
  done: "処理済み",
  failed: "失敗",
  stopped: "停止",
  unevaluated: "未評価",
  evaluated: "評価済み",
  information_missing: "情報不足",
  reevaluation_waiting: "再評価待ち",
};
const reasons: Record<string, string> = {
  policy_unset: "有効な方針が未設定です",
  policy_conflict: "適用方針が競合しています",
  authority_revoked: "自動化主体の権限・有効状態を確認してください",
  basis_changed: "入力または方針が変更されています",
  deadline_or_attempts: "期限または試行上限に達しました",
  request_budget: "期間内の処理件数上限に達しました",
  cost_budget: "費用上限に達しました",
  usage_missing: "利用量が未確定です",
  usage_unavailable: "費用の根拠を確認できません",
  executor_failed: "処理に失敗しました",
  external_priority_conflict:
    "GitHubの優先度が競合しています。確認して採用値を選んでください",
  concurrent_manual_priority:
    "内外の手動変更が重なっています。確認して採用値を選んでください",
  external_level_unmapped: "採用値とGitHubラベルの対応を設定してください",
  external_observation_failed:
    "GitHubを取得できないため、保存した値を保持しています",
  batch_cost_unobserved: "先行する評価の消費・料金が確定するまで待っています",
  batch_cost_exhausted: "一括評価の費用上限に達しています",
  model_unset: "評価用モデルが未設定です",
  model_connection_unavailable: "許可されたモデル接続を利用できません",
  model_input_capacity: "評価入力が設定した容量上限を超えています",
  evaluation_context_unavailable:
    "選択Contextの参照・版・除外・送信禁止分類を確認してください",
  dependency_information_missing: "依存先の情報が不足しています",
  invalid_triage_response: "モデル応答が必要な形式や根拠を満たしていません",
  model_request_failed: "モデル要求に失敗しました",
  newer_input: "新しい入力の評価へ置き換えました",
  automation_authority_or_budget:
    "自動化主体の権限または消費上限を確認してください",
  issue_closed: "このIssueは完了しています",
};

type Surface = "priorities" | "rules" | "operations";
const surfaceModes: Record<Surface, string[]> = {
  priorities: ["priorities", "evaluations", "attempts", "history"],
  rules: ["heads", "records", "evaluations", "attempts", "history"],
  operations: ["jobs", "history", "heads", "records"],
};
const surfaceRecordKinds: Record<Surface, string[]> = {
  priorities: ["priority", "issue", "sync"],
  rules: [
    "policy",
    "activation",
    "model-input",
    "model-attempt",
    "sync",
    "batch",
  ],
  operations: ["control"],
};

export function Automation({
  native,
  workspace,
  role,
  surface = "rules",
}: {
  native: boolean;
  workspace: string;
  role: string;
  surface?: Surface;
}) {
  const navigation = useNavigation();
  const active = useScreenActive();
  const routeRef = useRef(navigation?.route);
  routeRef.current = navigation?.route;
  const view =
    navigation?.route.view ??
    (surface === "operations"
      ? "settings"
      : surface === "priorities"
        ? "queue"
        : "policies");
  const running = useRef(0);
  const base = `/automation/workspaces/${encodeURIComponent(workspace)}`;
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState("");
  const [mode, setMode] = useState(
    surface === "priorities"
      ? "priorities"
      : surface === "operations"
        ? "jobs"
        : "heads",
  );
  const [recordKind, setRecordKind] = useState(
    surface === "priorities"
      ? "priority"
      : surface === "operations"
        ? "control"
        : "policy",
  );
  const [filterID, setFilterID] = useState("");
  const [page, setPage] = useState<Page | null>(null);
  const [policy, setPolicy] = useState(blankPolicy);
  const [policyEditing, setPolicyEditing] = useState(false);
  const [policyID, setPolicyID] = useState("");
  const [policyVersion, setPolicyVersion] = useState("0");
  const [autoApply, setAutoApply] = useState(true);

  const [control, setControl] = useState(() => blankControl("triage"));
  const [controlVersion, setControlVersion] = useState("0");
  const [issueID, setIssueID] = useState("");
  const [priority, setPriority] = useState<Version | null>(null);
  const [detail, setDetail] = useState<Version | null>(null);
  const [scopeTarget, setScopeTarget] = useState<Ref | null>(null);

  const [externalState, setExternalState] = useState<{
    state: string;
    reason: string;
    observed_at?: string;
  } | null>(null);
  const [selected, setSelected] = useState<(Version & { target: Ref }) | null>(
    null,
  );
  const [priorityValue, setPriorityValue] = useState("");
  const [priorityReason, setPriorityReason] = useState("");
  const pending = useRef<{ key: string; id: string } | null>(null);
  const canEdit = role !== "viewer",
    admin = role === "admin";
  const target = issueID.startsWith("scope:") ? issueID : `scope:${issueID}`;
  const call = (path: string, body?: unknown) =>
    syncRequest(
      `${base}${path}`,
      native,
      body === undefined ? { method: "GET" } : { method: "POST", body },
    );
  const query = (request: unknown) => call("/query", request) as Promise<Page>;
  const run = async (fn: () => Promise<void>, current = () => true) => {
    running.current++;
    setBusy(true);
    setMessage("");
    try {
      await fn();
    } catch (e) {
      if (!current()) return;
      setPage(null);
      setPriority(null);
      setSelected(null);
      setDetail(null);
      setExternalState(null);
      if (
        e instanceof SyncHTTPError &&
        (e.status === 401 || e.status === 403)
      ) {
        setScopeTarget(null);
        setPolicy(blankPolicy());
        setPolicyID("");
        setPolicyVersion("0");
        setControl(blankControl(control.kind));
        setControlVersion("0");
        pending.current = null;
        setMessage("現在の閲覧・編集・管理権限を確認してください。");
      } else
        setMessage(
          "操作を完了できませんでした。入力・接続・現在の版を確認してください。",
        );
    } finally {
      running.current--;
      setBusy(running.current > 0);
    }
  };
  const mutation = (path: string, expected: string, value: object) => {
    const key = JSON.stringify({ path, expected, value });
    if (pending.current?.key !== key)
      pending.current = { key, id: crypto.randomUUID() };
    return call(path, {
      operation_id: pending.current.id,
      expected_version: expected,
      ...value,
    }) as Promise<{ data: Ref & { requested?: number; next?: string } }>;
  };
  const write = (id: string, expected: string, value: unknown) =>
    mutation(`/records/${encodeURIComponent(id)}`, expected, { value });
  const head = async (kind: string, id: string) =>
    (await query({ mode: "heads", kind, id, limit: 1 })).data.items[0] as
      Version | undefined;
  const loadPriority = async (requested = target, current = () => true) => {
    const [p, s, e] = await Promise.all([
      query({ mode: "priority", id: requested }),
      query({ mode: "selection", id: requested }),
      query({
        mode: "heads",
        kind: "sync",
        id: "priority-sync:" + requested,
        limit: 1,
      }),
    ]);
    const route = routeRef.current;
    if (
      !current() ||
      route?.screen !== "priority" ||
      route.target?.kind !== "scope" ||
      route.target.id !== requested
    )
      return;
    if (scopeTarget?.id !== requested) {
      setPriorityValue("");
      setPriorityReason("");
    }
    setPriority(p.data.items[0] as Version);
    const externalRecord = e.data.items[0] as Version | undefined;
    setExternalState(
      externalRecord
        ? {
            ...(externalRecord.value as { state: string; reason: string }),
            observed_at: externalRecord.observed_at,
          }
        : null,
    );
    const choice = s.data.items[0] as {
      target: Ref;
      policy: Version;
      reason: string;
    };
    setScopeTarget(choice.target);
    if (
      route.target?.id !== choice.target.id ||
      route.target.version !== choice.target.version
    )
      navigation?.navigate(
        { ...route, target: { ...choice.target, kind: "scope" } },
        { replace: true },
      );
    setSelected(
      choice.policy.id ? { ...choice.policy, target: choice.target } : null,
    );
    if (choice.reason) setMessage(reasons[choice.reason] ?? choice.reason);
  };
  const activation = async (enabled: boolean) => {
    const id = `activation:${policyID.replace(/^policy:/, "")}`;
    const existing = await head("activation", id);
    await write(id, existing?.version ?? "0", {
      policy: { id: policyID, version: policyVersion },
      enabled,
      auto_apply: autoApply,
    });
    setMessage(
      enabled
        ? "この方針の固定版を有効にしました。過去対象への要求は追加していません。"
        : "方針を無効にしました。未確定の処理は現在の権限と状態を再確認します。",
    );
  };
  useEffect(() => {
    if (!active) return;
    let current = true;
    const route = navigation?.route;
    let nextMode =
      surface === "priorities" && view === "queue"
        ? "priorities"
        : view === "evaluations"
          ? "evaluations"
          : view === "attempts"
            ? "attempts"
            : view === "jobs"
              ? "jobs"
              : view === "history"
                ? "history"
                : "heads";
    let nextKind =
      surface === "operations"
        ? "control"
        : surface === "priorities"
          ? "priority"
          : "policy";
    let requestedID = route?.query ?? "";
    try {
      const filter = JSON.parse(requestedID) as {
        mode: string;
        kind: string;
        id: string;
      };
      if (
        surfaceModes[surface].includes(filter.mode) &&
        (surfaceRecordKinds[surface].includes(filter.kind) ||
          filter.kind === "") &&
        typeof filter.id === "string"
      ) {
        nextMode = filter.mode;
        nextKind = filter.kind;
        requestedID = filter.id;
      }
    } catch {
      /* target reference */
    }
    setMode(nextMode);
    setRecordKind(nextKind);
    setFilterID(requestedID);
    setPage(null);
    if (
      !(surface === "priorities" && view === "queue" && route?.target) &&
      (nextMode !== "history" || requestedID)
    )
      void run(
        async () => {
          const result = await query({
            mode: nextMode,
            kind: ["heads", "records"].includes(nextMode) ? nextKind : "",
            id: requestedID,
            after: route?.cursor ?? "",
            limit: 50,
          });
          if (current) setPage(result);
        },
        () => current,
      );
    return () => {
      current = false;
    };
  }, [
    active,
    view,
    navigation?.route.query,
    navigation?.route.cursor,
    surface === "priorities" && view === "queue" && !navigation?.route.target,
  ]);
  useEffect(() => {
    const ref = navigation?.route.target;
    if (!active || !ref || (surface === "priorities" && view !== "queue"))
      return;
    let current = true;
    if (surface === "priorities" && ref.kind === "scope") {
      setIssueID(ref.id);
      void run(
        () => loadPriority(ref.id, () => current),
        () => current,
      );
    } else if (
      (surface === "rules" && ref.kind === "triage-policy") ||
      (surface === "operations" && ref.kind === "automation-control")
    )
      void run(
        async () => {
          const record = (await call(
            `/records/${encodeURIComponent(ref.id)}?version=${encodeURIComponent(ref.version)}`,
          )) as Version;
          if (!current) return;
          if (record.kind === "policy") setPolicyEditing(true);
          if (
            record.kind === "policy" &&
            (policyID !== record.id || policyVersion !== record.version)
          ) {
            setPolicy(record.value as Policy);
            setPolicyID(record.id);
            setPolicyVersion(record.version);
          }
          if (record.kind === "control") {
            setControl(record.value as Control);
            setControlVersion(record.version);
          }
        },
        () => current,
      );
    return () => {
      current = false;
    };
  }, [
    active,
    view,
    navigation?.route.target?.id,
    navigation?.route.target?.version,
  ]);
  const current = priority?.value as Priority | undefined;
  const priorityMatches =
    navigation?.route.target?.kind === "scope" &&
    current?.target.id === navigation.route.target.id;
  const content = (issueDetails: ReactNode, batchDetails: ReactNode) => (
    <ScreenContent>
      {
        <section className="team-details workflow-editor triage-editor">
          <h3>
            {surface === "priorities"
              ? "Issue優先度"
              : surface === "operations"
                ? "共通の処理運用"
                : "Triage方針と評価"}
          </h3>
          {surface !== "operations" && (
            <NavigationLink to={{ workspace, screen: "operations" }} related>
              共通の処理枠・Worker状態を確認
            </NavigationLink>
          )}
          <p>
            {surface === "operations"
              ? "HopperとTriageの自動化主体・処理枠・予算・Worker状態を管理します。設定は既存の共有レコードを使い、保存後も画面の開閉から独立して処理します。"
              : "採用値・推奨・手動固定を分けます。同じ方針・尺度の優先度順、同順位は受付順です。異なる尺度を一つの順位にせず、未評価や判断不能を最低優先度として扱いません。"}
          </p>
          <Tabs
            id={`automation-${surface}`}
            label="作業の目的"
            items={
              surface === "priorities"
                ? [
                    { id: "queue", label: "Issue優先度" },
                    { id: "evaluations", label: "評価要求" },
                    { id: "attempts", label: "根拠・消費" },
                    { id: "history", label: "対象処理の履歴" },
                  ]
                : surface === "operations"
                  ? [
                      { id: "settings", label: "主体・処理枠・予算" },
                      { id: "jobs", label: "Worker処理" },
                      { id: "history", label: "対象処理の履歴" },
                    ]
                  : [
                      { id: "policies", label: "方針" },
                      { id: "evaluations", label: "評価要求" },
                      { id: "attempts", label: "根拠・消費" },
                      { id: "history", label: "対象処理の履歴" },
                    ]
            }
            value={view}
            onChange={(next) =>
              navigation?.navigate(
                {
                  ...(navigation?.route ?? { workspace }),
                  screen:
                    surface === "priorities"
                      ? "priority"
                      : surface === "operations"
                        ? "operations"
                        : "triage",
                  view: next,
                  query:
                    surface === "priorities" &&
                    navigation?.route.target?.kind === "scope" &&
                    ["evaluations", "attempts"].includes(next)
                      ? navigation.route.target.id
                      : undefined,
                  cursor: undefined,
                },
                { focus: false },
              )
            }
          />
          <section
            role="tabpanel"
            id={`automation-${surface}-panel-${view}`}
            aria-labelledby={`automation-${surface}-tab-${view}`}
            tabIndex={0}
          >
            <details>
              <summary>記録種別・版履歴・IDで絞る（詳細操作）</summary>
              <div className="team-controls">
                <label>
                  表示
                  <select
                    value={mode}
                    onChange={(e) => {
                      setMode(e.target.value);
                      setPage(null);
                    }}
                  >
                    {surfaceModes[surface].includes("priorities") && (
                      <option value="priorities">優先度順のIssue一覧</option>
                    )}
                    {surfaceModes[surface].includes("heads") && (
                      <option value="heads">現在の版</option>
                    )}
                    {surfaceModes[surface].includes("records") && (
                      <option value="records">版履歴</option>
                    )}
                    {surfaceModes[surface].includes("evaluations") && (
                      <option value="evaluations">Triageの評価要求</option>
                    )}
                    {surfaceModes[surface].includes("attempts") && (
                      <option value="attempts">
                        Triageの根拠・不明点・消費
                      </option>
                    )}
                    {surfaceModes[surface].includes("jobs") && (
                      <option value="jobs">処理要求</option>
                    )}
                    {surfaceModes[surface].includes("history") && (
                      <option value="history">処理履歴</option>
                    )}
                  </select>
                </label>
                {["heads", "records"].includes(mode) && (
                  <label>
                    記録の種類
                    <select
                      value={recordKind}
                      onChange={(e) => {
                        setRecordKind(e.target.value);
                        setPage(null);
                      }}
                    >
                      {surfaceRecordKinds[surface].includes("policy") && (
                        <option value="policy">方針</option>
                      )}
                      {surfaceRecordKinds[surface].includes("priority") && (
                        <option value="priority">優先度</option>
                      )}
                      {surfaceRecordKinds[surface].includes("activation") && (
                        <option value="activation">有効化</option>
                      )}
                      {surfaceRecordKinds[surface].includes("control") && (
                        <option value="control">自動化主体・処理枠</option>
                      )}
                      {surfaceRecordKinds[surface].includes("issue") && (
                        <option value="issue">Issue情報</option>
                      )}
                      {surfaceRecordKinds[surface].includes("model-input") && (
                        <option value="model-input">固定した評価入力</option>
                      )}
                      {surfaceRecordKinds[surface].includes(
                        "model-attempt",
                      ) && (
                        <option value="model-attempt">
                          原応答・根拠・利用量
                        </option>
                      )}
                      {surfaceRecordKinds[surface].includes("sync") && (
                        <option value="sync">GitHub優先度の同期</option>
                      )}
                      {surfaceRecordKinds[surface].includes("batch") && (
                        <option value="batch">一括評価</option>
                      )}
                      {surfaceRecordKinds[surface].includes("") && (
                        <option value="">すべて</option>
                      )}
                    </select>
                  </label>
                )}
                <label>
                  IDで絞る
                  <input
                    value={filterID}
                    onChange={(e) => {
                      setFilterID(e.target.value);
                      setPage(null);
                    }}
                  />
                </label>
                <button
                  disabled={busy}
                  onClick={() =>
                    navigation?.navigate({
                      ...navigation.route,
                      target: undefined,
                      query: JSON.stringify({
                        mode,
                        kind: recordKind,
                        id: filterID,
                      }),
                      cursor: undefined,
                    })
                  }
                >
                  取得
                </button>
              </div>
            </details>
            <button
              disabled={busy}
              onClick={() =>
                navigation?.navigate({
                  ...navigation.route,
                  target: undefined,
                  query: JSON.stringify({
                    mode,
                    kind: recordKind,
                    id: filterID,
                  }),
                  cursor: undefined,
                })
              }
            >
              一覧を更新
            </button>
            {view === "history" && !filterID && (
              <p>
                評価要求・Worker処理から「処理履歴を見る」を選んでください。履歴の対象を指定してから取得します。
              </p>
            )}
            {surface === "priorities" &&
              navigation?.route.target?.kind === "scope" && (
                <NavigationLink
                  to={
                    navigation.returnTo?.screen === "priority" &&
                    !navigation.returnTo.target
                      ? navigation.returnTo
                      : { workspace, screen: "priority", view: "queue" }
                  }
                >
                  Issue優先度の一覧へ戻る
                </NavigationLink>
              )}
            {page &&
              !(
                surface === "priorities" &&
                view === "queue" &&
                navigation?.route.target
              ) && (
                <>
                  {mode === "priorities" ? (
                    <div
                      className="table-scroll"
                      role="region"
                      aria-label="方針・尺度ごとのIssue優先度"
                      tabIndex={0}
                    >
                      <table className="work-list">
                        <thead>
                          <tr>
                            <th>対象と目的</th>
                            <th>採用中</th>
                            <th>最新推奨</th>
                            <th>固定</th>
                            <th>評価状態・理由</th>
                            <th>比較する方針・尺度</th>
                          </tr>
                        </thead>
                        <tbody>
                          {page.data.items.map((row) => {
                            const r = row as Version,
                              p = r.value as Priority;
                            return (
                              <tr key={r.id}>
                                <th scope="row">
                                  <NavigationLink
                                    to={{
                                      ...(navigation?.route ?? {
                                        workspace,
                                        screen: "priority",
                                      }),
                                      target: {
                                        ...p.target,
                                        kind: "scope",
                                        name: r.title || p.target.id,
                                      },
                                    }}
                                    related
                                  >
                                    {r.title || p.target.id}
                                  </NavigationLink>
                                  {r.deadline && <p>期限：{r.deadline}</p>}
                                </th>
                                <td>
                                  {p.adopted ?? "未設定"}
                                  <p>{p.adopted_reason || "採用理由未設定"}</p>
                                </td>
                                <td>
                                  {p.recommendation ?? "未評価"}
                                  <p>
                                    {reasons[p.recommendation_reason] ??
                                      p.recommendation_reason}
                                  </p>
                                  {p.recommendation && (
                                    <small>
                                      推奨の尺度：
                                      {p.recommendation_policy
                                        ? `${p.recommendation_policy.id}@${p.recommendation_policy.version}`
                                        : "未取得"}
                                    </small>
                                  )}
                                </td>
                                <td>
                                  <StatusBadge
                                    label="反映"
                                    value={p.fixed ? "手動固定" : "固定なし"}
                                  />
                                </td>
                                <td>
                                  <StatusBadge
                                    label="評価"
                                    value={states[p.state] ?? p.state}
                                  />
                                  <p>{reasons[p.reason] ?? p.reason}</p>
                                </td>
                                <td>
                                  {r.policy_group ||
                                    (p.adopted_policy
                                      ? `${p.adopted_policy.id}@${p.adopted_policy.version}`
                                      : "比較尺度は未確定")}
                                </td>
                              </tr>
                            );
                          })}
                        </tbody>
                      </table>
                    </div>
                  ) : (
                    <ul>
                      {page.data.items.map((row, i) => {
                        const r = row as Version,
                          j = row as Job;
                        return (
                          <li key={i}>
                            <details>
                              <summary>
                                {mode === "priorities"
                                  ? `${r.title ?? r.id} · 採用 ${(r.value as Priority).adopted ?? "未設定"} · ${states[(r.value as Priority).state] ?? (r.value as Priority).state}`
                                  : mode === "history"
                                    ? `${states[j.state] ?? j.state} · ${(row as { at: string }).at} · ${(row as { attempt: number }).attempt}回`
                                    : `${(r.value as { name?: string })?.name || (r.kind === "control" ? (r.id.endsWith("hopper") ? "Hopperの処理設定" : "Triageの処理設定") : r.id)} ${["jobs", "evaluations"].includes(mode) ? `${states[j.state] ?? j.state} · ${j.kind} · ${j.attempts}回` : r.version}`}
                              </summary>
                              {r.title && (
                                <p>
                                  {r.title} / 採用値:{" "}
                                  {(r.value as Priority).adopted ?? "未設定"} /{" "}
                                  {(r.value as Priority).fixed
                                    ? "手動固定"
                                    : "固定なし"}{" "}
                                  /{" "}
                                  {states[(r.value as Priority).state] ??
                                    (r.value as Priority).state}
                                </p>
                              )}
                              {r.received_at && (
                                <p>
                                  受付: {r.received_at}（経過{" "}
                                  {Math.max(
                                    0,
                                    Math.floor(
                                      (Date.now() -
                                        new Date(r.received_at).getTime()) /
                                        60000,
                                    ),
                                  )}
                                  分） / 期限: {r.deadline ?? "未設定"} /
                                  次の期限評価: {r.next_due ?? "予定なし"}
                                </p>
                              )}
                              {r.policy_group && (
                                <p>
                                  比較する方針・尺度の固定版: {r.policy_group}
                                </p>
                              )}
                              {r.kind === "model-attempt" && (
                                <TriageAudit
                                  value={r.value}
                                  states={states}
                                  reasons={reasons}
                                  onUsage={(ref) =>
                                    void run(async () =>
                                      setDetail(
                                        (await syncRequest(
                                          `/usage/workspaces/${encodeURIComponent(workspace)}/records/${encodeURIComponent(ref.id)}?version=${encodeURIComponent(ref.version)}`,
                                          native,
                                          { method: "GET" },
                                        )) as Version,
                                      ),
                                    )
                                  }
                                />
                              )}
                              <Disclosure title="原記録のJSON">
                                {() => (
                                  <pre>{JSON.stringify(row, null, 2)}</pre>
                                )}
                              </Disclosure>
                            </details>
                            {["jobs", "evaluations"].includes(mode) && (
                              <>
                                <p>
                                  {reasons[j.reason] ?? j.reason}
                                  {j.has_checkpoint
                                    ? " / 再開位置を保存済み"
                                    : ""}
                                </p>
                                <button
                                  disabled={busy}
                                  onClick={() =>
                                    navigation?.navigate({
                                      ...navigation.route,
                                      view: "history",
                                      query: j.id,
                                      cursor: undefined,
                                    })
                                  }
                                >
                                  処理履歴を見る
                                </button>
                                {admin && (
                                  <>
                                    <button
                                      disabled={
                                        busy ||
                                        j.state === "done" ||
                                        j.state === "stopped"
                                      }
                                      onClick={() =>
                                        void run(async () => {
                                          await mutation(
                                            `/jobs/${encodeURIComponent(j.id)}`,
                                            j.control_version,
                                            {
                                              action: "stop",
                                              reason: "管理画面から停止",
                                            },
                                          );
                                          setMessage(
                                            "停止を記録しました。確定済みの副作用は取り消しません。",
                                          );
                                          setPage(null);
                                        })
                                      }
                                    >
                                      処理を停止
                                    </button>
                                    <button
                                      disabled={
                                        busy ||
                                        ![
                                          "blocked",
                                          "failed",
                                          "stopped",
                                        ].includes(j.state)
                                      }
                                      onClick={() =>
                                        void run(async () => {
                                          await mutation(
                                            `/jobs/${encodeURIComponent(j.id)}`,
                                            j.control_version,
                                            {
                                              action: "resume",
                                              reason: "管理画面から明示再開",
                                            },
                                          );
                                          setMessage(
                                            "現在の権限と根拠を確認し、再開を記録しました。",
                                          );
                                          setPage(null);
                                        })
                                      }
                                    >
                                      明示的に再開
                                    </button>
                                  </>
                                )}
                              </>
                            )}
                            {[
                              "issue",
                              "model-input",
                              "model-attempt",
                              "sync",
                            ].includes(r.kind) && (
                              <button
                                disabled={busy}
                                onClick={() =>
                                  void run(async () =>
                                    setDetail(
                                      (await call(
                                        `/records/${encodeURIComponent(r.id)}?version=${encodeURIComponent(r.version)}`,
                                      )) as Version,
                                    ),
                                  )
                                }
                              >
                                この固定版の本文・原応答を確認
                              </button>
                            )}
                            {r.kind === "priority" && (
                              <button
                                disabled={busy}
                                onClick={() =>
                                  navigation?.navigate(
                                    {
                                      workspace,
                                      screen: "priority",
                                      view: "queue",
                                      target: {
                                        ...(r.value as Priority).target,
                                        kind: "scope",
                                      },
                                    },
                                    { related: true },
                                  )
                                }
                              >
                                このIssueの優先度・根拠を確認
                              </button>
                            )}
                            {r.kind === "policy" && (
                              <NavigationLink
                                to={{
                                  workspace,
                                  screen: "triage",
                                  view: "policies",
                                  target: {
                                    id: r.id,
                                    version: r.version,
                                    kind: "triage-policy",
                                    name: (r.value as Policy).name,
                                  },
                                }}
                                related
                              >
                                この方針の固定版を確認・編集
                              </NavigationLink>
                            )}
                            {r.kind === "control" && (
                              <NavigationLink
                                to={{
                                  workspace,
                                  screen: "operations",
                                  view: "settings",
                                  target: {
                                    id: r.id,
                                    version: r.version,
                                    kind: "automation-control",
                                  },
                                }}
                                related
                              >
                                この種類の主体・処理枠・予算を確認
                              </NavigationLink>
                            )}
                          </li>
                        );
                      })}
                    </ul>
                  )}
                  {page.data.next && (
                    <button
                      disabled={busy}
                      onClick={() =>
                        navigation?.navigate({
                          ...navigation.route,
                          cursor: page.data.next,
                          target: undefined,
                        })
                      }
                    >
                      次の50件
                    </button>
                  )}
                </>
              )}
            {detail && (
              <section aria-label="選択したTriage固定版">
                <h3>選択した固定版</h3>
                <p>
                  {detail.id} · {detail.version}
                </p>
                <pre>{JSON.stringify(detail.value, null, 2)}</pre>
                <button onClick={() => setDetail(null)}>詳細を閉じる</button>
              </section>
            )}
            {surface === "rules" && view === "policies" && canEdit && (
              <fieldset disabled={busy}>
                <legend>Triage方針</legend>
                <button
                  onClick={() =>
                    void run(async () => {
                      const levels = (await call("/template")) as Level[];
                      setPolicy({ ...blankPolicy(), levels });
                      setPolicyID(`policy:${crypto.randomUUID()}`);
                      setPolicyVersion("0");
                      setPolicyEditing(true);
                    })
                  }
                >
                  初期テンプレートで新しい方針を作る
                </button>
                {policyEditing && (
                  <>
                    <label>
                      方針ID
                      <input
                        value={policyID}
                        readOnly={policyVersion !== "0"}
                        onChange={(e) => setPolicyID(e.target.value)}
                      />
                    </label>
                    <label>
                      名前
                      <input
                        value={policy.name}
                        onChange={(e) =>
                          setPolicy({ ...policy, name: e.target.value })
                        }
                      />
                    </label>
                    <label>
                      対象Scope ID（複数はカンマ区切り）
                      <input
                        value={policy.scopes.join(",")}
                        onChange={(e) =>
                          setPolicy({
                            ...policy,
                            scopes: e.target.value
                              .split(",")
                              .map((s) => s.trim())
                              .filter(Boolean),
                          })
                        }
                      />
                    </label>
                    <label>
                      方針の優先順（大きい値を優先）
                      <input
                        type="number"
                        value={policy.precedence}
                        onChange={(e) =>
                          setPolicy({
                            ...policy,
                            precedence: Number(e.target.value),
                          })
                        }
                      />
                    </label>
                    <label>
                      評価基準
                      <textarea
                        value={policy.criteria}
                        onChange={(e) =>
                          setPolicy({ ...policy, criteria: e.target.value })
                        }
                      />
                    </label>
                    <p>
                      優先度は上から順に扱います。名称・段階数を変更できます。未評価や失敗は優先度の段階に含めません。
                    </p>
                    {policy.levels.map((l, i) => (
                      <div className="team-controls" key={i}>
                        {(
                          [
                            ["id", "段階ID"],
                            ["name", "段階名"],
                            ["meaning", "判断基準"],
                          ] as const
                        ).map(([k, label]) => (
                          <label key={k}>
                            {label}
                            <input
                              value={l[k]}
                              onChange={(e) =>
                                setPolicy({
                                  ...policy,
                                  levels: policy.levels.map((x, n) =>
                                    n === i ? { ...x, [k]: e.target.value } : x,
                                  ),
                                })
                              }
                            />
                          </label>
                        ))}
                        <button
                          disabled={i === 0}
                          onClick={() => {
                            const levels = [...policy.levels];
                            [levels[i - 1], levels[i]] = [
                              levels[i],
                              levels[i - 1],
                            ];
                            setPolicy({ ...policy, levels });
                          }}
                        >
                          上へ
                        </button>
                        <button
                          onClick={() =>
                            setPolicy({
                              ...policy,
                              levels: policy.levels.filter((_, n) => n !== i),
                            })
                          }
                        >
                          段階を削除
                        </button>
                      </div>
                    ))}
                    <button
                      onClick={() =>
                        setPolicy({
                          ...policy,
                          levels: [
                            ...policy.levels,
                            { id: "", name: "", meaning: "" },
                          ],
                        })
                      }
                    >
                      段階を追加
                    </button>
                    <TriageModelSettings
                      value={policy.model}
                      onChange={(model) => setPolicy({ ...policy, model })}
                      call={call}
                      run={run}
                    />
                    {admin && (
                      <details>
                        <summary>GitHub優先度ラベルの双方向同期</summary>
                        <p>
                          方針ごとに接続と対応ラベルを設定します。外部で人が変えた値を固定として取り込み、同時の手動変更は保留します。既存ラベルを使い、本文・状態・根拠は送信しません。
                        </p>
                        <label>
                          GitHub接続ID
                          <input
                            value={policy.external?.connection ?? ""}
                            onChange={(e) =>
                              setPolicy({
                                ...policy,
                                external: e.target.value
                                  ? {
                                      connection: e.target.value,
                                      labels: policy.external?.labels ?? {},
                                    }
                                  : undefined,
                              })
                            }
                          />
                        </label>
                        {policy.external &&
                          policy.levels.map((level) => (
                            <label key={level.id}>
                              {level.name}のGitHubラベル
                              <input
                                value={policy.external?.labels[level.id] ?? ""}
                                onChange={(e) =>
                                  setPolicy({
                                    ...policy,
                                    external: {
                                      connection: policy.external!.connection,
                                      labels: {
                                        ...policy.external!.labels,
                                        [level.id]: e.target.value,
                                      },
                                    },
                                  })
                                }
                              />
                            </label>
                          ))}
                      </details>
                    )}
                    <button
                      disabled={
                        !policyID ||
                        !policy.name ||
                        !policy.criteria ||
                        !policy.scopes.length ||
                        !policy.levels.length
                      }
                      onClick={() =>
                        void run(async () => {
                          const id = policyID.startsWith("policy:")
                            ? policyID
                            : `policy:${policyID}`;
                          const r = await write(id, policyVersion, policy);
                          setPolicyID(r.data.id);
                          setPolicyVersion(r.data.version);
                          navigation?.navigate(
                            {
                              workspace,
                              screen: "triage",
                              view: "policies",
                              target: { ...r.data, kind: "triage-policy" },
                            },
                            { replace: true },
                          );
                          setMessage(
                            "方針の版を保存しました。有効化や過去対象の処理は開始していません。",
                          );
                        })
                      }
                    >
                      方針の版を保存
                    </button>
                    {policyVersion !== "0" && <p>選択版: {policyVersion}</p>}
                    {admin && policyVersion !== "0" && (
                      <>
                        <label>
                          <input
                            type="checkbox"
                            checked={autoApply}
                            onChange={(e) => setAutoApply(e.target.checked)}
                          />
                          有効な評価を自動反映する（手動固定は維持）
                        </label>
                        <button
                          onClick={() => void run(() => activation(true))}
                        >
                          この版を有効化
                        </button>
                        <button
                          onClick={() => void run(() => activation(false))}
                        >
                          方針を無効化
                        </button>
                        {batchDetails}
                      </>
                    )}
                  </>
                )}
              </fieldset>
            )}
            {surface === "priorities" && view === "queue" && (
              <fieldset id="triage-priority-editor" disabled={busy}>
                <legend>Issueの優先度</legend>
                <details>
                  <summary>IssueをIDで指定（詳細操作）</summary>
                  <label>
                    Issue Scope ID
                    <input
                      value={issueID}
                      onChange={(e) => {
                        setIssueID(e.target.value);
                        setPriority(null);
                        setSelected(null);
                      }}
                    />
                  </label>
                  <button
                    disabled={!issueID}
                    onClick={() =>
                      navigation?.navigate(
                        {
                          ...navigation.route,
                          target: {
                            kind: "scope",
                            id: target,
                            version: "head",
                          },
                        },
                        { related: true },
                      )
                    }
                  >
                    現在の優先度・方針を取得
                  </button>
                </details>
                {current && priorityMatches && (
                  <>
                    <p>
                      採用値: {current.adopted ?? "未設定"} /{" "}
                      {current.fixed ? "手動固定" : "固定なし（方針に従う）"} /{" "}
                      {states[current.state] ?? current.state}
                    </p>
                    <p>
                      <StatusBadge
                        label="最新推奨"
                        value={current.recommendation ?? "未評価"}
                      />{" "}
                      {reasons[current.recommendation_reason] ??
                        current.recommendation_reason}
                    </p>
                    {current.recommendation && (
                      <p>
                        推奨の尺度の固定版：
                        {current.recommendation_policy
                          ? `${current.recommendation_policy.id}@${current.recommendation_policy.version}`
                          : "未取得"}{" "}
                        / 推奨の対象版：
                        {current.recommendation_target?.version ?? "未取得"}
                      </p>
                    )}
                    <p>{reasons[current.reason] ?? current.reason}</p>
                    <p>採用理由: {current.adopted_reason || "未設定"}</p>
                    {externalState && (
                      <p
                        role={
                          externalState.state === "conflict"
                            ? "alert"
                            : undefined
                        }
                      >
                        GitHub優先度の同期:{" "}
                        {states[externalState.state] ?? externalState.state} /{" "}
                        {reasons[externalState.reason] ?? externalState.reason}{" "}
                        / 最終取得: {externalState.observed_at ?? "未取得"}
                      </p>
                    )}
                    <p>
                      採用尺度の固定版:{" "}
                      {(current.adopted_policy ?? current.policy).id} ·{" "}
                      {(current.adopted_policy ?? current.policy).version}
                    </p>
                    <p>
                      評価対象の固定版：{current.target.id}@
                      {current.target.version}
                    </p>
                    <button
                      onClick={() =>
                        navigation?.navigate({
                          ...navigation.route,
                          view: "evaluations",
                          query: current.target.id,
                          cursor: undefined,
                        })
                      }
                    >
                      このIssueの評価要求を取得
                    </button>
                    <button
                      onClick={() =>
                        navigation?.navigate({
                          ...navigation.route,
                          view: "attempts",
                          query: current.target.id,
                          cursor: undefined,
                        })
                      }
                    >
                      このIssueの根拠・不明点・消費を取得
                    </button>
                    {current.job && current.basis && (
                      <button
                        onClick={() =>
                          void run(async () =>
                            setDetail(
                              (await call(
                                `/records/${encodeURIComponent("triage-input:" + current.job)}?version=${encodeURIComponent(current.basis!)}`,
                              )) as Version,
                            ),
                          )
                        }
                      >
                        評価に使った固定入力を確認
                      </button>
                    )}
                    {issueDetails}
                  </>
                )}
                {canEdit && priority && selected && priorityMatches && (
                  <>
                    <label>
                      手動で採用する優先度
                      <select
                        value={priorityValue}
                        onChange={(e) => setPriorityValue(e.target.value)}
                      >
                        <option value="">選択してください</option>
                        {(selected.value as Policy).levels.map((l) => (
                          <option key={l.id} value={l.id}>
                            {l.name}（{l.id}）
                          </option>
                        ))}
                      </select>
                    </label>
                    {current?.recommendation && (
                      <button
                        data-variant="secondary"
                        disabled={
                          current.recommendation_policy?.id !== selected.id ||
                          current.recommendation_policy?.version !==
                            selected.version
                        }
                        title="現在の操作方針と推奨の尺度が一致する場合に選べます。異なる尺度は手動で確認してください。"
                        onClick={() =>
                          setPriorityValue(current.recommendation!)
                        }
                      >
                        最新推奨を手動採用の入力欄へ選ぶ
                      </button>
                    )}
                    <label>
                      変更・解除の理由
                      <input
                        value={priorityReason}
                        onChange={(e) => setPriorityReason(e.target.value)}
                      />
                    </label>
                    <button
                      disabled={!priorityValue || !priorityReason}
                      onClick={() =>
                        void run(async () => {
                          await write(priority.id, priority.version, {
                            target: selected.target,
                            policy: {
                              id: selected.id,
                              version: selected.version,
                            },
                            value: priorityValue,
                            reason: priorityReason,
                            unlock: false,
                          });
                          await loadPriority();
                          setMessage("手動の優先度を採用して固定しました。");
                        })
                      }
                    >
                      手動変更して固定
                    </button>
                    <button
                      disabled={!current?.fixed || !priorityReason}
                      onClick={() =>
                        void run(async () => {
                          await write(priority.id, priority.version, {
                            target: selected.target,
                            policy: {
                              id: selected.id,
                              version: selected.version,
                            },
                            value: null,
                            reason: priorityReason,
                            unlock: true,
                          });
                          await loadPriority();
                          setMessage(
                            "固定を解除しました。現在の入力と方針に有効な推奨だけを反映します。",
                          );
                        })
                      }
                    >
                      固定を解除して自動反映へ戻す
                    </button>
                  </>
                )}
              </fieldset>
            )}
            {surface === "operations" && view === "settings" && admin && (
              <fieldset disabled={busy}>
                <legend>自動化主体・処理枠・予算</legend>
                <label>
                  処理の種類
                  <select
                    value={control.kind}
                    onChange={(e) => {
                      setControl(blankControl(e.target.value));
                      setControlVersion("0");
                    }}
                  >
                    <option value="triage">Triage</option>
                    <option value="hopper">Hopper</option>
                  </select>
                </label>
                <button
                  onClick={() =>
                    void run(async () => {
                      const r = await head(
                        "control",
                        `control:${control.kind}`,
                      );
                      setControl(
                        r ? (r.value as Control) : blankControl(control.kind),
                      );
                      setControlVersion(r?.version ?? "0");
                    })
                  }
                >
                  現在の設定を読み込む
                </button>
                <p>
                  保存した管理者の現在のWorkspace権限を確認して処理します。ログインセッションの終了とは独立して動作し、権限の失効時は保留します。
                </p>
                {(
                  [
                    ["enabled", "有効にする"],
                    ["can_read", "対象の取得を許可する"],
                    ["can_commit", "結果の確定を許可する"],
                  ] as const
                ).map(([k, label]) => (
                  <label key={k}>
                    <input
                      type="checkbox"
                      checked={control[k]}
                      onChange={(e) =>
                        setControl({ ...control, [k]: e.target.checked })
                      }
                    />
                    {label}
                  </label>
                ))}
                {(
                  [
                    ["slots", "同時処理枠"],
                    ["max_attempts", "最大試行回数"],
                    ["lease_seconds", "担当期限（秒）"],
                    ["timeout_seconds", "1回の処理期限（秒）"],
                    ["retry_seconds", "再試行間隔（秒）"],
                    ["max_requests", "期間内の試行上限"],
                    ["period_seconds", "集計期間（秒）"],
                  ] as const
                ).map(([k, label]) => (
                  <label key={k}>
                    {label}
                    <input
                      type="number"
                      min={k === "retry_seconds" ? 0 : 1}
                      value={control[k]}
                      onChange={(e) =>
                        setControl({ ...control, [k]: Number(e.target.value) })
                      }
                    />
                  </label>
                ))}
                <label>
                  <input
                    type="checkbox"
                    checked={control.budget !== null}
                    onChange={(e) =>
                      setControl({
                        ...control,
                        budget: e.target.checked
                          ? { amount: "", currency: "", valuation: "estimated" }
                          : null,
                      })
                    }
                  />
                  記録済み費用による上限を使う
                </label>
                {control.budget && (
                  <>
                    <label>
                      費用上限
                      <input
                        value={control.budget.amount}
                        onChange={(e) =>
                          setControl({
                            ...control,
                            budget: {
                              ...control.budget!,
                              amount: e.target.value,
                            },
                          })
                        }
                      />
                    </label>
                    <label>
                      通貨
                      <input
                        value={control.budget.currency}
                        onChange={(e) =>
                          setControl({
                            ...control,
                            budget: {
                              ...control.budget!,
                              currency: e.target.value,
                            },
                          })
                        }
                      />
                    </label>
                    <label>
                      評価種別
                      <select
                        value={control.budget.valuation}
                        onChange={(e) =>
                          setControl({
                            ...control,
                            budget: {
                              ...control.budget!,
                              valuation: e.target.value,
                            },
                          })
                        }
                      >
                        <option value="reported">報告額</option>
                        <option value="estimated">推定額</option>
                        <option value="api_equivalent">API料金換算額</option>
                        <option value="allocated">配賦額</option>
                      </select>
                    </label>
                  </>
                )}
                <p>
                  利用量の遅延・欠測がある経路では厳密な金額上限を保証せず、件数上限を併用します。HopperとTriageの枠・予算は別です。
                </p>
                <button
                  onClick={() =>
                    void run(async () => {
                      const r = await write(
                        `control:${control.kind}`,
                        controlVersion,
                        control,
                      );
                      setControlVersion(r.data.version);
                      setMessage(
                        "自動化主体・処理枠・予算の版を保存しました。",
                      );
                    })
                  }
                >
                  管理設定を保存
                </button>
              </fieldset>
            )}
            {message && <p role="status">{message}</p>}
          </section>
        </section>
      }
    </ScreenContent>
  );
  const withIssue = (batchDetails: ReactNode) =>
    surface === "priorities" && scopeTarget ? (
      <TriageIssue
        key={`${scopeTarget.id}@${scopeTarget.version}`}
        scope={scopeTarget}
        canEdit={canEdit}
        admin={admin}
        call={call}
        write={write}
        run={run}
        render={(issueDetails) => content(issueDetails, batchDetails)}
      />
    ) : (
      content(null, batchDetails)
    );
  return surface === "rules" ? (
    <TriageBatch
      key={`${policyID}@${policyVersion}`}
      policy={{ id: policyID, version: policyVersion }}
      scopes={policy.scopes}
      call={call}
      write={write}
      run={run}
      render={withIssue}
    />
  ) : (
    withIssue(null)
  );
}
