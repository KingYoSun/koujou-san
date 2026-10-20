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
  return (
    <section aria-label="取得した比較結果">
      <p>
        集計時点: {page.data.as_of} / 反映位置: {page.data.through} /{" "}
        {page.data.timezone}
      </p>
      <p>
        {costLabel(condition.kind)}
        。通貨・評価種別・計測範囲ごとに表示します。取得済み部分を全費用と扱いません。
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
            <dd>{summary.execution_seconds ?? "未取得"} 秒</dd>
            <dt>開始までの待機時間の取得済み合計</dt>
            <dd>{summary.waiting_seconds ?? "未取得"} 秒</dd>
            <dt>時間・Run情報の未取得</dt>
            <dd>
              {summary.time_unavailable}件 / {summary.run_metadata_unavailable}
              件
            </dd>
          </dl>
          <p>
            時間はRunごとに一度だけ数えます。実行時間にはRun内の回答待ちを含みます。手戻りは明示された前Runへの参照を数え、失敗から推測しません。
          </p>
          <ul>
            {summary.quality.map((q, i) => (
              <li key={i}>
                {costLabel(q.source)} · {costLabel(q.state)} ·{" "}
                {costLabel(q.verdict)}: {q.records}件
              </li>
            ))}
          </ul>
          <p>
            保存時の検品結果です。現在の有効性は検品の詳細で確認してください。CLIの自己申告を独立検品のPASSとして比較しません。
          </p>
        </>
      )}
      {page.data.items.length === 0 && (
        <p>
          この条件で参照できる記録はありません。未取得の費用を0と補完しません。
        </p>
      )}
      <table>
        <caption>
          {mode === "compare_quality"
            ? "検品条件ごとの保存時判定"
            : mode === "compare_details"
              ? "元計測と費用の根拠"
              : "費用と計測範囲"}
        </caption>
        <thead>
          <tr>
            {mode === "compare_quality" ? (
              <>
                <th>検品条件の固定版</th>
                <th>判定の種類</th>
                <th>保存時の結果</th>
                <th>件数</th>
              </>
            ) : (
              <>
                <th>帰属・モデル</th>
                <th>数量・範囲</th>
                <th>評価・金額</th>
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
                  <td>{r.records}</td>
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
                    {r.records == null ? "" : `${r.records}計測範囲`}
                  </td>
                  <td>
                    <details>
                      <summary>数量と計測範囲</summary>
                      {r.quantities && Array.isArray(r.quantities) ? (
                        r.quantities.map((q) => (
                          <p key={q.category}>
                            {costLabel(q.category)}: {q.value ?? "未取得"}{" "}
                            {q.unit}（実測 {q.reported}、推定 {q.estimated}
                            、確認対象 {q.uncertain ?? 0}、未取得 {q.missing}
                            、非該当 {q.not_applicable}）
                          </p>
                        ))
                      ) : (
                        <p>元記録の数量と取得範囲を根拠から確認できます。</p>
                      )}
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
                  <td>
                    {costLabel(condition.kind)}
                    <br />
                    {amountLabel(r.amount, r.currency)}
                    <br />
                    {costLabel(r.state)}
                    <p>
                      定額・ローカルの支払額を0と扱いません。API換算額は支払額や節約額ではありません。
                    </p>
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
                        この計測範囲を明示再評価
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
        <button disabled={busy} onClick={onNext}>
          比較結果の次の50件
        </button>
      )}
      {mode === "compare" && (
        <button disabled={busy} onClick={onDetails}>
          この条件の元計測・料金を50件取得
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
