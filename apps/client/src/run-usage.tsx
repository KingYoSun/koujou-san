import { useEffect, useRef, useState } from "react";
import { syncRequest } from "./auth";
import { SyncHTTPError } from "./sync-error";
import { Disclosure } from "./disclosure";
import { costLabel } from "./usage-comparison-model";

type Ref = { id: string; version: string };
type Quantity = {
  value: string | null;
  state: string;
  unit: string;
  reason?: string;
  includes?: string[];
};
export type UsageEntry = {
  key: string;
  group: string;
  kind: string;
  state: string;
  amount: string | null;
  currency: string;
  model?: string | null;
  refs: Ref[];
  quantities: Record<string, Quantity>;
  value: {
    actual_model?: string | null;
    connection?: string;
    contract?: string;
    source?: string;
    price?: Ref;
    quantities?: Record<string, Quantity>;
    measurement?: {
      state: string;
      protocol: string;
      cli_version: string;
      billing: string;
      scope: string;
      reason?: string;
      estimate_usd?: string | null;
      source_ref: string;
      source_digest: string;
    };
  };
};
type Page = {
  data: { items: UsageEntry[] };
  meta: { next_cursor: string | null };
};
const kinds: Record<string, string> = {
  usage: "利用量",
  reported: "報告額",
  estimated: "単価からの推定額",
  api_equivalent: "API料金換算額",
  allocated: "配賦額",
};
const states: Record<string, string> = {
  provisional: "途中更新・暫定",
  final: "終了時取得",
  partial: "一部取得",
  unavailable: "取得不可",
  not_applicable: "LLM非該当",
  review_needed: "帰属・計測範囲の確認が必要",
  confirmed: "取得範囲を確認済み",
  pending: "遅着・訂正後の再評価待ち",
  unrated: "未算定",
  rated: "評価済み",
};
const categories: Record<string, string> = {
  input: "入力",
  cache_read: "キャッシュ読取り",
  cache_write: "キャッシュ作成",
  output: "出力",
  reasoning: "推論",
};

export function UsageEntryView({ entry }: { entry: UsageEntry }) {
  const m = entry.value.measurement;
  const uncertain = entry.state === "review_needed";
  return (
    <section aria-label="モデル別の計測">
      <h5>{entry.model ?? entry.value.actual_model ?? "モデル未取得"}</h5>
      <p>
        {kinds[entry.kind] ?? entry.kind}：{states[entry.state] ?? entry.state}
      </p>
      {m && (
        <>
          <p>
            計測：{states[m.state] ?? m.state} / {m.protocol} {m.cli_version}
          </p>
          <p>
            取得範囲：
            {m.state === "not_applicable"
              ? "LLMを使用しない接続"
              : m.state === "unavailable"
                ? "計測範囲は未確認"
                : m.protocol === "codex-jsonl"
                  ? "親セッション（子セッションは未取得）"
                  : m.protocol === "claude-jsonl"
                    ? "CLIの処理パイプライン（範囲外の補助呼出しは未取得）"
                    : m.scope}
          </p>
          <p>契約：{costLabel(m.billing)}</p>
          {m.reason === "session_boundary_unconfirmed" && (
            <p>
              セッション累計から今回のRunの消費を切り分けられないため、確定した利用量・費用へ計上していません。
            </p>
          )}
          {m.estimate_usd != null && !uncertain && (
            <p>CLIの参考推定額：{m.estimate_usd} USD</p>
          )}
        </>
      )}
      {uncertain ? (
        <p>数量は今回のRunへ確定帰属していません。</p>
      ) : (
        <dl>
          {Object.entries(entry.value.quantities ?? entry.quantities ?? {}).map(
            ([key, q]) => (
              <div key={key}>
                <dt>
                  {categories[key] ?? key}
                  {q.includes?.length
                    ? `（${q.includes.map((v) => categories[v] ?? v).join("・")}を含む）`
                    : ""}
                </dt>
                <dd>
                  {q.state === "not_applicable"
                    ? "非該当"
                    : q.value == null
                      ? "未取得"
                      : `${/^\d+$/.test(q.value) ? Number(q.value).toLocaleString("ja-JP") : q.value} ${q.unit}`}
                  {q.state === "estimated" ? "（推定）" : ""}
                </dd>
              </div>
            ),
          )}
        </dl>
      )}
      {entry.kind !== "usage" && (
        <p>
          {kinds[entry.kind]}：
          {entry.amount == null
            ? "未算定"
            : `${entry.amount} ${entry.currency}`}
        </p>
      )}
      {entry.kind !== "usage" && entry.value.source && (
        <p>情報源：{entry.value.source}</p>
      )}
    </section>
  );
}

export async function queryRunUsage(
  native: boolean,
  workspace: string,
  runID: string,
  kind = "usage",
  cursor = "",
) {
  return (await syncRequest(
    `/usage/workspaces/${encodeURIComponent(workspace)}/query`,
    native,
    {
      method: "POST",
      body: { mode: "run", kind, attribution_id: runID, cursor, limit: 50 },
    },
  )) as Page;
}

export function RunUsage({
  native,
  workspace,
  runID,
  onDenied,
}: {
  native: boolean;
  workspace: string;
  runID: string;
  onDenied: (e: unknown) => void;
}) {
  const [kind, setKind] = useState("usage");
  const [page, setPage] = useState<Page | null>(null);
  const [detail, setDetail] = useState<unknown>(null);
  const [message, setMessage] = useState("");
  const [busy, setBusy] = useState(false);
  const epoch = useRef(0);
  const fail = (e: unknown) => {
    setPage(null);
    setDetail(null);
    setMessage(
      "利用量を取得できません。接続と現在の閲覧権限を確認してください。",
    );
    if (e instanceof SyncHTTPError && (e.status === 401 || e.status === 403))
      onDenied(e);
  };
  const load = async (cursor = "", requestedKind = kind) => {
    const current = ++epoch.current;
    setBusy(true);
    setMessage("");
    setDetail(null);
    try {
      const value = await queryRunUsage(
        native,
        workspace,
        runID,
        requestedKind,
        cursor,
      );
      if (epoch.current === current) setPage(value);
    } catch (e) {
      if (epoch.current === current) fail(e);
    } finally {
      if (epoch.current === current) setBusy(false);
    }
  };
  useEffect(() => {
    void load();
    return () => {
      epoch.current++;
    };
  }, [native, workspace, runID]);
  const read = async (ref: Ref) => {
    const current = ++epoch.current;
    setBusy(true);
    setMessage("");
    try {
      const value = await syncRequest(
        `/usage/workspaces/${encodeURIComponent(workspace)}/records/${encodeURIComponent(ref.id)}?version=${encodeURIComponent(ref.version)}`,
        native,
        { method: "GET" },
      );
      if (epoch.current === current) setDetail(value);
    } catch (e) {
      if (epoch.current === current) fail(e);
    } finally {
      if (epoch.current === current) setBusy(false);
    }
  };
  return (
    <section aria-label="このRunのモデル・利用量・費用">
      <h4>このRunのモデル・利用量・費用</h4>
      <label>
        表示する計測・評価
        <select
          disabled={busy}
          value={kind}
          onChange={(e) => {
            setKind(e.target.value);
            void load("", e.target.value);
          }}
        >
          {Object.entries(kinds).map(([value, label]) => (
            <option key={value} value={value}>
              {label}
            </option>
          ))}
        </select>
      </label>
      <button
        data-variant="secondary"
        disabled={busy}
        aria-busy={busy}
        onClick={() => void load()}
      >
        {busy ? "利用量を取得中…" : "このRunの利用量を更新"}
      </button>
      {message && <p role="status">{message}</p>}
      {page && (
        <p>
          {page.data.items.length}件を表示
          {page.meta.next_cursor ? "（続きあり）" : ""}
        </p>
      )}
      {page?.data.items.length === 0 && (
        <p>この種別の記録は未取得・未算定です。</p>
      )}
      {page?.data.items.map((entry) => (
        <article key={entry.key}>
          <UsageEntryView entry={entry} />
          <Disclosure title="計測・料金の根拠を確認">
            {() => (
              <>
                {entry.value.measurement?.source_ref && (
                  <p>端末内の原記録は未取得です。</p>
                )}
                {entry.refs.map((ref) => (
                  <button
                    key={ref.id + ref.version}
                    data-variant="secondary"
                    disabled={busy}
                    onClick={() => void read(ref)}
                  >
                    {ref.id.startsWith("price:")
                      ? "固定した料金版"
                      : "固定した計測・評価記録"}
                    を開く：{ref.id}
                  </button>
                ))}
                <pre>{JSON.stringify(entry.value, null, 2)}</pre>
              </>
            )}
          </Disclosure>
        </article>
      ))}
      {page?.meta.next_cursor && (
        <button
          data-variant="secondary"
          disabled={busy}
          onClick={() => void load(page.meta.next_cursor!)}
        >
          利用量・評価の次のページ
        </button>
      )}
      {detail != null && (
        <section aria-label="固定した利用量・料金の根拠">
          <h5>固定した利用量・料金の根拠</h5>
          <pre>{JSON.stringify(detail, null, 2)}</pre>
        </section>
      )}
    </section>
  );
}
