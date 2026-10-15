import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { invoke } from "@tauri-apps/api/core";
import { syncRequest } from "./auth";
import { SyncHTTPError } from "./sync-error";

type Job = {
  id: number;
  name: string;
  status: string;
  conclusion: string;
  run_attempt: number;
};
type Snapshot = {
  url: string;
  title?: string;
  source_updated_at?: string;
  source_version: string;
  state: string;
  commit?: string;
  merge_commit?: string;
  workflow_id?: number;
  run_id?: number;
  run_attempt?: number;
  conclusion?: string;
  jobs?: Job[];
};
type Observation = {
  target: {
    id: string;
    repository: string;
    repository_id: number;
    kind: string;
    external_id: string;
    attempt?: number;
  };
  observation_state: string;
  reason?: string;
  checked_at: string | null;
  observed_at: string | null;
  due_at: string;
  delayed: boolean;
  version: number;
  snapshot: Snapshot | null;
  expected_commit?: string;
  required_jobs?: string[];
};
type Page = { data: Observation[]; meta: { next_cursor: string | null } };
type History = {
  items: { version: number; observed_at: string; snapshot: Snapshot }[];
  next_cursor: string;
};
const reasons: Record<string, string> = {
  not_fetched: "未取得",
  connection_failed: "GitHubへの接続失敗",
  authentication_failed: "GitHubの認証失敗",
  not_accessible: "GitHubの対象へアクセスできません",
  rate_limited: "GitHubのAPI制限により待機中",
  fetch_interrupted: "取得が中断されました",
  source_unavailable: "GitHubが応答できません",
  changed_during_read: "取得中に実行状態が変化しました。再照合を待っています",
  invalid_source_response: "GitHubの応答を確認できません",
  unexpected_permissions: "観測専用の権限を確認してください",
  repository_not_allowed: "接続先リポジトリの許可を確認してください",
};
const at = (value: string | null | undefined) =>
  value ? new Date(value).toLocaleString() : "未取得";
// A source conclusion is deliberately displayed without turning it into a
// workflow acceptance decision. Required-job coverage is a separate fact.
export function GithubSnapshot({
  value,
  expected,
  required = [],
}: {
  value: Snapshot;
  expected?: string;
  required?: string[];
}) {
  return (
    <div className="github-snapshot">
      <p>
        {value.title} / GitHubの状態: {value.state}
        {value.conclusion ? ` / CIの結論: ${value.conclusion}` : ""}
      </p>
      {value.commit && (
        <p>
          対象commit: <code>{value.commit}</code>
          {expected && (
            <strong>
              {value.commit.toLowerCase() === expected.toLowerCase()
                ? "（指定commitと一致）"
                : "（指定commitと不一致）"}
            </strong>
          )}
        </p>
      )}
      {value.merge_commit && (
        <p>
          merge commit: <code>{value.merge_commit}</code>
        </p>
      )}
      {value.run_id && (
        <p>
          workflow {value.workflow_id} / 実行 {value.run_id} /{" "}
          {value.run_attempt}回目
        </p>
      )}
      {required.length > 0 && (
        <ul>
          {required.map((name) => {
            const jobs = value.jobs?.filter((job) => job.name === name) ?? [];
            return (
              <li key={name}>
                必要ジョブ {name}:{" "}
                {jobs.length
                  ? jobs
                      .map(
                        (job) =>
                          `${job.status} / ${job.conclusion || "結論なし"}`,
                      )
                      .join(", ")
                  : "未取得・対象ジョブなし"}
              </li>
            );
          })}
        </ul>
      )}
      {value.jobs && (
        <details>
          <summary>全ジョブ（{value.jobs.length}件）</summary>
          <ul>
            {value.jobs.map((job) => (
              <li key={job.id}>
                {job.name}: {job.status} / {job.conclusion || "結論なし"} /{" "}
                {job.run_attempt}回目
              </li>
            ))}
          </ul>
        </details>
      )}
      <p>
        取得元の更新: {at(value.source_updated_at)} / 取得内容の版:{" "}
        <code>{value.source_version}</code>
      </p>
    </div>
  );
}
function SourceLink({ href, native }: { href: string; native: boolean }) {
  // Only the known GitHub host is an external navigation destination.
  const url = new URL(href);
  if (url.protocol !== "https:" || url.hostname !== "github.com") return null;
  return native ? (
    <button onClick={() => void invoke("open_github", { url: href })}>
      GitHubで開く
    </button>
  ) : (
    <a href={href} target="_blank" rel="noreferrer">
      GitHubで開く
    </a>
  );
}
export function GithubObservations({
  workspace,
  native,
  api,
  user,
}: {
  workspace: string;
  native: boolean;
  api: string;
  user: string;
}) {
  const [after, setAfter] = useState("");
  const [error, setError] = useState("");
  const [history, setHistory] = useState<{
    target: string;
    page: History;
    before: string;
  }>();
  const base = `/github/workspaces/${encodeURIComponent(workspace)}/observations`;
  const query = useQuery({
    queryKey: ["github", api, user, workspace, after],
    queryFn: async () =>
      (await syncRequest(`${base}?after=${encodeURIComponent(after)}`, native, {
        method: "GET",
      })) as Page,
    retry: false,
    refetchInterval: 15000,
    gcTime: 0,
  });
  const denied =
    query.error instanceof SyncHTTPError &&
    (query.error.status === 401 || query.error.status === 403);
  const page = denied ? undefined : query.data;
  const unavailable =
    query.error instanceof SyncHTTPError &&
    (query.error.response as { error?: string })?.error === "not_configured";
  useEffect(() => {
    if (
      history &&
      (denied || !page?.data.some((item) => item.target.id === history.target))
    )
      setHistory(undefined);
  }, [page, denied, history]);
  async function readHistory(target: string, before = "") {
    try {
      const result = (await syncRequest(
        `${base}/${encodeURIComponent(target)}/history?before=${before}`,
        native,
        { method: "GET" },
      )) as History;
      setHistory({ target, page: result, before });
    } catch {
      setHistory(undefined);
      setError("観測履歴を取得できません。");
    }
  }
  return (
    <details className="workspace-management github-observations">
      <summary>GitHubの共有観測</summary>
      <p>サーバー側で観測します。CIの結論と工程の受入判定は別の事実です。</p>
      <button
        onClick={() => {
          setHistory(undefined);
          setError("");
          void query.refetch();
        }}
      >
        共有観測を再読込
      </button>
      {query.error && (
        <p role="status">
          {unavailable
            ? "GitHubの観測接続は未設定です。"
            : "共有観測を取得できません。表示中の結果は前回取得時点のものです。"}
        </p>
      )}
      {error && <p role="status">{error}</p>}
      {!error && page?.data.length === 0 && (
        <p>
          {after
            ? "このページに閲覧可能な対象はありません。"
            : "登録された観測対象はありません。"}
        </p>
      )}
      {page?.data.map((item) => (
        <article key={item.target.id} className="github-observation">
          <h3>
            {item.target.repository} / {item.target.kind}{" "}
            {item.target.external_id}
          </h3>
          <p>
            観測:{" "}
            {item.observation_state === "observed"
              ? "取得済み"
              : item.observation_state === "not_fetched"
                ? "未取得"
                : "Unknown（現在状態を確認できません）"}{" "}
            {item.reason && (reasons[item.reason] ?? item.reason)}{" "}
            {item.delayed && " / 定期照合が遅れています"}
          </p>
          <p>
            最終確認: {at(item.checked_at)} / 最終成功取得:{" "}
            {at(item.observed_at)} / 次回照合予定: {at(item.due_at)}
          </p>
          {item.snapshot && (
            <>
              <GithubSnapshot
                value={item.snapshot}
                expected={item.expected_commit}
                required={item.required_jobs}
              />
              <SourceLink href={item.snapshot.url} native={native} />
            </>
          )}
          <button onClick={() => void readHistory(item.target.id)}>
            変更観測の履歴
          </button>
          {history?.target === item.target.id && (
            <div>
              <button onClick={() => void readHistory(item.target.id)}>
                最新の履歴
              </button>
              {history.page.items.map((entry) => (
                <div key={entry.version}>
                  <h4>
                    観測版 {entry.version} / {at(entry.observed_at)}
                  </h4>
                  <GithubSnapshot value={entry.snapshot} />
                </div>
              ))}
              {history.page.next_cursor && (
                <button
                  onClick={() =>
                    void readHistory(item.target.id, history.page.next_cursor)
                  }
                >
                  前の履歴
                </button>
              )}
            </div>
          )}
        </article>
      ))}
      {after && (
        <button
          onClick={() => {
            setAfter("");
            setHistory(undefined);
          }}
        >
          最初のページ
        </button>
      )}
      {page?.meta.next_cursor && (
        <button
          onClick={() => {
            setAfter(page.meta.next_cursor!);
            setHistory(undefined);
          }}
        >
          次の観測対象
        </button>
      )}
    </details>
  );
}
