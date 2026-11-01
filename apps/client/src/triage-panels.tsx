import { useState, type ReactNode } from "react";
import {
  ServerModelFields,
  type ModelConnection,
  type SemanticSettings,
} from "./server-model";
import { StatusBadge } from "./status-badge";
import { Field, jsonError } from "./field";

type Ref = { id: string; version: string };
export const at = (iso: string) => new Date(iso).toLocaleString();
export const valuationOptions = [
  ["reported", "報告額"],
  ["estimated", "単価からの推定額"],
  ["api_equivalent", "API料金換算額"],
  ["allocated", "配賦額"],
] as const;
const issueStates: Record<string, string> = { open: "未完了", closed: "完了" };
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
        評価：{states[attempt.state] ?? attempt.state} /{" "}
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
        <button
          key={ref.id}
          data-variant="secondary"
          onClick={() => onUsage(ref)}
        >
          モデル・利用量を確認：{ref.id}
        </button>
      ))}
      {attempt.costs.map((ref) => (
        <button
          key={ref.id}
          data-variant="secondary"
          onClick={() => onUsage(ref)}
        >
          料金版・費用を確認：{ref.id}
        </button>
      ))}
      {!attempt.costs.length && <p>この記録には費用の確定参照がありません。</p>}
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
  const [connections, setConnections] = useState<ModelConnection[] | null>(
      null,
    ),
    [advanced, setAdvanced] = useState("");
  return (
    <fieldset>
      <legend>Triageのサーバーモデル</legend>
      <button
        data-variant="secondary"
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
          disabled={!connections?.length}
          onClick={() =>
            void run(async () => {
              const c = connections![0],
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
            connections={connections ?? []}
            onChange={(patch) => onChange({ ...value, ...patch })}
          />
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
              <Field
                label="評価入力・料金の詳細（JSON）"
                value={advanced}
                rows={10}
                onChange={setAdvanced}
                error={jsonError(advanced)}
              />
              <button
                disabled={!!jsonError(advanced)}
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
        </>
      )}
      {connections && !connections.length && (
        <p>利用できるサーバー接続がありません。</p>
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
        data-variant="secondary"
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
        Issue情報を取得
      </button>
      {loaded && (
        <>
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
              {Object.entries(issueStates).map(([v, label]) => (
                <option key={v} value={v}>
                  {label}
                </option>
              ))}
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
          <Field
            label="依存するIssue・Runの固定参照（JSON）"
            value={deps}
            rows={5}
            onChange={(next) => canEdit && setDeps(next)}
            error={jsonError(deps)}
          />
          {admin && (
            <details>
              <summary>既存の外部Issueとの接続</summary>
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
                {value.observed.title} ·{" "}
                {issueStates[value.observed.state] ?? value.observed.state} ·{" "}
                {at(value.observed.at)}
              </p>
              <pre>{value.observed.body}</pre>
            </details>
          )}
          {canEdit && (
            <button
              disabled={!value.title || !!jsonError(deps)}
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
          {version !== "0" && <p>保存版：{version}</p>}
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
  states,
  reasons,
  call,
  write,
  run,
  render,
}: {
  policy: Ref;
  scopes: string[];
  states: Record<string, string>;
  reasons: Record<string, string>;
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
      <button
        data-variant="secondary"
        disabled={!id}
        onClick={() => void run(() => load(id))}
      >
        保存した一括評価を取得
      </button>
      {!batch && (
        <>
          <label>
            対象Scope ID
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
            費用上限
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
              {valuationOptions.map(([v, label]) => (
                <option key={v} value={v}>
                  {label}
                </option>
              ))}
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
            基準時刻：{at(batch.reference_time)} / 要求済み：{batch.requested} /
            上限：{batch.count}
          </p>
          <p>
            <StatusBadge
              label="状態"
              value={states[batch.state] ?? batch.state}
            />{" "}
            {reasons[batch.reason] ??
              reasons[`batch_${batch.reason}`] ??
              batch.reason}
          </p>
          <p>継続位置：{batch.after || "先頭"}</p>
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
