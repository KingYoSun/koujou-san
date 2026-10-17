import { useRef, useState } from "react";
import { syncRequest } from "./auth";
import { SyncHTTPError } from "./sync-error";

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
  source: { target: Ref; facts: unknown };
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
  not_matched: "条件不成立",
  matched: "条件成立・発行なし",
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
  const [saved, setSaved] = useState<Ref | null>(null);
  const [selected, setSelected] = useState<Version | null>(null);
  const [pastKind, setPastKind] = useState("github");
  const [pastTarget, setPastTarget] = useState("*");
  const [pastAfter, setPastAfter] = useState(0);
  const [trial, setTrial] = useState<Version | null>(null);
  const [bounds, setBounds] = useState("");
  const pending = useRef<{ key: string; id: string } | null>(null);
  const admin = role === "admin";
  const call = <T,>(path: string, body?: unknown) =>
    syncRequest(
      `${base}${path}`,
      native,
      body === undefined ? { method: "GET" } : { method: "POST", body },
    ) as Promise<T>;
  const run = async (f: () => Promise<void>) => {
    setBusy(true);
    setNotice("");
    try {
      await f();
    } catch (e) {
      setPage(null);
      setSelected(null);
      setTrial(null);
      if (
        e instanceof SyncHTTPError &&
        (e.status === 401 || e.status === 403)
      ) {
        setDraft("");
        setSaved(null);
        setID("");
        setVersion("0");
        pending.current = null;
        setNotice("現在の閲覧・編集・管理権限を確認してください。");
      } else
        setNotice(
          "入力・接続・現在の版を確認してください。同じ内容は同じ操作として再送します。",
        );
    } finally {
      setBusy(false);
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
      enabled
        ? "保存した版を有効化しました。過去の結果は発行しません。"
        : "新しい評価と未確定の発行を停止しました。発行済みの仕事は保持されます。",
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
        : "連鎖を再開可能にしました。保留した発行は履歴から明示して再開してください。",
    );
  };
  return (
    <details className="workspace-management">
      <summary>Hopper: 結果から次の仕事を発行</summary>
      <p>
        共有された結果をサーバーが評価します。画面を閉じても動作し、発行・Run作成・実行開始はそれぞれ現在の権限で確認します。
      </p>
      <p>
        処理枠・自動化主体・費用上限は「Triage方針・優先度・処理状態」のHopper管理設定で指定します。
      </p>
      <p role="status">{notice}</p>
      <fieldset disabled={busy}>
        <legend>定義と履歴</legend>
        <label>
          表示対象
          <select
            value={kind}
            onChange={(e) => {
              setKind(e.target.value);
              setPage(null);
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
            onChange={(e) => {
              setMode(e.target.value);
              setPage(null);
            }}
          >
            <option value="heads">現在</option>
            <option value="history">履歴・過去の試行を含む</option>
          </select>
        </label>
        <label>
          起点で絞る
          <input value={root} onChange={(e) => setRoot(e.target.value)} />
        </label>
        <button onClick={() => void run(() => query())}>
          Hopperの一覧を取得
        </button>
        <ul>
          {page?.items.map((r) => (
            <li key={`${r.id}/${r.version}`}>
              <button
                onClick={() => {
                  setSelected(r);
                  if (r.kind === "definition") {
                    setID(r.id);
                    setVersion(r.version);
                    setDraft(JSON.stringify(r.value, null, 2));
                    setSaved(hopperRef(r));
                  }
                }}
              >
                {r.kind === "definition"
                  ? (r.value as { name: string }).name
                  : (states[(r.value as Evaluation).state] ?? r.kind)}{" "}
                · {r.id}
              </button>
            </li>
          ))}
        </ul>
        {page?.items.length === 50 && (
          <button
            onClick={() => void run(() => query(page.after, page.after_id))}
          >
            次の50件
          </button>
        )}
      </fieldset>
      {selected && (
        <section aria-label="Hopperの詳細">
          {selected.kind === "evaluation" &&
            (() => {
              const e = selected.value as Evaluation;
              return (
                <>
                  <h3>
                    {states[e.state] ?? e.state}
                    {e.dry_run ? "（過去の試行・発行なし）" : ""}
                  </h3>
                  <p>{reasons[e.reason] ?? e.reason}</p>
                  {e.cost && (
                    <p>
                      記録済み費用（{e.cost.valuation}）:{" "}
                      {e.cost.amount === null
                        ? "未確定"
                        : `${e.cost.amount} ${e.cost.currency}`}
                      。欠測・遅着を含む実請求額の上限は保証しません。
                    </p>
                  )}
                  <p>
                    条件の判定:{" "}
                    {e.verdict === "true"
                      ? "成立"
                      : e.verdict === "false"
                        ? "不成立"
                        : "判断不能"}{" "}
                    / 深度: {e.depth}
                  </p>
                  <p>
                    元の結果: {e.source.target.id} / 発行先:{" "}
                    {e.destination?.id ?? "未発行"}
                  </p>
                  <button
                    disabled={busy}
                    onClick={() => {
                      setRoot(e.root);
                      setKind("evaluation");
                      setPage(null);
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
                      ].includes(e.state) && (
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
                      <label>
                        再開時の連鎖上限（変更する場合だけJSONで指定）
                        <textarea
                          aria-label="再開時の連鎖上限"
                          value={bounds}
                          onChange={(e) => setBounds(e.target.value)}
                        />
                      </label>
                      <button
                        disabled={busy}
                        onClick={() => void run(() => chain(false))}
                      >
                        この連鎖を明示的に再開
                      </button>
                    </>
                  )}
                </>
              );
            })()}
          <details>
            <summary>固定版・条件・根拠の詳細</summary>
            <pre>{JSON.stringify(selected, null, 2)}</pre>
          </details>
        </section>
      )}
      {role !== "viewer" && (
        <fieldset disabled={busy}>
          <legend>Hopper定義を編集</legend>
          <button
            onClick={() =>
              void run(async () => {
                setDraft(JSON.stringify(await call("/template"), null, 2));
                setID(`hopper:${crypto.randomUUID()}`);
                setVersion("0");
                setSaved(null);
              })
            }
          >
            初期設定で新しいHopperを作る
          </button>
          <p>
            初期設定は自動発行・通常の割当と開始です。対象・型付き条件・登録済みテンプレートの固定版を指定してください。保存と有効化は別の操作です。
          </p>
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
          <label>
            条件・発行先・連鎖上限・重複の設定（JSON）
            <textarea
              aria-label="条件・発行先・連鎖上限・重複の設定（JSON）"
              rows={16}
              value={draft}
              onChange={(e) => setDraft(e.target.value)}
            />
          </label>
          <button
            disabled={!id || !draft}
            onClick={() =>
              void run(async () => {
                const pin = await mutate(
                  `/records/${encodeURIComponent(id)}`,
                  version,
                  { value: JSON.parse(draft) },
                );
                setSaved(pin);
                setVersion(pin.version);
                setNotice(
                  "新しい版を保存しました。有効な版はまだ切り替わっていません。",
                );
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
      {admin && (
        <fieldset disabled={busy || !saved}>
          <legend>過去の結果を発行せずに試す</legend>
          <p>
            保存した固定版で50件ずつ評価します。Issue・Runの作成や開始は行いません。
          </p>
          <label>
            結果の種類
            <select
              value={pastKind}
              onChange={(e) => {
                setPastKind(e.target.value);
                setPastAfter(0);
              }}
            >
              <option value="github">外部CI</option>
              <option value="run">Run</option>
            </select>
          </label>
          <label>
            対象（全ては *）
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
              <pre>{JSON.stringify(trial.value, null, 2)}</pre>
              <button
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
    </details>
  );
}
