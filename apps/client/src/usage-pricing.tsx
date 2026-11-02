import {
  ScreenContent,
  NavigationLink,
  useNavigation,
  useScreenActive,
} from "./navigation";
import { DetailPane } from "./panes";
import { at } from "./github-observations";
import { UsageRecordDetails } from "./usage-record-details";
import { allowAnswerScope, answerScopeEpoch } from "./sync-store";
import { UsageComparison } from "./usage-comparison";
import { amountLabel, costLabel } from "./usage-comparison-model";
import { useEffect, useRef, useState } from "react";
import { syncRequest } from "./auth";
import { SyncHTTPError } from "./sync-error";

type Ref = { id: string; version: string };
type RecordView = Ref & { kind: string; value: unknown };
type Page = {
  data: {
    items: unknown[];
    as_of: string;
    through: number;
    timezone: string;
    kind: string;
  };
  meta: { next_cursor: string | null };
};
type Quantity = {
  value: string | null;
  unit: string;
  state?: string;
  missing?: number;
  not_applicable?: number;
};
type Item = Record<string, unknown>;
type Cells = {
  key: string;
  record?: Ref;
  attribution: string;
  model: string;
  count?: number;
  quantities: Record<string, Quantity>;
  amount?: string | null;
  state?: string;
  currency: string;
  at: string;
};
const text = (v: unknown, fallback = "非該当") => (v ? String(v) : fallback);
const attributed = (kind: unknown, id: unknown, workflow: unknown) =>
  [costLabel(kind as string), id, workflow && `工程 ${workflow}`]
    .filter(Boolean)
    .join(" / ");
// One row shape for aggregate buckets, export entries, fixed records and
// late-arrival ranges; values a row does not carry read 非該当.
function cells(mode: string, kind: string, item: unknown): Cells {
  const r = item as Item,
    v = (r.value ?? {}) as Item,
    a = v.attribution as Item | undefined;
  if (mode === "aggregate")
    return {
      key: r.key as string,
      attribution: attributed(r.attribution_kind, r.attribution_id, r.workflow),
      model: text(r.model, "未取得"),
      count: r.records as number,
      quantities: Object.fromEntries(
        (r.quantities as (Quantity & { category: string })[]).map((q) => [
          q.category,
          q,
        ]),
      ),
      amount: kind === "usage" ? undefined : (r.amount as string | null),
      state:
        r.amount != null && r.incomplete
          ? `未算定 ${r.incomplete}件を除く`
          : undefined,
      currency: text(r.currency, ""),
      at: r.day as string,
    };
  if (mode === "changes")
    return {
      key: String(r.position),
      record: r.source as Ref,
      attribution: `計測範囲 ${r.group}`,
      model: "非該当",
      quantities: {},
      currency: "",
      at: `${at(r.affected_from as string)} ～ ${at(r.affected_until as string)}`,
    };
  const recordKind = mode === "records" ? (r.kind as string) : kind,
    price = recordKind === "price",
    evaluation = recordKind !== "usage" && !price,
    when = v.at ?? v.effective_from ?? r.at;
  return {
    key: `${r.key ?? r.id}@${r.version ?? ""}`,
    record:
      mode === "records"
        ? { id: r.id as string, version: r.version as string }
        : undefined,
    attribution: a
      ? attributed(a.kind, a.id, a.workflow)
      : price
        ? text(v.contract && `契約 ${v.contract}`)
        : `計測範囲 ${r.group ?? v.coverage_group}`,
    model:
      a || price
        ? [v.connection, price ? v.model : (v.actual_model ?? "未取得")]
            .filter(Boolean)
            .join(" / ")
        : "非該当",
    quantities: (r.quantities ?? v.quantities ?? {}) as Record<
      string,
      Quantity
    >,
    amount: evaluation ? ((r.amount ?? v.amount) as string | null) : undefined,
    state: evaluation ? costLabel((r.state ?? v.state) as string) : undefined,
    currency: evaluation ? text(r.currency ?? v.currency, "") : "",
    at: when ? at(when as string) : "非該当",
  };
}
const categoryOrder = [
  "input",
  "output",
  "cache_read",
  "cache_write",
  "reasoning",
];
const quantity = (q?: Quantity) =>
  !q || q.state === "not_applicable"
    ? "非該当"
    : q.value == null
      ? q.not_applicable && !q.missing
        ? "非該当"
        : "未取得"
      : q.value +
        (q.state === "estimated"
          ? "（推定）"
          : q.missing
            ? `（未取得 ${q.missing}件を除く）`
            : "");
const money = (c: Cells, currency: boolean) =>
  c.amount === undefined
    ? "非該当"
    : c.amount == null
      ? (c.state ?? "未算定")
      : amountLabel(c.amount, currency ? c.currency : undefined) +
        (c.state ? `（${c.state}）` : "");
export function UsageTable({
  mode,
  page,
  selected,
  busy,
  onRead,
}: {
  mode: string;
  page: Page;
  selected?: Ref | null;
  busy: boolean;
  onRead: (r: Ref) => void;
}) {
  const rows = page.data.items.map((item) => cells(mode, page.data.kind, item));
  const categories = [
    ...new Map(
      rows.flatMap((c) =>
        Object.entries(c.quantities).map(([k, q]) => [k, q.unit] as const),
      ),
    ),
  ].sort(
    ([a], [b]) =>
      (categoryOrder.indexOf(a) + 1 || 99) -
      (categoryOrder.indexOf(b) + 1 || 99),
  );
  const currencies = new Set(rows.map((c) => c.currency).filter(Boolean)),
    currency = currencies.size === 1 ? [...currencies][0] : "",
    withRecord = mode === "records" || mode === "changes";
  return (
    <div className="table-scroll">
      <table className="usage-table" aria-busy={busy}>
        <thead>
          <tr>
            {withRecord && <th>記録</th>}
            <th>帰属</th>
            <th>モデル</th>
            {mode === "aggregate" && <th data-numeric>件数</th>}
            {categories.map(([k, unit]) => (
              <th key={k} data-numeric>
                {unit === "token"
                  ? `${costLabel(k)}トークン`
                  : `${costLabel(k)}（${unit}）`}
              </th>
            ))}
            <th data-numeric>金額{currency && `（${currency}）`}</th>
            <th>
              {mode === "aggregate"
                ? `集計日（${page.data.timezone}）`
                : "取得時点"}
            </th>
          </tr>
        </thead>
        <tbody>
          {rows.map((c) => (
            <tr
              key={c.key}
              aria-current={
                c.record &&
                c.record.id === selected?.id &&
                c.record.version === selected.version
                  ? "true"
                  : undefined
              }
            >
              {withRecord && (
                <td>
                  {mode === "records" ? (
                    <button
                      disabled={busy}
                      data-variant="secondary"
                      onClick={() => onRead(c.record!)}
                    >
                      {c.record!.id} @ {c.record!.version}
                    </button>
                  ) : (
                    `${c.record!.id} @ ${c.record!.version}`
                  )}
                </td>
              )}
              <td>{c.attribution}</td>
              <td>{c.model}</td>
              {mode === "aggregate" && <td data-numeric>{c.count}</td>}
              {categories.map(([k]) => (
                <td key={k} data-numeric>
                  {quantity(c.quantities[k])}
                </td>
              ))}
              <td data-numeric>{money(c, !currency)}</td>
              <td>{c.at}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
type Rate = { amount: string; per: string; unit: string };
type Price = {
  connection: string;
  contract: string;
  model: string;
  currency: string;
  effective_from: string;
  effective_until: string | null;
  source: string;
  scale: number;
  rounding: string;
  rates: Record<string, Rate>;
};
const blank = (): Price => ({
  connection: "",
  contract: "",
  model: "",
  currency: "",
  effective_from: "",
  effective_until: null,
  source: "",
  scale: 6,
  rounding: "half_up",
  rates: {},
});

// This bounded administration surface precedes the comparison UI in Issue #32.
// Amounts, quantities and rates remain decimal strings, including form values.
export function UsagePricing({
  native,
  workspace,
  role,
  api,
  user,
  settings = false,
}: {
  native: boolean;
  workspace: string;
  role: string;
  api: string;
  user: string;
  settings?: boolean;
}) {
  const navigation = useNavigation();
  const navigationRef = useRef(navigation);
  navigationRef.current = navigation;
  const visible = useScreenActive();
  const messageRef = useRef<HTMLParagraphElement>(null);
  const readSerial = useRef(0);
  const [mode, setMode] = useState(settings ? "records" : "aggregate");
  const [kind, setKind] = useState(settings ? "price" : "usage");
  const [from, setFrom] = useState(new Date().toISOString().slice(0, 10));
  const [until, setUntil] = useState(
    new Date(Date.now() + 86400000).toISOString().slice(0, 10),
  );
  const [timezone, setTimezone] = useState(
    Intl.DateTimeFormat().resolvedOptions().timeZone,
  );
  const [filter, setFilter] = useState({
    scopes: "",
    workflow: "",
    model: "",
    attribution: "",
    attribution_id: "",
    currency: "",
    id: "",
  });
  const [page, setPage] = useState<Page | null>(null);
  const [detail, setDetail] = useState<RecordView | null>(null);
  const [price, setPrice] = useState(blank);
  const [priceID, setPriceID] = useState("");
  const [version, setVersion] = useState("0");
  const [category, setCategory] = useState("");
  const [message, setMessage] = useState("");
  useEffect(() => {
    if (!visible || !message) return;
    messageRef.current?.focus();
    messageRef.current?.scrollIntoView({ block: "nearest" });
  }, [message]);
  const [busy, setBusy] = useState(false);
  const pending = useRef<{ key: string; operation: string } | null>(null);
  const base = `/usage/workspaces/${encodeURIComponent(workspace)}`;
  const fail = (e: unknown, failed: string) => {
    if (e instanceof SyncHTTPError && (e.status === 401 || e.status === 403)) {
      setPage(null);
      setDetail(null);
      setPrice(blank());
      pending.current = null;
      setPriceID("");
      setVersion("0");
      setMessage("閲覧権限がありません。");
    } else setMessage(`${failed}できませんでした。`);
  };
  const query = async (cursor = "") => {
    setBusy(true);
    setMessage("");
    try {
      const response = await syncRequest(`${base}/query`, native, {
        method: "POST",
        body: {
          mode,
          kind,
          from: `${from}T00:00:00Z`,
          until: `${until}T00:00:00Z`,
          timezone,
          ...filter,
          scopes: filter.scopes
            .split(",")
            .map((s) => s.trim())
            .filter(Boolean),
          cursor,
          limit: 50,
        },
      });
      setPage(response as Page);
      setDetail(null);
    } catch (e) {
      fail(e, `${costLabel(kind)}を取得`);
    } finally {
      setBusy(false);
    }
  };
  const read = async (r: { id: string; version: string }) => {
    const serial = ++readSerial.current;
    setBusy(true);
    setMessage("");
    try {
      const epoch = answerScopeEpoch({ api, user, workspace });
      const response = (await syncRequest(
        `${base}/records/${encodeURIComponent(r.id)}?version=${encodeURIComponent(r.version)}`,
        native,
        { method: "GET" },
      )) as RecordView;
      if (
        !allowAnswerScope({ api, user, workspace }, epoch) ||
        serial !== readSerial.current
      )
        return;
      setDetail(response);
      if (response.kind === "price") {
        setPrice(response.value as Price);
        setPriceID(r.id);
        setVersion(r.version);
      }
    } catch (e) {
      fail(e, "記録を取得");
    } finally {
      setBusy(false);
    }
  };
  const save = async () => {
    const key = JSON.stringify({ priceID, version, price });
    if (pending.current?.key !== key)
      pending.current = { key, operation: crypto.randomUUID() };
    setBusy(true);
    setMessage("");
    try {
      const response = (await syncRequest(
        `${base}/records/${encodeURIComponent(priceID.startsWith("price:") ? priceID : `price:${priceID}`)}`,
        native,
        {
          method: "POST",
          body: {
            operation_id: pending.current.operation,
            expected_version: version,
            value: price,
          },
        },
      )) as { data: { id: string; version: string } };
      setPriceID(response.data.id);
      setVersion(response.data.version);
      const nav = navigationRef.current;
      if (nav?.route.screen === "pricing")
        nav.navigate(
          {
            ...nav.route,
            target: {
              kind: "price",
              id: response.data.id,
              version: response.data.version,
            },
          },
          { replace: true },
        );
      setMessage(
        `料金版 ${response.data.id} @ ${response.data.version} を保存しました。`,
      );
    } catch (e) {
      fail(e, "料金版を保存");
    } finally {
      setBusy(false);
    }
  };
  useEffect(() => {
    const target = navigation?.route.target;
    if (
      !visible ||
      navigation?.route.screen !== "pricing" ||
      target?.kind !== "price"
    )
      return;
    if (priceID === target.id && version === target.version) return;
    void read(target);
  }, [
    visible,
    navigation?.route.target?.id,
    navigation?.route.target?.version,
  ]);
  return (
    <>
      {!settings && (
        <UsageComparison
          native={native}
          workspace={workspace}
          role={role}
          api={api}
          user={user}
          onInspect={read}
        />
      )}
      <ScreenContent>
        <section className="team-details" aria-label="利用量・料金">
          {!settings && <h3>利用量・料金</h3>}
          <ScreenContent>
            <div className="team-controls">
              {!settings && (
                <label>
                  表示
                  <select
                    value={mode}
                    onChange={(e) => {
                      setMode(e.target.value);
                      setPage(null);
                      setKind("usage");
                    }}
                  >
                    <option value="aggregate">期間集計</option>
                    <option value="export">export</option>
                    <option value="records">記録・版履歴</option>
                    <option value="changes">遅着・訂正の範囲</option>
                  </select>
                </label>
              )}
              {!settings && (
                <label>
                  種別
                  <select
                    value={kind}
                    onChange={(e) => {
                      setKind(e.target.value);
                      setPage(null);
                    }}
                  >
                    {(mode === "records"
                      ? ["usage", "price", "evaluation"]
                      : [
                          "usage",
                          "reported",
                          "estimated",
                          "api_equivalent",
                          "allocated",
                        ]
                    ).map((k) => (
                      <option key={k} value={k}>
                        {costLabel(k)}
                      </option>
                    ))}
                  </select>
                </label>
              )}
              {["aggregate", "export"].includes(mode) && (
                <label>
                  開始日（UTC）
                  <input
                    type="date"
                    value={from}
                    onChange={(e) => setFrom(e.target.value)}
                  />
                </label>
              )}
              {["aggregate", "export"].includes(mode) && (
                <label>
                  終了日（UTC・含まない）
                  <input
                    type="date"
                    value={until}
                    onChange={(e) => setUntil(e.target.value)}
                  />
                </label>
              )}
              {["aggregate", "export"].includes(mode) && (
                <label>
                  集計タイムゾーン
                  <input
                    value={timezone}
                    onChange={(e) => setTimezone(e.target.value)}
                  />
                </label>
              )}
              {Object.entries({
                scopes: "Scope ID",
                workflow: "Workflow ID",
                model: "実モデル",
                attribution: "帰属種別",
                attribution_id: "帰属先 ID",
                currency: "通貨",
                id: "記録 ID",
              })
                .filter(([k]) =>
                  mode === "records"
                    ? k === "id"
                    : mode === "changes"
                      ? false
                      : k !== "id",
                )
                .map(([k, label]) => {
                  const change = (e: { target: { value: string } }) => {
                    setFilter({ ...filter, [k]: e.target.value });
                    setPage(null);
                  };
                  return (
                    <label key={k}>
                      {label}
                      {k === "attribution" ? (
                        <select value={filter.attribution} onChange={change}>
                          <option value="">
                            Run・操作・評価・未帰属すべて
                          </option>
                          {[
                            "run",
                            "operation",
                            "hopper",
                            "triage",
                            "unattributed",
                          ].map((a) => (
                            <option key={a} value={a}>
                              {costLabel(a)}
                            </option>
                          ))}
                        </select>
                      ) : (
                        <input
                          value={filter[k as keyof typeof filter]}
                          onChange={change}
                        />
                      )}
                    </label>
                  );
                })}
              <button
                disabled={busy}
                aria-busy={busy}
                onClick={() => void query()}
              >
                {busy
                  ? `${costLabel(kind)}を取得中…`
                  : `${costLabel(kind)}を取得`}
              </button>
            </div>
            {page && (
              <>
                <p>
                  集計時点：{page.data.as_of} / 反映位置：{page.data.through} /{" "}
                  {page.data.timezone} / {page.data.items.length}件を表示
                  {page.meta.next_cursor && "（続きあり）"}
                </p>
                {page.data.items.length === 0 ? (
                  <p>該当する{costLabel(kind)}はありません。</p>
                ) : (
                  <UsageTable
                    mode={mode}
                    page={page}
                    selected={detail}
                    busy={busy}
                    onRead={(r) => void read(r)}
                  />
                )}
                {page.meta.next_cursor && (
                  <button
                    disabled={busy}
                    data-variant="secondary"
                    onClick={() => void query(page.meta.next_cursor!)}
                  >
                    {costLabel(kind)}の次のページ
                  </button>
                )}
                {mode === "export" && (
                  <a
                    download="koujou-usage-page.json"
                    href={`data:application/json;charset=utf-8,${encodeURIComponent(JSON.stringify(page, null, 2))}`}
                  >
                    表示ページを書き出す
                  </a>
                )}
              </>
            )}
            {detail && (
              <DetailPane>
                <section aria-label="選択した記録の詳細">
                  <UsageRecordDetails
                    record={detail}
                    onRead={(r) => void read(r)}
                  />
                  {!settings && detail.kind === "price" && (
                    <NavigationLink
                      to={{
                        workspace,
                        screen: "pricing",
                        target: {
                          kind: "price",
                          id: detail.id,
                          version: detail.version,
                        },
                      }}
                      related
                    >
                      この料金版の設定を開く
                    </NavigationLink>
                  )}
                </section>
              </DetailPane>
            )}
          </ScreenContent>
          <ScreenContent screens={["pricing"]}>
            {priceID && version !== "0" && (
              <p>
                編集中の料金版：{priceID} / {version}
              </p>
            )}
            {role === "viewer" && <p>料金設定の変更には編集権限が必要です。</p>}
            {role !== "viewer" && (
              <fieldset disabled={busy}>
                <legend>料金の手動設定</legend>
                <button
                  onClick={() => {
                    setPrice(blank());
                    setPriceID("");
                    setVersion("0");
                    navigation?.navigate({
                      workspace,
                      screen: "pricing",
                      view: "new",
                    });
                  }}
                >
                  新しい料金表
                </button>
                <label>
                  料金表 ID
                  <input
                    value={priceID}
                    readOnly={version !== "0"}
                    onChange={(e) => setPriceID(e.target.value)}
                  />
                </label>
                {(
                  [
                    ["connection", "接続先の識別名"],
                    ["contract", "契約の識別名"],
                    ["model", "課金対象の実モデル"],
                    ["currency", "通貨"],
                    ["effective_from", "適用開始（ISO 8601）"],
                    ["source", "単価の根拠"],
                  ] as const
                ).map(([k, label]) => (
                  <label key={k}>
                    {label}
                    <input
                      value={price[k]}
                      onChange={(e) =>
                        setPrice({ ...price, [k]: e.target.value })
                      }
                    />
                  </label>
                ))}
                <label>
                  適用終了（ISO 8601）
                  <input
                    value={price.effective_until ?? ""}
                    onChange={(e) =>
                      setPrice({
                        ...price,
                        effective_until: e.target.value || null,
                      })
                    }
                  />
                </label>
                <label>
                  金額の小数桁数
                  <input
                    type="number"
                    min="0"
                    max="30"
                    value={price.scale}
                    onChange={(e) =>
                      setPrice({ ...price, scale: Number(e.target.value) })
                    }
                  />
                </label>
                <label>
                  丸め規則
                  <select
                    value={price.rounding}
                    onChange={(e) =>
                      setPrice({ ...price, rounding: e.target.value })
                    }
                  >
                    <option value="half_up">四捨五入</option>
                    <option value="half_even">最近接・偶数丸め</option>
                    <option value="down">切り捨て</option>
                  </select>
                </label>
                <label>
                  課金区分
                  <input
                    value={category}
                    list="usage-pricing-categories"
                    onChange={(e) => setCategory(e.target.value)}
                  />
                </label>
                <datalist id="usage-pricing-categories">
                  {["input", "cache", "output", "image"].map((c) => (
                    <option key={c} value={c} />
                  ))}
                </datalist>
                <button
                  disabled={!category.trim()}
                  onClick={() => {
                    setPrice({
                      ...price,
                      rates: {
                        ...price.rates,
                        [category.trim()]: { amount: "", per: "", unit: "" },
                      },
                    });
                    setCategory("");
                  }}
                >
                  課金区分を追加
                </button>
                {Object.entries(price.rates).map(([k, r]) => (
                  <div className="team-controls" key={k}>
                    <strong>{k}</strong>
                    {(
                      [
                        ["amount", "単価"],
                        ["per", "単価あたりの数量"],
                        ["unit", "数量の単位"],
                      ] as const
                    ).map(([field, label]) => (
                      <label key={field}>
                        {label}
                        <input
                          value={r[field]}
                          onChange={(e) =>
                            setPrice({
                              ...price,
                              rates: {
                                ...price.rates,
                                [k]: { ...r, [field]: e.target.value },
                              },
                            })
                          }
                        />
                      </label>
                    ))}
                    <button
                      data-variant="secondary"
                      onClick={() => {
                        const rates = { ...price.rates };
                        delete rates[k];
                        setPrice({ ...price, rates });
                      }}
                    >
                      区分を削除
                    </button>
                  </div>
                ))}
                <button
                  disabled={!priceID || !Object.keys(price.rates).length}
                  onClick={() => void save()}
                >
                  料金版を保存
                </button>
              </fieldset>
            )}
          </ScreenContent>
        </section>
      </ScreenContent>
      <ScreenContent>
        <p role="status" ref={messageRef} tabIndex={-1}>
          {message}
        </p>
      </ScreenContent>
    </>
  );
}
