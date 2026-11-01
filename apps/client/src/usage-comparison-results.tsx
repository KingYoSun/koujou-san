import {
  amountLabel,
  distinctCostRefs,
  costLabel,
  type CostCondition,
  type CostPage,
  type CostRef,
  type CostRow,
} from "./usage-comparison-model";
import { NavigationLink, useNavigation } from "./navigation";
export function ComparisonResults({
  page,
  condition,
  mode,
  onNext,
  onDetails,
  onRead,
  onReevaluate,
  busy,
  editable,
}: {
  page: CostPage;
  condition: CostCondition;
  mode: string;
  onNext: () => void;
  onDetails: () => void;
  onRead: (ref: CostRef) => void;
  onReevaluate: (row: CostRow) => void;
  busy: boolean;
  editable: boolean;
}) {
  const navigation = useNavigation();
  const summary = page.data.summary;
  const measured = (v: string | null | undefined, unit: string) =>
    v == null ? "未取得" : `${v} ${unit}`;
  return (
    <section aria-label="取得した比較結果">
      <p>
        集計時点：{page.data.as_of} / 反映位置：{page.data.through} /{" "}
        {page.data.timezone} / {page.data.items.length}件を表示
        {page.meta.next_cursor && "（続きあり）"}
      </p>
      {summary && (
        <>
          <dl>
            <dt>対象Run</dt>
            <dd>
              {summary.runs}件（終了 {summary.completed}、失敗 {summary.failed}
              、未完了 {summary.unfinished}）
            </dd>
            <dt>手戻り・再試行</dt>
            <dd>{summary.rework}件</dd>
            <dt>実行時間の取得済み合計</dt>
            <dd>{measured(summary.execution_seconds, "秒")}</dd>
            <dt>開始までの待機時間の取得済み合計</dt>
            <dd>{measured(summary.waiting_seconds, "秒")}</dd>
            <dt>時間・Run情報の未取得</dt>
            <dd>
              {summary.time_unavailable}件 / {summary.run_metadata_unavailable}
              件
            </dd>
          </dl>
          <ul>
            {summary.quality.map((q, i) => (
              <li key={i}>
                {costLabel(q.source)} · {costLabel(q.state)} ·{" "}
                {costLabel(q.verdict)}：{q.records}件
              </li>
            ))}
          </ul>
        </>
      )}
      {page.data.items.length === 0 && (
        <p>この条件で参照できる記録はありません。</p>
      )}
      <table>
        <caption>
          {mode === "compare_quality"
            ? "検品条件ごとの保存時判定"
            : `${costLabel(condition.kind)}の${
                mode === "compare_details"
                  ? "元計測と費用の根拠"
                  : "費用と計測範囲"
              }`}
          （{condition.from.slice(0, 10)}～{condition.until.slice(0, 10)}）
        </caption>
        <thead>
          <tr>
            {mode === "compare_quality" ? (
              <>
                <th>検品条件の固定版</th>
                <th>判定の種類</th>
                <th>保存時の結果</th>
                <th data-numeric>件数</th>
              </>
            ) : (
              <>
                <th>帰属・モデル</th>
                <th>数量・範囲</th>
                <th data-numeric>評価・金額</th>
                <th>根拠・操作</th>
              </>
            )}
          </tr>
        </thead>
        <tbody>
          {page.data.items.map((r, i) => (
            <tr key={r.key ?? i}>
              {mode === "compare_quality" ? (
                <>
                  <td>
                    {r.inspection
                      ? `${r.inspection.id} @ ${r.inspection.version}`
                      : "独立検品なし・未取得"}
                  </td>
                  <td>{costLabel(r.source)}</td>
                  <td>
                    {costLabel(r.state)} / {costLabel(r.verdict)}
                  </td>
                  <td data-numeric>{r.records}</td>
                </>
              ) : (
                <>
                  <td>
                    {costLabel(r.attribution_kind)}
                    <br />
                    {r.attribution_id}
                    <br />
                    {r.model ?? "モデル未取得"}
                    <br />
                    {r.records == null ? "" : `計測範囲 ${r.records}件`}
                  </td>
                  <td>
                    <details>
                      <summary>数量と計測範囲</summary>
                      {r.quantities && Array.isArray(r.quantities)
                        ? r.quantities.map((q) => (
                            <p key={q.category}>
                              {costLabel(q.category)}：
                              {measured(q.value, q.unit)}（実測 {q.reported}
                              、推定 {q.estimated}、確認対象 {q.uncertain ?? 0}
                              、未取得 {q.missing}、非該当 {q.not_applicable}）
                            </p>
                          ))
                        : null}
                      {costLabel(
                        r.measurement ??
                          (
                            r.value?.measurement as
                              Record<string, string> | undefined
                          )?.state,
                      )}
                      <br />
                      {r.measured_scope ??
                        (
                          r.value?.measurement as
                            Record<string, string> | undefined
                        )?.scope}
                      <br />
                      {costLabel(
                        r.billing ??
                          (
                            r.value?.measurement as
                              Record<string, string> | undefined
                          )?.billing,
                      )}
                    </details>
                  </td>
                  <td data-numeric>
                    {costLabel(condition.kind)}
                    <br />
                    {amountLabel(r.amount, r.currency)}
                    <br />
                    {costLabel(r.state)}
                  </td>
                  <td>
                    {distinctCostRefs(r.refs).flatMap((ref) => {
                      if (
                        !ref.version ||
                        ref.version === "0" ||
                        ref.version === "head"
                      )
                        return [];
                      const kind = ref.id.split(":")[0];
                      if (
                        kind !== "run" &&
                        kind !== "run-definition" &&
                        kind !== "workflow-definition"
                      )
                        return [];
                      return [
                        <p key={ref.id + "@" + ref.version}>
                          <NavigationLink
                            to={{
                              workspace: navigation?.route.workspace ?? "",
                              screen: kind === "run" ? "work" : "workflows",
                              target: { ...ref, kind },
                              view: kind === "run" ? "history" : "edit",
                            }}
                            related
                          >
                            {kind === "run"
                              ? "この計測のRunを確認"
                              : "使用した定義の固定版を確認"}
                          </NavigationLink>
                        </p>,
                      ];
                    })}
                    {distinctCostRefs(r.refs)
                      .filter((ref) =>
                        /^(usage|price|evaluation):/.test(ref.id),
                      )
                      .map((ref) => (
                        <button
                          disabled={busy}
                          key={ref.id + "@" + ref.version}
                          onClick={() => onRead(ref)}
                        >
                          {ref.id.startsWith("price:")
                            ? "料金版"
                            : ref.id.startsWith("evaluation:")
                              ? "費用評価"
                              : "元計測"}{" "}
                          {ref.id} @ {ref.version}
                        </button>
                      ))}
                    {editable && r.group && (
                      <button
                        disabled={busy}
                        onClick={() =>
                          onReevaluate({ ...r, refs: distinctCostRefs(r.refs) })
                        }
                      >
                        計測範囲を再評価
                      </button>
                    )}
                  </td>
                </>
              )}
            </tr>
          ))}
        </tbody>
      </table>
      {page.meta.next_cursor && (
        <button disabled={busy} data-variant="secondary" onClick={onNext}>
          比較結果の次のページ
        </button>
      )}
      {mode === "compare" && (
        <button
          disabled={busy}
          aria-busy={busy}
          data-variant="secondary"
          onClick={onDetails}
        >
          元計測・料金を取得
        </button>
      )}
      <a
        download="koujou-comparison-page.json"
        href={
          "data:application/json;charset=utf-8," +
          encodeURIComponent(JSON.stringify({ condition, page }, null, 2))
        }
      >
        条件と表示ページを書き出す
      </a>
    </section>
  );
}
