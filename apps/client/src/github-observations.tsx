import { ScreenContent, useScreenActive } from "./navigation";
import { useEffect, useState } from "react";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { invoke } from "@tauri-apps/api/core";
import { syncRequest } from "./auth";
import { SyncHTTPError } from "./sync-error";
import { StatusBadge } from "./status-badge";
import { DetailPane } from "./panes";

type Job = {
  id: number;
  name: string;
  status: string;
  conclusion: string;
  run_attempt: number;
};
type Snapshot = {
  provider?: string;
  kind?: string;
  instance?: string;
  server_version?: string;
  workflow?: string;
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
    provider?: string;
    instance?: string;
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
  connection_failed: "接続先への接続失敗",
  authentication_failed: "接続先の認証失敗",
  not_accessible: "対象へアクセスできません",
  rate_limited: "接続先のAPI制限により待機中",
  fetch_interrupted: "取得が中断されました",
  source_unavailable: "接続先が応答できません",
  changed_during_read: "取得中に実行状態が変化しました。再照合を待っています",
  invalid_source_response: "取得元の応答を確認できません",
  status_unobserved: "statusは未観測です",
  workflow_result_unavailable: "対象workflowの実行結果は未取得です",
  unexpected_permissions: "観測専用の権限を確認してください",
  repository_not_allowed: "接続先リポジトリの許可を確認してください",
};
export const at = (value: string | null | undefined) =>
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
  const provider = value.provider === "gitea" ? "Gitea" : "GitHub";
  const matched = value.commit?.toLowerCase() === expected?.toLowerCase();
  return (
    <div className="github-snapshot">
      <p>
        {value.title} / {provider}の状態：{value.state}
        {value.conclusion ? ` / CIの結論：${value.conclusion}` : ""}
      </p>
      {value.commit && (
        <p>
          対象commit：<code>{value.commit}</code>{" "}
          {expected && (
            <StatusBadge
              label="指定commit"
              value={matched ? "一致" : "不一致"}
              tone={matched ? "positive" : "warning"}
            />
          )}
        </p>
      )}
      {value.merge_commit && (
        <p>
          {provider}のmerge_commit_sha：<code>{value.merge_commit}</code>
        </p>
      )}
      {value.run_id && (
        <p>
          workflow {value.workflow ?? value.workflow_id} / 実行 {value.run_id} /{" "}
          {value.run_attempt}回目
        </p>
      )}
      {required.length > 0 && (
        <ul>
          {required.map((name) => {
            const jobs = value.jobs?.filter((job) => job.name === name) ?? [];
            return (
              <li key={name}>
                必要ジョブ {name}：
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
          <summary>
            {value.kind === "status" ? "全status" : "全ジョブ"}：
            {value.jobs.length}件
          </summary>
          <ul>
            {value.jobs.map((job) => (
              <li key={job.id}>
                {job.name}：{job.status} / {job.conclusion || "結論なし"} /{" "}
                {job.run_attempt > 0 ? `${job.run_attempt}回目` : ""}
              </li>
            ))}
          </ul>
        </details>
      )}
      <p>
        取得元の更新：{at(value.source_updated_at)} / 取得内容の版：
        <code>{value.source_version}</code>
      </p>
    </div>
  );
}
export function sourceURL(href: string, instance = "https://github.com") {
  try {
    const base = new URL(instance),
      url = new URL(href);
    if (
      !["http:", "https:"].includes(base.protocol) ||
      base.username ||
      base.password ||
      base.search ||
      base.hash ||
      url.origin !== base.origin ||
      url.username ||
      url.password ||
      !url.pathname.startsWith(base.pathname.replace(/\/$/, "") + "/")
    )
      return;
    return url;
  } catch {
    return;
  }
}
export function SourceLink({
  href,
  native,
  instance,
}: {
  href: string;
  native: boolean;
  instance?: string;
}) {
  const url = sourceURL(href, instance);
  if (!url) return null;
  const provider = instance ? "Gitea" : "GitHub";
  return native ? (
    <button
      onClick={() =>
        void invoke(instance ? "open_forge" : "open_github", {
          url: url.href,
          ...(instance ? { instance } : {}),
        })
      }
    >
      {provider}で開く
    </button>
  ) : (
    <a href={href} target="_blank" rel="noreferrer">
      {provider}で開く
    </a>
  );
}
export function ObservationDetail({
  item,
  native,
  history,
  onHistory,
}: {
  item: Observation;
  native: boolean;
  history?: History;
  onHistory: (before?: string) => void;
}) {
  return (
    <section aria-label="選択した観測の詳細">
      <h4>
        {item.target.repository} / {item.target.kind} {item.target.external_id}
      </h4>
      <p>
        最終確認：{at(item.checked_at)} / 最終成功取得：{at(item.observed_at)} /
        次回照合予定：{at(item.due_at)}
      </p>
      {item.snapshot && (
        <>
          <GithubSnapshot
            value={item.snapshot}
            expected={item.expected_commit}
            required={item.required_jobs}
          />
          <SourceLink
            href={item.snapshot.url}
            native={native}
            instance={item.target.instance}
          />
        </>
      )}
      <button data-variant="secondary" onClick={() => onHistory()}>
        {history ? "最新の履歴を取得" : "変更観測の履歴を取得"}
      </button>
      {history?.items.map((entry) => (
        <div key={entry.version}>
          <h5>
            観測版 {entry.version} / {at(entry.observed_at)}
          </h5>
          <GithubSnapshot value={entry.snapshot} />
        </div>
      ))}
      {history?.next_cursor && (
        <button
          data-variant="secondary"
          onClick={() => onHistory(history.next_cursor)}
        >
          履歴の前のページ
        </button>
      )}
    </section>
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
  const active = useScreenActive();
  const [after, setAfter] = useState("");
  const [error, setError] = useState("");
  const [selected, setSelected] = useState("");
  const [history, setHistory] = useState<{
    target: string;
    page: History;
    before: string;
  }>();
  const base = `/forge/workspaces/${encodeURIComponent(workspace)}/observations`;
  const query = useQuery({
    queryKey: ["github", api, user, workspace, after],
    queryFn: async () =>
      (await syncRequest(`${base}?after=${encodeURIComponent(after)}`, native, {
        method: "GET",
      })) as Page,
    retry: false,
    enabled: active,
    refetchInterval: active ? 15000 : false,
    gcTime: 0,
    placeholderData: keepPreviousData,
  });
  const denied =
    query.error instanceof SyncHTTPError &&
    (query.error.status === 401 || query.error.status === 403);
  const page = denied ? undefined : query.data;
  const unavailable =
    query.error instanceof SyncHTTPError &&
    (query.error.response as { error?: string })?.error === "not_configured";
  const item = page?.data.find((value) => value.target.id === selected);
  useEffect(() => {
    if (selected && !item) {
      setSelected("");
      setHistory(undefined);
    }
  }, [selected, item]);
  function select(target: string) {
    setSelected(target);
    setHistory(undefined);
  }
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
      setError("観測履歴を取得できませんでした。");
    }
  }
  return (
    <ScreenContent>
      <section className="workspace-management github-observations">
        <button
          data-variant="secondary"
          aria-busy={query.isFetching}
          onClick={() => {
            setHistory(undefined);
            setError("");
            void query.refetch();
          }}
        >
          {query.isFetching ? "共有観測を取得中…" : "共有観測を再取得"}
        </button>
        {query.error && (
          <p role="status">
            {unavailable
              ? "外部の観測接続は未設定です。"
              : denied
                ? "共有観測を閲覧する権限がありません。"
                : "共有観測を取得できませんでした。"}
          </p>
        )}
        {error && <p role="status">{error}</p>}
        {page && (
          <p aria-live="polite">
            観測対象：{page.data.length}件を表示
            {page.meta.next_cursor ? "（続きあり）" : ""}
          </p>
        )}
        {page?.data.length === 0 && (
          <p>
            {after
              ? "このページに閲覧可能な対象はありません。"
              : "登録された観測対象はありません。"}
          </p>
        )}
        {page && page.data.length > 0 && (
          <ul>
            {page.data.map((row) => (
              <li key={row.target.id} className="github-observation">
                <button
                  aria-current={row.target.id === selected ? "true" : undefined}
                  onClick={() => select(row.target.id)}
                >
                  {row.target.repository} / {row.target.kind}{" "}
                  {row.target.external_id}
                </button>
                <p>
                  {row.target.provider === "gitea" ? "Gitea" : "GitHub"} ·{" "}
                  {row.target.instance ?? "https://github.com"}
                  {row.snapshot?.server_version
                    ? ` / 確認した版：${row.snapshot.server_version}`
                    : ""}
                </p>
                <p>
                  <StatusBadge
                    label="観測"
                    tone={
                      row.observation_state === "observed" ||
                      row.observation_state === "not_fetched"
                        ? "neutral"
                        : "warning"
                    }
                    value={
                      row.observation_state === "observed"
                        ? "取得済み"
                        : row.observation_state === "not_fetched"
                          ? "未取得"
                          : "Unknown"
                    }
                  />{" "}
                  {row.reason && (reasons[row.reason] ?? row.reason)}{" "}
                  {row.delayed && " / 定期照合が遅れています"}
                </p>
              </li>
            ))}
          </ul>
        )}
        {after && (
          <button
            data-variant="secondary"
            onClick={() => {
              setAfter("");
              select("");
            }}
          >
            観測対象の最初のページ
          </button>
        )}
        {page?.meta.next_cursor && (
          <button
            data-variant="secondary"
            onClick={() => {
              setAfter(page.meta.next_cursor!);
              select("");
            }}
          >
            観測対象の次のページ
          </button>
        )}
        {item && (
          <DetailPane>
            <ObservationDetail
              item={item}
              native={native}
              history={
                history?.target === item.target.id ? history.page : undefined
              }
              onHistory={(before) => void readHistory(item.target.id, before)}
            />
          </DetailPane>
        )}
      </section>
    </ScreenContent>
  );
}
