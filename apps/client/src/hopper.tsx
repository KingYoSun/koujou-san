import {
  ScreenContent,
  NavigationLink,
  useNavigation,
  useScreenActive,
} from "./navigation";
import {
  ServerModelFields,
  type ModelConnection,
  type SemanticSettings,
} from "./server-model";
import { useEffect, useRef, useState } from "react";
import { syncRequest } from "./auth";
import { SyncHTTPError } from "./sync-error";
import { Tabs, TabPanel } from "./tabs";
import { StatusBadge } from "./status-badge";
import { Disclosure } from "./disclosure";
import { DetailPane } from "./panes";
import { Field, jsonError } from "./field";
import { HopperRuleFields } from "./hopper-rule-fields";
import { applyFields } from "./workflow-json";

type Ref = { id: string; version: string };
// Both record pages and mutation replies contain fields outside a fixed Ref.
export const hopperRef = (ref: Ref): Ref => ({
  id: ref.id,
  version: ref.version,
});
type Version = Ref & { kind: string; position: number; value: unknown };
type Page = { items: Version[]; after: number; after_id: string };
type Evaluation = {
  definition: Ref;
  source: { target: Ref; facts: unknown; previous_evaluation?: Ref };
  reevaluation?: string;
  semantic?: {
    input?: Ref;
    response?: Ref;
    state: string;
    reason?: string;
    decision?: {
      verdict: string;
      reason: string;
      candidate: unknown;
      evidence: unknown[];
      unknown: string[];
    };
    usage: Ref[];
    costs: Ref[];
    usage_state: string;
  };
  root: string;
  depth: number;
  state: string;
  reason: string;
  verdict: string;
  dry_run: boolean;
  destination?: Ref;
  cost?: {
    state: string;
    amount: string | null;
    currency: string;
    valuation: string;
  };
};
const states: Record<string, string> = {
  published: "発行済み",
  linked: "既存Issueへ関連付け",
  proposal: "提案・採用待ち",
  held: "保留",
  blocked: "停止・上限・権限の確認待ち",
  unknown: "判断不能",
  input_waiting: "入力待ち",
  failed: "意味判断に失敗",
  complete: "意味判断を保存済み",
  not_matched: "条件不成立",
  matched: "条件成立・発行なし",
};
const kinds: Record<string, string> = {
  definition: "定義",
  activation: "有効化",
  evaluation: "評価",
  chain: "連鎖",
  source: "元結果",
};
const reasons: Record<string, string> = {
  historical_no_publication: "過去の結果を発行せずに試行しました",
  chain_limit_or_stopped:
    "連鎖が停止中、または深度・件数・評価・期間の上限に達しました",
  workflow_owns_retry: "既存工程が再試行を担当します",
  required_evidence_unavailable: "必要な証拠がまだ共有されていません",
  result_field_unavailable: "結果の項目を取得・判定できません",
  problem_identity_missing: "同じ問題を照合する識別情報がありません",
  existing_open_issue: "同じ問題の未完了Issueを使います",
  recurrence: "完了済みの問題が再発したため提案しました",
  duplicate_problem: "重複候補として保留しています",
  proposal_policy: "定義の提案方針に従って採用を待っています",
  assignment_resumed: "同じRunの割当待ちを明示的に再開しました",
  cost_unobserved: "費用の欠測・未算定・遅着があるため保留しています",
  cost_exhausted: "記録済み費用が上限に達しました",
  duplicate_basis_unavailable: "照合先の現在の参照権限を確認してください",
  publication_authority_or_input_denied:
    "発行・実行・入力の現在の許可を確認してください",
  mapped_input_unavailable: "発行先へ渡す入力を確定できません",
  template_input_invalid: "テンプレートと入力の対応を確認してください",
  model_connection_unavailable:
    "サーバーのモデル接続設定・認証・稼働状態を確認してください",
  model_not_permitted: "指定モデルとfallbackの許可設定を確認してください",
  model_request_failed:
    "モデル要求が失敗しました。原応答と試行履歴を確認してください",
  invalid_semantic_response:
    "応答形式または引用した証拠が不正のため採用していません",
  evaluation_context_unavailable:
    "評価Contextの権限・送信分類・固定版を確認してください",
  model_input_capacity: "評価入力の容量上限を超えたため、省略せず保留しました",
};

export function Hopper({
  native,
  workspace,
  role,
}: {
  native: boolean;
  workspace: string;
  role: string;
}) {
  const navigation = useNavigation();
  const active = useScreenActive();
  const running = useRef(0);
  const view = navigation?.route.view ?? "rules";
  const [fields, setFields] = useState<Record<string, string | null>>({});
  const base = `/hopper/workspaces/${encodeURIComponent(workspace)}`;
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState("");
  const [page, setPage] = useState<Page | null>(null);
  const [kind, setKind] = useState("definition");
  const [mode, setMode] = useState("heads");
  const [root, setRoot] = useState("");
  const [id, setID] = useState("");
  const [version, setVersion] = useState("0");
  const [draft, setDraft] = useState("");
  const draftOwner = JSON.stringify([id, version, navigation?.route.target]);
  const draftOwnerRef = useRef(draftOwner);
  draftOwnerRef.current = draftOwner;
  const activeRef = useRef(active);
  activeRef.current = active;
  const [saved, setSaved] = useState<Ref | null>(null);
  const [selected, setSelected] = useState<Version | null>(null);
  const [pastKind, setPastKind] = useState("github");
  const [pastTarget, setPastTarget] = useState("*");
  const [pastAfter, setPastAfter] = useState(0);
  const [trial, setTrial] = useState<Version | null>(null);
  const [bounds, setBounds] = useState("");
  const [connections, setConnections] = useState<ModelConnection[] | null>(
    null,
  );
  const [attempts, setAttempts] = useState<Page | null>(null);
  const [detail, setDetail] = useState<Version | null>(null);
  const pending = useRef<{ key: string; id: string } | null>(null);
  const admin = role === "admin";
  const call = <T,>(path: string, body?: unknown) =>
    syncRequest(
      `${base}${path}`,
      native,
      body === undefined ? { method: "GET" } : { method: "POST", body },
    ) as Promise<T>;
  const run = async (f: () => Promise<void>, current = () => true) => {
    running.current++;
    setBusy(true);
    setNotice("");
    try {
      await f();
    } catch (e) {
      if (!current()) return;
      setSelected(null);
      setTrial(null);
      setAttempts(null);
      setDetail(null);
      if (
        e instanceof SyncHTTPError &&
        (e.status === 401 || e.status === 403)
      ) {
        setDraft("");
        setFields({});
        setSaved(null);
        setConnections(null);
        setID("");
        setVersion("0");
        pending.current = null;
        setNotice("現在の閲覧・編集・管理権限を確認してください。");
      } else
        setNotice(
          "操作を完了できませんでした。入力・接続・現在の版を確認してください。",
        );
    } finally {
      running.current--;
      setBusy(running.current > 0);
    }
  };
  const mutate = async (path: string, expected: string, value: object) => {
    const key = JSON.stringify({ path, expected, value });
    if (pending.current?.key !== key)
      pending.current = { key, id: crypto.randomUUID() };
    const result = await call<{ data?: Ref; error?: unknown }>(path, {
      operation_id: pending.current.id,
      expected_version: expected,
      ...value,
    });
    if (!result.data || result.error) throw new Error("not_accepted");
    pending.current = null;
    return hopperRef(result.data);
  };
  const query = async (after = 0, after_id = "") =>
    setPage(
      await call<Page>("/query", {
        mode,
        kind,
        root,
        after,
        after_id,
        limit: 50,
      }),
    );
  const head = async (recordID: string) => {
    const p = await call<Page>("/query", {
      mode: "heads",
      id: recordID,
      limit: 1,
    });
    return p.items[0];
  };
  const activation = async (enabled: boolean) => {
    if (!saved) return;
    const key = `hopper-active:${saved.id.replace(/^hopper:/, "")}`;
    const current = await head(key);
    await mutate(
      `/records/${encodeURIComponent(key)}`,
      current?.version ?? "0",
      { value: { definition: saved, enabled, controller: "" } },
    );
    setNotice(
      enabled ? "保存した版を有効化しました。" : "このHopperを無効化しました。",
    );
  };
  const decide = async (record: Version, action: string) => {
    await mutate(
      `/decisions/${encodeURIComponent(record.id)}`,
      record.version,
      { action },
    );
    setSelected(
      await call<Version>(`/records/${encodeURIComponent(record.id)}`),
    );
    await query();
  };
  const chain = async (stopped: boolean) => {
    if (!selected) return;
    const e = selected.value as Evaluation;
    const key = `hopper-chain:${e.root}`;
    const current = await head(key);
    await mutate(
      `/decisions/${encodeURIComponent(key)}`,
      current?.version ?? "0",
      {
        action: stopped ? "stop" : "resume",
        ...(bounds.trim() ? { bounds: JSON.parse(bounds) } : {}),
      },
    );
    setNotice(
      stopped
        ? "この起点からの連鎖を停止しました。"
        : "連鎖を再開可能にしました。",
    );
  };
  let semantic: SemanticSettings | undefined;
  try {
    semantic = JSON.parse(applyFields(draft, fields)).semantic;
  } catch {
    /* The JSON editor can be incomplete. */
  }
  const editSemantic = (change: Partial<SemanticSettings>) => {
    const value = JSON.parse(applyFields(draft, fields));
    value.semantic = { ...value.semantic, ...change };
    setDraft(JSON.stringify(value, null, 2));
    setFields({});
  };
  const fixedDetail = async (ref: Ref) =>
    setDetail(
      await call<Version>(
        `/records/${encodeURIComponent(ref.id)}?version=${encodeURIComponent(ref.version)}`,
      ),
    );
  const loadAttempts = async (record: Version, after = 0, after_id = "") =>
    setAttempts(
      await call<Page>("/query", {
        mode: "attempts",
        id: record.id,
        limit: 50,
        after,
        after_id,
      }),
    );
  useEffect(() => {
    if (!active) return;
    let current = true;
    const route = navigation?.route;
    let nextKind =
      view === "evaluations"
        ? "evaluation"
        : view === "chains"
          ? route?.query
            ? "evaluation"
            : "chain"
          : "definition";
    let requestedRoot = route?.query ?? "",
      nextMode =
        view === "evaluations" || (view === "chains" && route?.query)
          ? "history"
          : "heads";
    try {
      const filter = JSON.parse(requestedRoot) as {
        kind: string;
        mode: string;
        root: string;
      };
      if (
        ["definition", "activation", "evaluation", "chain"].includes(
          filter.kind,
        ) &&
        ["heads", "history"].includes(filter.mode) &&
        typeof filter.root === "string"
      ) {
        nextKind = filter.kind;
        nextMode = filter.mode;
        requestedRoot = filter.root;
      }
    } catch {
      /* plain chain root */
    }
    setKind(nextKind);
    setMode(nextMode);
    let after = 0,
      after_id = "";
    try {
      const cursor = JSON.parse(route?.cursor ?? "[]") as [number, string];
      after = typeof cursor[0] === "number" ? cursor[0] : 0;
      after_id = typeof cursor[1] === "string" ? cursor[1] : "";
    } catch {
      /* first page */
    }
    setRoot(requestedRoot);
    if (view !== "trial")
      void run(
        async () => {
          const result = await call<Page>("/query", {
            mode: nextMode,
            kind: nextKind,
            root: requestedRoot,
            after,
            after_id,
            limit: 50,
          });
          if (current) setPage(result);
        },
        () => current,
      );
    return () => {
      current = false;
    };
  }, [active, view, navigation?.route.query, navigation?.route.cursor]);
  useEffect(() => {
    if (!active) return;
    let current = true;
    const target = navigation?.route.target;
    setSelected(null);
    setAttempts(null);
    setDetail(null);
    if (target?.kind.startsWith("hopper-"))
      void run(
        async () => {
          const record = await call<Version>(
            `/records/${encodeURIComponent(target.id)}?version=${encodeURIComponent(target.version)}`,
          );
          if (!current) return;
          setSelected(record);
          if (record.kind === "definition") {
            if (id !== record.id || version !== record.version) {
              setDraft(JSON.stringify(record.value, null, 2));
              setFields({});
            }
            setID(record.id);
            setVersion(record.version);
            setSaved(hopperRef(record));
          }
        },
        () => current,
      );
    return () => {
      current = false;
    };
  }, [active, navigation?.route.target?.id, navigation?.route.target?.version]);
  let composed: string | undefined;
  try {
    composed = applyFields(draft, fields);
  } catch {
    /* invalid fields remain editable */
  }
  const boundsError = bounds.trim() ? jsonError(bounds) : undefined;
  const boundsField = (
    <Field
      label="再開時の連鎖上限（JSON）"
      value={bounds}
      rows={4}
      onChange={setBounds}
      error={boundsError}
    />
  );
  return (
    <ScreenContent>
      {
        <section className="workspace-management workflow-editor hopper-editor">
          <NavigationLink to={{ workspace, screen: "operations" }} related>
            共通の処理枠・Worker状態を確認
          </NavigationLink>
          <p role="status">{notice}</p>
          <Tabs
            id="hopper-purpose"
            label="Hopperの目的"
            items={[
              { id: "rules", label: "ルール" },
              {
                id: "trial",
                label: "発行せずに試す",
                disabledReason: admin ? undefined : "管理権限が必要です",
              },
              { id: "evaluations", label: "評価・発行履歴" },
              { id: "chains", label: "連鎖・停止" },
            ]}
            value={view}
            onChange={(next) =>
              navigation?.navigate(
                {
                  workspace,
                  screen: "hopper",
                  view: next,
                  ...(next === "trial" && saved
                    ? { target: { ...saved, kind: "hopper-definition" } }
                    : {}),
                },
                { focus: false },
              )
            }
          />
          <TabPanel id="hopper-purpose" value={view} active={view}>
            {view !== "trial" && (
              <fieldset disabled={busy}>
                <legend>
                  {view === "evaluations"
                    ? "評価・発行履歴"
                    : view === "chains"
                      ? "連鎖の起点と停止状態"
                      : "ルール一覧"}
                </legend>
                <details>
                  <summary>履歴の範囲・記録種別・起点</summary>
                  <label>
                    表示対象
                    <select
                      value={kind}
                      onChange={(e) => {
                        setKind(e.target.value);
                        setSelected(null);
                      }}
                    >
                      <option value="definition">定義</option>
                      <option value="activation">有効な版と停止状態</option>
                      <option value="evaluation">評価と発行</option>
                      <option value="chain">連鎖の停止・再開</option>
                    </select>
                  </label>
                  <label>
                    版の範囲
                    <select
                      value={mode}
                      onChange={(e) => setMode(e.target.value)}
                    >
                      <option value="heads">現在</option>
                      <option value="history">履歴・過去の試行を含む</option>
                    </select>
                  </label>
                  <label>
                    起点で絞る
                    <input
                      value={root}
                      onChange={(e) => setRoot(e.target.value)}
                    />
                  </label>
                </details>
                <button
                  data-variant="secondary"
                  aria-busy={busy}
                  onClick={() =>
                    navigation?.navigate({
                      ...navigation.route,
                      target: undefined,
                      query: JSON.stringify({ kind, mode, root }),
                      cursor: undefined,
                    })
                  }
                >
                  {busy ? "一覧を取得中…" : "Hopperの一覧を取得"}
                </button>
                {page && (
                  <p aria-live="polite">
                    {page.items.length
                      ? `${page.items.length}件を表示${page.items.length === 50 ? "（続きあり）" : ""}`
                      : "一致する記録がありません。"}
                  </p>
                )}
                {!!page?.items.length && (
                  <ul>
                    {page.items.map((r) => (
                      <li key={`${r.id}/${r.version}`}>
                        <NavigationLink
                          to={{
                            ...(navigation?.route ?? {
                              workspace,
                              screen: "hopper",
                            }),
                            target: {
                              ...hopperRef(r),
                              kind: `hopper-${r.kind}`,
                            },
                          }}
                          related
                          aria-current={
                            selected?.id === r.id &&
                            selected.version === r.version
                              ? "page"
                              : undefined
                          }
                        >
                          {r.kind === "definition"
                            ? (r.value as { name: string }).name
                            : (states[(r.value as Evaluation).state] ??
                              r.kind)}{" "}
                          <small>{r.id}</small>
                        </NavigationLink>
                      </li>
                    ))}
                  </ul>
                )}
                {page?.items.length === 50 && (
                  <button
                    data-variant="secondary"
                    aria-busy={busy}
                    onClick={() =>
                      navigation?.navigate({
                        ...navigation.route,
                        target: undefined,
                        cursor: JSON.stringify([page.after, page.after_id]),
                      })
                    }
                  >
                    {busy ? "一覧を取得中…" : "一覧の次のページ"}
                  </button>
                )}
              </fieldset>
            )}
            {view === "rules" && role !== "viewer" && (
              <fieldset disabled={busy}>
                <legend>Hopper定義を編集</legend>
                <p>
                  編集中：{id || "未選択"} / 前提版：{version} /
                  有効化する保存版：{saved?.version ?? "未保存"}
                </p>
                <button
                  onClick={() =>
                    void run(async () => {
                      setDraft(
                        JSON.stringify(await call("/template"), null, 2),
                      );
                      setFields({});
                      setID(`hopper:${crypto.randomUUID()}`);
                      setVersion("0");
                      setSaved(null);
                    })
                  }
                >
                  初期設定で新しいHopperを作る
                </button>
                <label>
                  定義ID
                  <input
                    value={id}
                    onChange={(e) => {
                      setID(e.target.value);
                      setSaved(null);
                      setVersion("0");
                    }}
                  />
                </label>
                {draft && (
                  <HopperRuleFields
                    text={draft}
                    fields={fields}
                    onChange={(key, raw) =>
                      setFields({ ...fields, [key]: raw })
                    }
                  >
                    <button
                      data-variant="secondary"
                      onClick={() =>
                        void run(async () => {
                          setConnections(
                            await call<ModelConnection[]>("/model-connections"),
                          );
                        })
                      }
                    >
                      サーバーのモデル接続を取得
                    </button>
                    {connections && !connections.length && (
                      <p>利用できるサーバー接続がありません。</p>
                    )}
                    <button
                      disabled={!draft || !connections?.length || !!semantic}
                      onClick={() =>
                        void run(async () => {
                          const value = JSON.parse(applyFields(draft, fields)),
                            connection = connections![0],
                            model = connection.models[0];
                          value.semantic = {
                            ...(await call<SemanticSettings>(
                              "/semantic-template",
                            )),
                            connection: connection.id,
                            revision: connection.revision,
                            model: model.requested,
                            allowed_models: model.allowed_actual,
                          };
                          value.condition = {
                            all: [value.condition, { semantic: true }],
                          };
                          setDraft(JSON.stringify(value, null, 2));
                          setFields({});
                        })
                      }
                    >
                      現在の条件に意味判断を追加
                    </button>
                    {semantic && (
                      <fieldset>
                        <legend>サーバーモデルによる意味判断</legend>
                        <ServerModelFields
                          settings={semantic}
                          connections={connections ?? []}
                          onChange={editSemantic}
                        />
                        <label>
                          <input
                            type="checkbox"
                            checked={semantic.reevaluate_evidence}
                            onChange={(e) =>
                              editSemantic({
                                reevaluate_evidence: e.target.checked,
                              })
                            }
                          />
                          必要な証拠の遅着・変更時に関連評価だけ再評価する
                        </label>
                      </fieldset>
                    )}
                  </HopperRuleFields>
                )}
                <button
                  disabled={!composed}
                  data-variant="secondary"
                  onClick={() => {
                    if (composed) {
                      setDraft(composed);
                      setFields({});
                    }
                  }}
                >
                  項目をJSONへ反映
                </button>
                <Disclosure title="定義のJSON原文">
                  {() => (
                    <Field
                      label="条件・発行先・連鎖上限・重複の設定（JSON）"
                      value={draft}
                      rows={16}
                      onChange={(next) => {
                        setDraft(next);
                        setFields({});
                      }}
                      error={jsonError(draft)}
                    />
                  )}
                </Disclosure>
                <button
                  disabled={!id || !composed}
                  onClick={() =>
                    void run(async () => {
                      const pin = await mutate(
                        `/records/${encodeURIComponent(id)}`,
                        version,
                        { value: JSON.parse(applyFields(draft, fields)) },
                      );
                      if (draftOwnerRef.current !== draftOwner) return;
                      setSaved(pin);
                      setVersion(pin.version);
                      setDraft(applyFields(draft, fields));
                      setFields({});
                      if (activeRef.current)
                        navigation?.navigate(
                          {
                            workspace,
                            screen: "hopper",
                            view: "rules",
                            target: { ...pin, kind: "hopper-definition" },
                          },
                          { replace: true },
                        );
                      setNotice("定義の版を保存しました。");
                    })
                  }
                >
                  定義の版を保存
                </button>
                {admin && (
                  <>
                    <button
                      disabled={!saved}
                      onClick={() => void run(() => activation(true))}
                    >
                      保存した版を有効化
                    </button>
                    <button
                      disabled={!saved}
                      onClick={() => void run(() => activation(false))}
                    >
                      このHopperを無効化
                    </button>
                  </>
                )}
              </fieldset>
            )}
            {view === "trial" && admin && (
              <fieldset disabled={busy || !saved}>
                <legend>過去の結果を発行せずに試す</legend>
                {!saved && <p>保存した定義が未選択です。</p>}
                <label>
                  結果の種類
                  <select
                    value={pastKind}
                    onChange={(e) => {
                      setPastKind(e.target.value);
                      setPastAfter(0);
                    }}
                  >
                    <option value="github">GitHubの外部CI</option>
                    <option value="gitea">GiteaのCI・status</option>
                    <option value="run">Run</option>
                  </select>
                </label>
                <label>
                  対象
                  <input
                    value={pastTarget}
                    onChange={(e) => {
                      setPastTarget(e.target.value);
                      setPastAfter(0);
                    }}
                  />
                </label>
                <button
                  onClick={() =>
                    void run(async () => {
                      const pin = await mutate("/evaluate-past", "0", {
                        definition: saved,
                        kind: pastKind,
                        target: pastTarget,
                        after: pastAfter,
                        limit: 50,
                      });
                      const result = await call<Version>(
                        `/records/${encodeURIComponent(pin.id)}?version=${encodeURIComponent(pin.version)}`,
                      );
                      setTrial(result);
                    })
                  }
                >
                  過去50件を発行せずに評価
                </button>
                {trial && (
                  <>
                    <p>
                      {(trial.value as { items: Ref[] }).items.length}
                      件を受け付けました。
                      {(trial.value as { items: Ref[] }).items.some(
                        (item) => !item.version,
                      )
                        ? "サーバーで処理します。"
                        : "発行しない評価結果を保存しました。"}
                    </p>
                    <ol>
                      {(trial.value as { items: Ref[] }).items.map((ref, i) => (
                        <li key={ref.id + ref.version}>
                          {ref.version ? (
                            <NavigationLink
                              to={{
                                workspace,
                                screen: "hopper",
                                view: "evaluations",
                                target: {
                                  ...hopperRef(ref),
                                  kind: "hopper-evaluation",
                                },
                              }}
                              related
                            >
                              試行結果 {i + 1} を確認
                            </NavigationLink>
                          ) : (
                            <span>評価 {i + 1} はサーバーで処理待ちです。</span>
                          )}
                        </li>
                      ))}
                    </ol>
                    <details>
                      <summary>受付範囲と参照の詳細</summary>
                      <pre>{JSON.stringify(trial.value, null, 2)}</pre>
                    </details>
                    <button
                      data-variant="secondary"
                      onClick={() => {
                        setPastAfter((trial.value as { after: number }).after);
                        setTrial(null);
                      }}
                    >
                      この続きの範囲を選ぶ
                    </button>
                  </>
                )}
              </fieldset>
            )}
          </TabPanel>
        </section>
      }
      {selected && (
        <DetailPane>
          <section
            aria-label={`選択した${kinds[selected.kind] ?? selected.kind}の詳細`}
          >
            <h4>{selected.id}</h4>
            {selected.kind === "evaluation" &&
              (() => {
                const e = selected.value as Evaluation;
                return (
                  <>
                    <p>{reasons[e.reason] ?? e.reason}</p>
                    {e.semantic && (
                      <section aria-label="意味判断の根拠と消費">
                        <h5>
                          意味判断：
                          {states[e.semantic.state] ?? e.semantic.state}
                        </h5>
                        <p>
                          {reasons[e.semantic.reason ?? ""] ??
                            e.semantic.reason}
                        </p>
                        <p>
                          この評価のモデル消費：{e.semantic.usage.length}件 /
                          費用：
                          {e.semantic.usage_state === "complete"
                            ? "算定済み"
                            : e.semantic.usage_state === "not_applicable"
                              ? "要求なし"
                              : "欠測・未算定あり"}
                        </p>
                        {e.semantic.decision && (
                          <>
                            <p>{e.semantic.decision.reason}</p>
                            <pre>
                              {JSON.stringify(
                                {
                                  candidate: e.semantic.decision.candidate,
                                  evidence: e.semantic.decision.evidence,
                                  unknown: e.semantic.decision.unknown,
                                },
                                null,
                                2,
                              )}
                            </pre>
                          </>
                        )}
                        {e.semantic.input && (
                          <button
                            disabled={busy}
                            onClick={() =>
                              void run(() => fixedDetail(e.semantic!.input!))
                            }
                          >
                            固定した評価入力を確認
                          </button>
                        )}
                        {e.semantic.response && (
                          <button
                            disabled={busy}
                            onClick={() =>
                              void run(() => fixedDetail(e.semantic!.response!))
                            }
                          >
                            採用したモデルの原応答を確認
                          </button>
                        )}
                        <button
                          data-variant="secondary"
                          disabled={busy}
                          onClick={() => void run(() => loadAttempts(selected))}
                        >
                          この評価の試行・消費履歴を取得
                        </button>
                        {attempts && (
                          <>
                            <ul>
                              {attempts.items.map((a) => (
                                <li key={`${a.id}/${a.version}`}>
                                  <button
                                    disabled={busy}
                                    onClick={() =>
                                      void run(() => fixedDetail(a))
                                    }
                                  >
                                    試行{" "}
                                    {(a.value as { number: number }).number} ·{" "}
                                    {a.id}
                                  </button>
                                </li>
                              ))}
                            </ul>
                            {attempts.items.length === 50 && (
                              <button
                                data-variant="secondary"
                                disabled={busy}
                                aria-busy={busy}
                                onClick={() =>
                                  void run(() =>
                                    loadAttempts(
                                      selected,
                                      attempts.after,
                                      attempts.after_id,
                                    ),
                                  )
                                }
                              >
                                {busy ? "試行を取得中…" : "試行の次のページ"}
                              </button>
                            )}
                          </>
                        )}
                        {admin && (
                          <button
                            disabled={busy}
                            onClick={() =>
                              void run(() => decide(selected, "reevaluate"))
                            }
                          >
                            同じ結果を新しい履歴で再評価
                          </button>
                        )}
                      </section>
                    )}
                    {e.source.previous_evaluation && (
                      <button
                        disabled={busy}
                        onClick={() =>
                          void run(async () => {
                            await fixedDetail(e.source.previous_evaluation!);
                          })
                        }
                      >
                        前の評価と発行先を確認
                      </button>
                    )}
                    {e.reevaluation && (
                      <p>再評価を受付済み：{e.reevaluation}</p>
                    )}
                    {e.cost && (
                      <p>
                        記録済み費用（{e.cost.valuation}）：
                        {e.cost.amount === null
                          ? "未確定"
                          : `${e.cost.amount} ${e.cost.currency}`}
                      </p>
                    )}
                    <p>
                      条件の判定：
                      {e.verdict === "true"
                        ? "成立"
                        : e.verdict === "false"
                          ? "不成立"
                          : "判断不能"}{" "}
                      / 深度：{e.depth}
                    </p>
                    <div className="team-controls">
                      <StatusBadge
                        label="発行"
                        value={states[e.state] ?? e.state}
                      />
                      <StatusBadge
                        label="評価方法"
                        value={e.dry_run ? "試行・発行なし" : "本番の結果"}
                      />
                      <NavigationLink
                        to={{
                          workspace,
                          screen: e.source.target.id.startsWith("run:")
                            ? "work"
                            : "hopper",
                          target: {
                            ...e.source.target,
                            kind: e.source.target.id.startsWith("run:")
                              ? "run"
                              : "hopper-source",
                          },
                          view: e.source.target.id.startsWith("run:")
                            ? "evidence"
                            : "evaluations",
                        }}
                        related
                      >
                        元の固定結果を確認
                      </NavigationLink>
                      <NavigationLink
                        to={{
                          workspace,
                          screen: "hopper",
                          view: "rules",
                          target: {
                            ...e.definition,
                            kind: "hopper-definition",
                          },
                        }}
                        related
                      >
                        使用したルールの固定版
                      </NavigationLink>
                      {e.destination && (
                        <NavigationLink
                          to={{
                            workspace,
                            screen: e.destination.id.startsWith("run:")
                              ? "work"
                              : e.destination.id.startsWith("scope:")
                                ? "priority"
                                : "publications",
                            target: {
                              ...e.destination,
                              kind: e.destination.id.startsWith("run:")
                                ? "run"
                                : e.destination.id.startsWith("scope:")
                                  ? "scope"
                                  : "publication",
                            },
                            view: e.destination.id.startsWith("run:")
                              ? "overview"
                              : undefined,
                          }}
                          related
                        >
                          発行先を確認
                        </NavigationLink>
                      )}
                      {!e.destination && <p>発行先は未確定です。</p>}
                    </div>
                    <button
                      disabled={busy}
                      onClick={() => {
                        navigation?.navigate({
                          workspace,
                          screen: "hopper",
                          view: "chains",
                          query: e.root,
                          target: {
                            ...hopperRef(selected),
                            kind: "hopper-evaluation",
                          },
                        });
                      }}
                    >
                      この起点の連鎖を絞り込む
                    </button>
                    {admin && !e.dry_run && (
                      <>
                        {e.state === "published" &&
                          e.destination?.id.startsWith("run:") && (
                            <button
                              disabled={busy}
                              onClick={() =>
                                void run(() => decide(selected, "resume"))
                              }
                            >
                              割当の保留を再開
                            </button>
                          )}
                        {[
                          "proposal",
                          "held",
                          "blocked",
                          "input_waiting",
                        ].includes(e.state) &&
                          (!e.semantic || e.semantic.state === "complete") && (
                            <button
                              disabled={busy}
                              onClick={() =>
                                void run(() => decide(selected, "publish"))
                              }
                            >
                              この提案・保留を発行する
                            </button>
                          )}
                        <button
                          disabled={busy}
                          onClick={() => void run(() => chain(true))}
                        >
                          この連鎖を停止
                        </button>
                        {boundsField}
                        <button
                          disabled={busy || !!boundsError}
                          onClick={() => void run(() => chain(false))}
                        >
                          この連鎖を明示的に再開
                        </button>
                      </>
                    )}
                  </>
                );
              })()}
            {selected.kind === "source" && <p>版：{selected.version}</p>}
            {selected.kind === "chain" && (
              <>
                <StatusBadge
                  label="連鎖"
                  value={
                    (selected.value as { stopped: boolean }).stopped
                      ? "停止"
                      : "再開を許可"
                  }
                />
                <p>起点：{(selected.value as { root: string }).root}</p>
                <NavigationLink
                  to={{
                    workspace,
                    screen: "hopper",
                    view: "chains",
                    query: (selected.value as { root: string }).root,
                  }}
                  related
                >
                  この起点の評価・発行を確認
                </NavigationLink>
                {admin && (
                  <fieldset disabled={busy}>
                    <button onClick={() => void run(() => chain(true))}>
                      この連鎖を停止
                    </button>
                    {boundsField}
                    <button
                      disabled={!!boundsError}
                      onClick={() => void run(() => chain(false))}
                    >
                      この連鎖を明示的に再開
                    </button>
                  </fieldset>
                )}
              </>
            )}
            <Disclosure title="固定版・条件・根拠の詳細">
              {() => <pre>{JSON.stringify(selected, null, 2)}</pre>}
            </Disclosure>
            {detail && (
              <Disclosure title="選択した固定版の内容" defaultOpen>
                {() => <pre>{JSON.stringify(detail, null, 2)}</pre>}
              </Disclosure>
            )}
          </section>
        </DetailPane>
      )}
    </ScreenContent>
  );
}
