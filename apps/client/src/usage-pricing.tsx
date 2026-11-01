import {
  ScreenContent,
  NavigationLink,
  useNavigation,
  useScreenActive,
} from "./navigation";
import { UsageRecordDetails } from "./usage-record-details";
import { allowAnswerScope, answerScopeEpoch } from "./sync-store";
import { UsageComparison } from "./usage-comparison";
import { useEffect, useRef, useState } from "react";
import { syncRequest } from "./auth";
import { SyncHTTPError } from "./sync-error";

type RecordView = { id: string; version: string; kind: string; value: unknown };
type Page = {
  data: { items: unknown[]; as_of: string; through: number; timezone: string };
  meta: { next_cursor: string | null };
};
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
const labels: Record<string, string> = {
  usage: "利用量",
  price: "料金版",
  evaluation: "費用評価",
  reported: "報告額",
  estimated: "単価からの推定額",
  api_equivalent: "API料金換算額",
  allocated: "配賦額",
};
const show = (v: unknown) => JSON.stringify(v, null, 2);

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
  const dataDetails = useRef<HTMLDetailsElement>(null);
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
  useEffect(() => {
    if (!visible || settings || !detail) return;
    dataDetails.current?.focus();
    dataDetails.current?.scrollIntoView({ block: "start" });
  }, [visible, settings, detail?.id, detail?.version]);
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
  const fail = (e: unknown) => {
    setPage(null);
    setDetail(null);
    if (e instanceof SyncHTTPError && (e.status === 401 || e.status === 403)) {
      setPrice(blank());
      pending.current = null;
      setPriceID("");
      setVersion("0");
      setMessage("現在の閲覧・編集権限を確認してください。");
    } else
      setMessage(
        "操作を完了できませんでした。入力・接続・現在の版を確認してください。",
      );
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
      fail(e);
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
      fail(e);
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
        `料金版 ${response.data.id} @ ${response.data.version} を保存しました。過去の費用評価は変更されません。`,
      );
    } catch (e) {
      fail(e);
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
        {
          <details
            className="team-details"
            open={settings || !!detail}
            ref={dataDetails}
            tabIndex={-1}
          >
            <summary>
              {settings
                ? "料金の設定と版履歴"
                : "利用量の個別集計・原記録を調べる"}
            </summary>
            <h3>利用量・料金</h3>

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
                          {labels[k]}
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
                  scopes: "Scope ID（複数はカンマ区切り）",
                  workflow: "Workflow ID",
                  model: "実モデル",
                  attribution:
                    "帰属種別（run / operation / hopper / triage / unattributed）",
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
                  .map(([k, label]) => (
                    <label key={k}>
                      {label}
                      <input
                        value={filter[k as keyof typeof filter]}
                        onChange={(e) => {
                          setFilter({ ...filter, [k]: e.target.value });
                          setPage(null);
                        }}
                      />
                    </label>
                  ))}
                <button disabled={busy} onClick={() => void query()}>
                  取得
                </button>
              </div>
              {page && (
                <>
                  <p>
                    集計時点: {page.data.as_of} / 反映位置: {page.data.through}{" "}
                    / {page.data.timezone}
                  </p>
                  <ul>
                    {page.data.items.map((item, i) => (
                      <li key={i}>
                        <pre>{show(item)}</pre>
                        {mode === "records" && (
                          <button
                            disabled={busy}
                            onClick={() => void read(item as RecordView)}
                          >
                            この版を参照
                          </button>
                        )}
                      </li>
                    ))}
                  </ul>
                  {page.meta.next_cursor && (
                    <button
                      disabled={busy}
                      onClick={() => void query(page.meta.next_cursor!)}
                    >
                      次の50件
                    </button>
                  )}
                  {mode === "export" && (
                    <a
                      download="koujou-usage-page.json"
                      href={`data:application/json;charset=utf-8,${encodeURIComponent(show(page))}`}
                    >
                      表示ページを書き出す
                    </a>
                  )}
                </>
              )}
              {detail && (
                <>
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
                </>
              )}
            </ScreenContent>
            <ScreenContent screens={["pricing"]}>
              {priceID && version !== "0" && (
                <p>
                  編集中の料金版: {priceID} / {version}
                </p>
              )}
              {role === "viewer" && (
                <p>料金設定の変更には編集権限が必要です。</p>
              )}
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
                      ["contract", "契約の識別名（個別契約を含む）"],
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
                    適用終了（ISO 8601・任意）
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
                      placeholder="input / cache / output / image など"
                      onChange={(e) => setCategory(e.target.value)}
                    />
                  </label>
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
          </details>
        }
      </ScreenContent>
      <ScreenContent>
        {message && (
          <p role="status" ref={messageRef} tabIndex={-1}>
            {message}
          </p>
        )}
      </ScreenContent>
    </>
  );
}
