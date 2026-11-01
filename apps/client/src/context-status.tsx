import type { Envelope, Meta } from "./context-ui";
const reasons: Record<string, string> = {
  projection_pending: "正本への更新を検索・探索へ反映中です。",
  required_version_pending: "必要な版の反映が済んでいません。",
  search_unavailable: "検索エンジンへ接続できません。",
  graph_unavailable: "関係探索へ接続できません。",
  arcade_unavailable: "関係探索へ接続できません。",
  limit_reached: "続きの候補があります。表示中の範囲だけを確認しています。",
  page_remaining: "続きがあります。",
  forbidden: "閲覧権限を確認できません。",
  not_accessed: "権限不足のため取得していません。",
  depth_limit: "探索の深さで打ち切りました。",
  node_limit: "探索したノード数の上限に達しました。",
  edge_limit: "探索した関係数の上限に達しました。",
  timeout: "探索の時間上限に達しました。",
  unfetched: "取得していない参照があります。",
  missing_reference: "参照先を取得できません。",
  empty_selection: "Contextが選ばれていません。注入なしも未選択です。",
  external_unfetched: "未取得の固定参照・添付があります。",
  external_failed: "固定参照・添付の取得に失敗しています。",
  conflicting_revisions: "同じContextの異なる版が選ばれています。",
  selection_conflict: "注入なしとContextの選択が両立していません。",
  version_conflict: "同じContextの異なる版が選ばれています。",
  incomplete_candidates: "候補の範囲を完全に確認できていません。",
  invalid_selection: "注入なしとContextの選択が両立していません。",
  unobserved: "未観測",
};
export const reasonText = (reason: string) => reasons[reason] ?? reason;
export function Completeness({
  meta,
  error,
}: {
  meta?: Meta;
  error?: unknown;
}) {
  if (error)
    return (
      <p role="alert">
        取得できません。
        {meta?.completeness.reason
          ? reasonText(meta.completeness.reason)
          : String(error).replace(/^Error: /, "")}
      </p>
    );
  if (!meta) return <p role="status">取得中…</p>;
  const state = meta.completeness;
  return (
    <p
      className={
        state.state === "complete" ? "context-meta" : "context-warning"
      }
      role="status"
    >
      {state.state === "complete"
        ? "指定した範囲の取得を確認しました。"
        : `未完了：${reasonText(state.reason ?? state.state)}`}
      <small> 確認位置：{meta.position ?? "未確認"}</small>
    </p>
  );
}
export function ListCount({
  label,
  items,
  more,
  empty = `${label}はありません。`,
}: {
  label: string;
  items?: readonly unknown[];
  more?: string | null;
  empty?: string;
}) {
  if (!items) return null;
  return (
    <p className="context-meta">
      {label}：{items.length}件{more && "（続きあり）"}
      {!items.length && <> {empty}</>}
    </p>
  );
}
export function EnvelopeStatus({
  value,
  error,
}: {
  value?: Envelope<unknown>;
  error?: unknown;
}) {
  return (
    <Completeness meta={value?.meta} error={error ?? value?.error?.code} />
  );
}
