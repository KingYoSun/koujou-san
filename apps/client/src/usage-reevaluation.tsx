import { useEffect, useRef, useState } from "react";
import { type CostRef, type CostRow } from "./usage-comparison-model";
export function UsageReevaluation({
  row,
  call,
  onSaved,
}: {
  row: CostRow;
  call: <T>(path: string, body?: unknown) => Promise<T>;
  onSaved: (ref: CostRef) => void;
}) {
  const [kind, setKind] = useState("api_equivalent"),
    [price, setPrice] = useState(""),
    [priceVersion, setPriceVersion] = useState(""),
    [previous, setPrevious] = useState(""),
    [previousVersion, setPreviousVersion] = useState(""),
    [amount, setAmount] = useState(""),
    [currency, setCurrency] = useState("USD"),
    [source, setSource] = useState(""),
    [busy, setBusy] = useState(false),
    [notice, setNotice] = useState("");
  const pending = useRef<
    { key: string; id: string; operation: string } | undefined
  >(undefined);
  useEffect(() => {
    const p = row.refs?.find((r) => r.id.startsWith("price:")),
      old = row.refs?.filter((r) => r.id.startsWith("evaluation:")).at(-1);
    setPrice(p?.id ?? "");
    setPriceVersion(p?.version ?? "");
    setPrevious(old?.id ?? "");
    setPreviousVersion(old?.version ?? "");
    setNotice("");
    pending.current = undefined;
  }, [row]);
  async function save() {
    setBusy(true);
    setNotice("");
    try {
      const value = {
        coverage_group: row.group,
        kind,
        usage: row.refs?.filter((r) => r.id.startsWith("usage:")) ?? [],
        price: ["estimated", "api_equivalent"].includes(kind)
          ? { id: price, version: priceVersion }
          : null,
        previous: previous
          ? { id: previous, version: previousVersion }
          : undefined,
        amount: amount || null,
        currency,
        source,
        scale: 6,
        rounding: "half_up",
      };
      const key = JSON.stringify(value);
      if (pending.current?.key !== key)
        pending.current = {
          key,
          id: "evaluation:" + crypto.randomUUID(),
          operation: crypto.randomUUID(),
        };
      const result = await call<{ data: CostRef }>(
        "/records/" + encodeURIComponent(pending.current.id),
        {
          operation_id: pending.current.operation,
          expected_version: "0",
          value,
        },
      );
      setNotice(
        `費用評価 ${result.data.id} @ ${result.data.version} を保存しました。`,
      );
      onSaved({ id: result.data.id, version: result.data.version });
    } catch {
      setNotice("費用評価を保存できませんでした。");
    } finally {
      setBusy(false);
    }
  }
  return (
    <fieldset disabled={busy}>
      <legend>費用を明示的に再評価</legend>
      <p>対象計測：{row.group}</p>
      <label>
        評価種別
        <select value={kind} onChange={(e) => setKind(e.target.value)}>
          <option value="api_equivalent">API料金換算額</option>
          <option value="estimated">単価からの推定額</option>
          <option value="reported">報告額</option>
          <option value="allocated">配賦額</option>
        </select>
      </label>
      {["estimated", "api_equivalent"].includes(kind) ? (
        <>
          <label>
            使う料金ID
            <input value={price} onChange={(e) => setPrice(e.target.value)} />
          </label>
          <label>
            使う料金の固定版
            <input
              value={priceVersion}
              onChange={(e) => setPriceVersion(e.target.value)}
            />
          </label>
        </>
      ) : (
        <>
          <label>
            金額
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
            金額の根拠
            <input value={source} onChange={(e) => setSource(e.target.value)} />
          </label>
        </>
      )}
      <label>
        前の費用評価ID
        <input value={previous} onChange={(e) => setPrevious(e.target.value)} />
      </label>
      <label>
        前の費用評価の固定版
        <input
          value={previousVersion}
          onChange={(e) => setPreviousVersion(e.target.value)}
        />
      </label>
      <button aria-busy={busy} onClick={() => void save()}>
        費用評価を保存
      </button>
      <p role="status">{busy ? "費用評価を保存中…" : notice}</p>
    </fieldset>
  );
}
