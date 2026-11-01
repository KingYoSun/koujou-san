import { useState, type ReactNode } from "react";
import {
  ServerModelFields,
  type ModelConnection,
  type SemanticSettings,
} from "./server-model";

type Ref = { id: string; version: string };
type Version = Ref & { value: unknown };
type Call = (path: string, body?: unknown) => Promise<unknown>;
type Run = (fn: () => Promise<void>) => Promise<void>;
type Write = (
  id: string,
  version: string,
  value: unknown,
) => Promise<{ data: Ref }>;
export type TriageModel = SemanticSettings & Record<string, unknown>;

export function TriageAudit({
  value,
  states,
  reasons,
  onUsage,
}: {
  value: unknown;
  states: Record<string, string>;
  reasons: Record<string, string>;
  onUsage: (ref: Ref) => void;
}) {
  const attempt = value as {
    state: string;
    reason: string;
    decision?: { unknown: string[]; evidence: { ref: Ref; quote: string }[] };
    usage: Ref[];
    costs: Ref[];
  };
  return (
    <section aria-label="評価の根拠と消費">
      <p>
        評価: {states[attempt.state] ?? attempt.state} /{" "}
        {reasons[attempt.reason] ?? attempt.reason}
      </p>
      {!!attempt.decision?.unknown.length && (
        <>
          <p>未確認の情報</p>
          <ul>
            {attempt.decision?.unknown.map((text, i) => (
              <li key={i}>{text}</li>
            ))}
          </ul>
        </>
      )}
      {attempt.decision?.evidence.map((e, i) => (
        <blockquote key={i}>
          {e.quote}
          <br />
          {e.ref.id} · {e.ref.version}
        </blockquote>
      ))}
      {attempt.usage.map((ref) => (
        <button key={ref.id} onClick={() => onUsage(ref)}>
          この要求のモデル・利用量を確認
        </button>
      ))}
      {attempt.costs.map((ref) => (
        <button key={ref.id} onClick={() => onUsage(ref)}>
          この要求の料金版・費用を確認
        </button>
      ))}
      {!attempt.costs.length && (
        <p>
          この記録には費用の確定参照がありません。未登録や欠測を0円として扱いません。
        </p>
      )}
    </section>
  );
}

export function TriageModelSettings({
  value,
  onChange,
  call,
  run,
}: {
  value?: TriageModel;
  onChange: (value: TriageModel) => void;
  call: Call;
  run: Run;
}) {
  const [connections, setConnections] = useState<ModelConnection[]>([]),
    [advanced, setAdvanced] = useState("");
  return (
    <fieldset>
      <legend>Triageのサーバーモデル</legend>
      <button
        onClick={() =>
          void run(async () =>
            setConnections(
              (await call("/model-connections")) as ModelConnection[],
            ),
          )
        }
      >
        モデル接続を取得
      </button>
      {!value && (
        <button
          disabled={!connections.length}
          onClick={() =>
            void run(async () => {
              const c = connections[0],
                m = c.models[0];
              onChange({
                ...((await call("/model-template")) as TriageModel),
                connection: c.id,
                revision: c.revision,
                model: m.requested,
                allowed_models: m.allowed_actual,
              });
            })
          }
        >
          この方針へモデル評価を設定
        </button>
      )}
      {value && (
        <>
          <ServerModelFields
            settings={value}
            connections={connections}
            onChange={(patch) => onChange({ ...value, ...patch })}
          />
          <p>
            Contextは選択した参照と除外から解決します。noneではContextを送りません。工数・費用・影響人数の欠測は不明のまま残します。参照の変更時は関係するIssueだけを再評価します。
          </p>
          <button
            onClick={() =>
              setAdvanced(
                JSON.stringify(
                  Object.fromEntries(
                    [
                      "context",
                      "evidence",
                      "fallbacks",
                      "prices",
                      "contract",
                      "valuation",
                      "cost",
                    ]
                      .filter((k) => value[k] !== undefined)
                      .map((k) => [k, value[k]]),
                  ),
                  null,
                  2,
                ),
              )
            }
          >
            Context・料金・fallbackの詳細設定を開く
          </button>
          {advanced && (
            <>
              <label>
                評価入力・料金の詳細（JSON）
                <textarea
                  rows={10}
                  value={advanced}
                  onChange={(e) => setAdvanced(e.target.value)}
                />
              </label>
              <button
                onClick={() =>
                  void run(async () =>
                    onChange({ ...value, ...JSON.parse(advanced) }),
                  )
                }
              >
                詳細設定を反映
              </button>
            </>
          )}
          <p>
            Hopperのモデル・入力・費用上限・処理枠とは別設定です。接続キーはサーバー側で管理します。
          </p>
        </>
      )}
      {!connections.length && (
        <p>
          利用可能な接続がない場合は、運用者がこのWorkspace用のサーバー接続を登録してください。
        </p>
      )}
    </fieldset>
  );
}

type IssueInfo = {
  scope: Ref;
  origin: string;
  title: string;
  body: string;
  state: string;
  deadline?: string;
  dependencies: Ref[];
  external?: {
    connection: string;
    number: number;
    labels?: Record<string, string>;
  };
  observed?: { title: string; body: string; state: string; at: string };
  sources?: Ref[];
};
export function TriageIssue({
  scope,
  canEdit,
  admin,
  call,
  write,
  run,
  render,
}: {
  scope: Ref;
  canEdit: boolean;
  admin: boolean;
  call: Call;
  write: Write;
  run: Run;
  render?: (content: ReactNode) => ReactNode;
}) {
  const [value, setValue] = useState<IssueInfo>({
      scope,
      origin: "human",
      title: "",
      body: "",
      state: "open",
      dependencies: [],
    }),
    [version, setVersion] = useState("0"),
    [deps, setDeps] = useState("[]"),
    [loaded, setLoaded] = useState(false);
  const id = `issue:${scope.id.replace(/^scope:/, "")}`;
  const content = (
    <fieldset>
      <legend>Issueの本文・期限・依存</legend>
      <button
        onClick={() =>
          void run(async () => {
            const page = (await call("/query", {
              mode: "heads",
              kind: "issue",
              id,
              limit: 1,
            })) as { data: { items: Version[] } };
            const rec = page.data.items[0];
            if (rec) {
              const fixed = (await call(
                `/records/${encodeURIComponent(rec.id)}?version=${encodeURIComponent(rec.version)}`,
              )) as Version;
              const v = fixed.value as IssueInfo;
              setValue(v);
              setVersion(rec.version);
              setDeps(JSON.stringify(v.dependencies, null, 2));
            }
            setLoaded(true);
          })
        }
      >
        Issue情報を取得・編集
      </button>
      {loaded && (
        <>
          <p>
            ScopeのGroupingと本文を分けて保存します。期限の7日前・24時間前・到来時に再評価します。
          </p>
          <label>
            Issue名
            <input
              value={value.title}
              readOnly={!canEdit}
              onChange={(e) => setValue({ ...value, title: e.target.value })}
            />
          </label>
          <label>
            Issue本文
            <textarea
              rows={5}
              value={value.body}
              readOnly={!canEdit}
              onChange={(e) => setValue({ ...value, body: e.target.value })}
            />
          </label>
          <label>
            状態
            <select
              value={value.state}
              disabled={!canEdit}
              onChange={(e) => setValue({ ...value, state: e.target.value })}
            >
              <option value="open">未完了</option>
              <option value="closed">完了</option>
            </select>
          </label>
          <label>
            期限
            <input
              type="datetime-local"
              readOnly={!canEdit}
              value={
                value.deadline
                  ? new Date(
                      new Date(value.deadline).getTime() -
                        new Date(value.deadline).getTimezoneOffset() * 60000,
                    )
                      .toISOString()
                      .slice(0, 16)
                  : ""
              }
              onChange={(e) =>
                setValue({
                  ...value,
                  deadline: e.target.value
                    ? new Date(e.target.value).toISOString()
                    : undefined,
                })
              }
            />
          </label>
          <label>
            依存するIssue・Runの固定参照（JSON）
            <textarea
              value={deps}
              readOnly={!canEdit}
              onChange={(e) => setDeps(e.target.value)}
            />
          </label>
          {admin && (
            <details>
              <summary>既存の外部Issueとの接続</summary>
              <p>
                優先度ラベルは方針の設定を使います。本文・状態の観測は別の入力として保持し、ラベル同期では書き換えません。外部への反映には現在の実行許可も必要です。
              </p>
              <label>
                外部接続ID
                <input
                  value={value.external?.connection ?? ""}
                  onChange={(e) =>
                    setValue({
                      ...value,
                      external: e.target.value
                        ? {
                            connection: e.target.value,
                            number: value.external?.number ?? 0,
                          }
                        : undefined,
                    })
                  }
                />
              </label>
              {value.external && (
                <label>
                  Issue番号
                  <input
                    type="number"
                    min={1}
                    value={value.external.number}
                    onChange={(e) =>
                      setValue({
                        ...value,
                        external: {
                          ...value.external!,
                          number: Number(e.target.value),
                        },
                      })
                    }
                  />
                </label>
              )}
            </details>
          )}
          {value.observed && (
            <details>
              <summary>外部で観測した本文・状態</summary>
              <p>
                {value.observed.title} · {value.observed.state} ·{" "}
                {value.observed.at}
              </p>
              <pre>{value.observed.body}</pre>
            </details>
          )}
          {canEdit && (
            <button
              disabled={!value.title}
              onClick={() =>
                void run(async () => {
                  const saved = await write(id, version, {
                    ...value,
                    scope,
                    dependencies: JSON.parse(deps),
                  });
                  setVersion(saved.data.version);
                })
              }
            >
              Issue情報を保存して再評価を要求
            </button>
          )}
          <p>保存版: {version}</p>
        </>
      )}
    </fieldset>
  );
  return render ? render(content) : content;
}

type Batch = {
  reference_time: string;
  after: string;
  requested: number;
  scanned: number;
  state: string;
  reason: string;
  count: number;
};
export function TriageBatch({
  policy,
  scopes,
  call,
  write,
  run,
  render,
}: {
  policy: Ref;
  scopes: string[];
  call: Call;
  write: Write;
  run: Run;
  render?: (content: ReactNode) => ReactNode;
}) {
  const [id, setID] = useState(""),
    [version, setVersion] = useState("0"),
    [scopeIDs, setScopes] = useState(scopes.join(",")),
    [from, setFrom] = useState("2026-01-01"),
    [until, setUntil] = useState(new Date().toISOString().slice(0, 10)),
    [count, setCount] = useState(100),
    [page, setPage] = useState(50),
    [amount, setAmount] = useState(""),
    [currency, setCurrency] = useState("USD"),
    [valuation, setValuation] = useState("estimated"),
    [batch, setBatch] = useState<Batch | null>(null);
  const load = async (name: string) => {
    const rec = (await call(`/records/${encodeURIComponent(name)}`)) as Version;
    setID(rec.id);
    setVersion(rec.version);
    setBatch(rec.value as Batch);
  };
  const advance = async (value: unknown) => {
    const name = id || `batch:${crypto.randomUUID()}`;
    const saved = await write(name, version, value);
    await load(saved.data.id);
  };
  const content = (
    <fieldset>
      <legend>既存Issueの一括評価</legend>
      <p>
        期間・Scope・総件数・費用を固定し、1ページずつ要求します。基準時刻と継続位置を保存し、通常の全件定期評価は行いません。
      </p>
      <label>
        一括評価ID
        <input
          value={id}
          onChange={(e) => {
            setID(e.target.value);
            setVersion("0");
            setBatch(null);
          }}
        />
      </label>
      <button disabled={!id} onClick={() => void run(() => load(id))}>
        保存した一括評価を取得
      </button>
      {!batch && (
        <>
          <label>
            対象Scope ID（カンマ区切り）
            <input
              value={scopeIDs}
              onChange={(e) => setScopes(e.target.value)}
            />
          </label>
          <label>
            受付期間の開始
            <input
              type="date"
              value={from}
              onChange={(e) => setFrom(e.target.value)}
            />
          </label>
          <label>
            受付期間の終了
            <input
              type="date"
              value={until}
              onChange={(e) => setUntil(e.target.value)}
            />
          </label>
          <label>
            要求する総件数の上限
            <input
              type="number"
              min={1}
              value={count}
              onChange={(e) => setCount(Number(e.target.value))}
            />
          </label>
          <label>
            1ページの件数
            <input
              type="number"
              min={1}
              max={100}
              value={page}
              onChange={(e) => setPage(Number(e.target.value))}
            />
          </label>
          <label>
            費用上限（十進数）
            <input value={amount} onChange={(e) => setAmount(e.target.value)} />
          </label>
          <label>
            通貨
            <input
              value={currency}
              onChange={(e) => setCurrency(e.target.value)}
            />
          </label>
          <label>
            費用の種類
            <select
              value={valuation}
              onChange={(e) => setValuation(e.target.value)}
            >
              <option value="estimated">推計</option>
              <option value="reported">実報告</option>
              <option value="api_equivalent">API換算</option>
              <option value="allocated">按分</option>
            </select>
          </label>
          <button
            disabled={
              !scopeIDs || !amount || !currency || !from || !until || !count
            }
            onClick={() =>
              void run(() =>
                advance({
                  policy,
                  scopes: scopeIDs
                    .split(",")
                    .map((s) => s.trim())
                    .filter(Boolean)
                    .map((s) => (s.startsWith("scope:") ? s : `scope:${s}`)),
                  from: new Date(`${from}T00:00:00`).toISOString(),
                  until: new Date(`${until}T23:59:59.999`).toISOString(),
                  count,
                  page_size: page,
                  cost: { amount, currency, valuation },
                }),
              )
            }
          >
            範囲を固定して最初のページを要求
          </button>
        </>
      )}
      {batch && (
        <>
          <p>
            基準時刻: {batch.reference_time} / 要求済み: {batch.requested} /
            上限: {batch.count} / 状態: {batch.state} {batch.reason}
          </p>
          <p>
            継続位置: {batch.after || "先頭"}
            。要求の受付とモデル評価の完了は別です。利用量や単価の欠測時は次の要求を保留します。
          </p>
          <button
            disabled={batch.state === "complete"}
            onClick={() => void run(() => advance({ continue: true }))}
          >
            保存した条件で次のページを要求
          </button>
          <button onClick={() => void run(() => advance({ stop: true }))}>
            この一括評価を停止
          </button>
        </>
      )}
    </fieldset>
  );
  return render ? render(content) : content;
}
