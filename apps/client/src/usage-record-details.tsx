import { useEffect, useRef } from "react";
import { UsageEntryView, type UsageEntry } from "./run-usage";
import {
  amountLabel,
  costLabel,
  openCostEditor,
  type CostRef,
} from "./usage-comparison-model";
export function UsageRecordDetails({
  record,
  onRead,
}: {
  record: CostRef & { kind: string; value: unknown };
  onRead: (ref: CostRef) => void;
}) {
  const target = useRef<HTMLElement>(null),
    value = record.value as Record<string, unknown>;
  useEffect(() => {
    target.current?.scrollIntoView({ block: "nearest" });
    target.current?.focus({ preventScroll: true });
  }, [record.id, record.version]);
  const ref = (r: unknown) => {
    if (!r || typeof r !== "object") return "未取得";
    const value = r as CostRef;
    return (
      <button onClick={() => onRead(value)}>
        {value.id} @ {value.version}
      </button>
    );
  };
  return (
    <section ref={target} tabIndex={-1} aria-label="費用の根拠">
      <h4>{costLabel(record.kind)}の固定記録</h4>
      <p>
        {record.id} @ {record.version}
      </p>
      <dl>
        <dt>取得元・評価根拠</dt>
        <dd>{String(value.source ?? "未取得")}</dd>
        <dt>契約</dt>
        <dd>{String(value.contract ?? "未取得")}</dd>
        <dt>記録日時・適用開始</dt>
        <dd>{String(value.at ?? value.effective_from ?? "未取得")}</dd>
      </dl>
      {record.kind === "usage" && (
        <UsageEntryView
          entry={{
            key: record.id,
            group: String(value.coverage_group),
            kind: "usage",
            state:
              value.coverage === "confirmed" ? "confirmed" : "review_needed",
            amount: null,
            currency: "",
            refs: [{ id: record.id, version: record.version }],
            value: value as UsageEntry["value"],
            quantities: (value.quantities ?? {}) as UsageEntry["quantities"],
          }}
        />
      )}
      {record.kind === "evaluation" && (
        <dl>
          <dt>評価種別</dt>
          <dd>{costLabel(String(value.kind))}</dd>
          <dt>金額と通貨</dt>
          <dd>
            {amountLabel(
              value.amount as string | null,
              String(value.currency ?? ""),
            )}
          </dd>
          <dt>状態</dt>
          <dd>{costLabel(String(value.state))}</dd>
          <dt>料金の固定版</dt>
          <dd>{ref(value.price)}</dd>
          <dt>前の費用評価</dt>
          <dd>{ref(value.previous)}</dd>
          <dt>通貨換算の根拠</dt>
          <dd>{value.fx ? JSON.stringify(value.fx) : "通貨換算なし"}</dd>
        </dl>
      )}
      {record.kind === "price" && (
        <>
          <p>
            この料金の固定版は過去評価の根拠です。変更後の料金を使うには明示的な再評価が必要です。
          </p>
          <dl>
            <dt>対象モデル</dt>
            <dd>{String(value.model)}</dd>
            <dt>通貨</dt>
            <dd>{String(value.currency)}</dd>
            <dt>料金の情報源</dt>
            <dd>{String(value.source)}</dd>
          </dl>
        </>
      )}
      <button onClick={() => openCostEditor("cost-model-editor")}>
        この根拠からモデル接続の編集へ
      </button>
      <button onClick={() => openCostEditor("cost-context-editor")}>
        この根拠からContextの編集へ
      </button>
      <button onClick={() => openCostEditor("cost-definition-editor")}>
        この根拠から工程定義の編集へ
      </button>
      <details>
        <summary>記録の技術情報と全項目</summary>
        <pre>{JSON.stringify(record, null, 2)}</pre>
      </details>
    </section>
  );
}
