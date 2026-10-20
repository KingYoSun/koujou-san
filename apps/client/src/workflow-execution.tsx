import { ScreenContent } from "./navigation";
import { useEffect, useRef, useState } from "react";
import { invoke } from "@tauri-apps/api/core";
import { syncRequest, workspaceAccessLost } from "./auth";
import { syncStorage, answerScopeEpoch, allowAnswerScope } from "./sync-store";
import { SyncHTTPError } from "./sync-error";

type Ref = { id: string; version: string };
type Record<T> = Ref & { value: T };
type Execution = {
  definition: Ref;
  state: string;
  reason?: string;
  count: number;
  cost?: {
    state: string;
    amount: string | null;
    currency: string;
    guarantee: string;
  };
};
type Task = {
  node: string;
  attempt: number;
  state: string;
  outcome?: string;
  run?: Ref;
  definition?: Ref;
  stale?: boolean;
};
type Graph = { nodes: { id: string; choices?: string[] }[] };
type Page = { items: Record<Task>[]; next: string };
type Impact = {
  execution: Ref;
  adoption: Ref;
  affected_unstarted: string[];
  invalidated_decisions: string[];
};

export function WorkflowExecution({
  native,
  api,
  user,
  workspace,
  definition,
  canEdit,
  canExecute,
  selected,
  onOpenRun,
}: {
  native: boolean;
  api: string;
  user: string;
  workspace: string;
  definition?: Ref;
  canEdit: boolean;
  canExecute: boolean;
  selected?: Ref;
  onOpenRun?: (ref: Ref) => void;
}) {
  const base = `/workflows/workspaces/${encodeURIComponent(workspace)}`;
  const [id, setID] = useState("");
  const [record, setRecord] = useState<Record<Execution>>();
  const [graph, setGraph] = useState<Graph>();
  const [page, setPage] = useState<Page>();
  const [cursor, setCursor] = useState("");
  const [inputs, setInputs] = useState("{}");
  const [repositories, setRepositories] = useState("{}");
  const [preview, setPreview] = useState("");
  const [impact, setImpact] = useState<Impact>();
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState("");
  const [executions, setExecutions] = useState<{
    items: Record<{ state: string }>[];
    next: string;
  }>();
  const operations = useRef(new Map<string, string>());
  const request = async (path: string, body?: unknown) => {
    const namespace = { api, user, workspace };
    const epoch = answerScopeEpoch(namespace);
    const result = await syncRequest(base + path, native, {
      method: body === undefined ? "GET" : "POST",
      body,
    });
    if (!allowAnswerScope(namespace, epoch))
      throw new SyncHTTPError(403, undefined);
    return result;
  };
  const act = async (work: () => Promise<void>) => {
    setBusy(true);
    setNotice("");
    try {
      await work();
    } catch (error) {
      setNotice(
        "操作を完了できませんでした。現在の版・権限・入力を確認してください。",
      );
      if (
        error instanceof SyncHTTPError &&
        (error.status === 401 || error.status === 403)
      ) {
        setRecord(undefined);
        setGraph(undefined);
        setPage(undefined);
        setPreview("");
        setImpact(undefined);
        setExecutions(undefined);
        if (await workspaceAccessLost(native, workspace, error))
          await syncStorage(native).remove({ api, user, workspace });
      }
    } finally {
      setBusy(false);
    }
  };
  const ref = (r: Ref) => ({ id: r.id, version: r.version });
  const mutate = async (path: string, version: string, body: object) => {
    const key = JSON.stringify({ path, version, body });
    if (!operations.current.has(key))
      operations.current.set(key, crypto.randomUUID());
    return (await request(path, {
      ...body,
      operation_id: operations.current.get(key),
      expected_version: version,
    })) as { data: Ref };
  };
  const load = async (target: string) => {
    const value = (await request(
      `/records/${encodeURIComponent(target)}?version=head`,
    )) as Record<Execution>;
    const def = (await request(
      `/records/${encodeURIComponent(value.value.definition.id)}?version=${encodeURIComponent(value.value.definition.version)}`,
    )) as Record<Graph>;
    const tasks = (await request("/query", {
      kind: "tasks",
      id: value.id,
      version: value.version,
      limit: 50,
    })) as { data: Page };
    setRecord(value);
    setID(value.id);
    setGraph(def.value);
    setPage(tasks.data);
    setCursor(tasks.data.next);
    setImpact(undefined);
    setPreview("");
  };
  const adopted = async () => {
    if (!definition) throw new Error("definition_required");
    return (await request(
      `/records/${encodeURIComponent("adoption:" + definition.id)}?version=head`,
    )) as Ref;
  };
  useEffect(() => {
    if (selected) void act(() => load(selected.id));
  }, [selected?.id, selected?.version]);
  return (
    <ScreenContent>
      {
        <section aria-label="工程の実行と判断">
          <h3>採用した工程の実行</h3>
          <p>
            実行開始は別の依頼です。工程の判断待ちと、Run途中の質問は別々に表示します。時間・費用の予算は追加開始を止める条件で、開始済みのCLIによる消費の上限は保証しません。
          </p>
          <fieldset disabled={busy}>
            <label>
              工程実行ID
              <input value={id} onChange={(e) => setID(e.target.value)} />
            </label>
            <button disabled={!id} onClick={() => void act(() => load(id))}>
              工程実行を取得・更新
            </button>
            <button
              onClick={() =>
                void act(async () => {
                  const v = (await request("/query", {
                    kind: "heads",
                    type: "execution",
                    limit: 50,
                  })) as { data: typeof executions };
                  setExecutions(v.data);
                })
              }
            >
              工程実行の一覧
            </button>
            {executions?.items.map((r) => (
              <button key={r.id} onClick={() => void act(() => load(r.id))}>
                {r.id}: {r.value.state}
              </button>
            ))}
            {executions?.next && (
              <button
                onClick={() =>
                  void act(async () => {
                    const v = (await request("/query", {
                      kind: "heads",
                      type: "execution",
                      after: executions.next,
                      limit: 50,
                    })) as { data: typeof executions };
                    setExecutions(v.data);
                  })
                }
              >
                工程実行の次のページ
              </button>
            )}
            {canExecute &&
              definition &&
              definition.id.startsWith("workflow-definition:") && (
                <details>
                  <summary>この定義の採用版から新しく開始する</summary>
                  <label>
                    明示する開始入力（node → 入力名 → JSON値）
                    <textarea
                      value={inputs}
                      onChange={(e) => setInputs(e.target.value)}
                    />
                  </label>
                  <label>
                    Runごとのリポジトリと固定commit（node → id・commit）
                    <textarea
                      value={repositories}
                      onChange={(e) => setRepositories(e.target.value)}
                    />
                  </label>
                  <button
                    onClick={() =>
                      void act(async () => {
                        const adoption = await adopted();
                        const target = `workflow-execution:${crypto.randomUUID()}`;
                        const result = await mutate(
                          `/executions/${encodeURIComponent(target)}/start`,
                          "0",
                          {
                            adoption: ref(adoption),
                            inputs_utf8: inputs,
                            repositories: JSON.parse(repositories),
                          },
                        );
                        await load(result.data.id);
                        setNotice("採用した固定版の実行開始を依頼しました。");
                      })
                    }
                  >
                    採用版から新しい工程実行の開始を依頼
                  </button>
                </details>
              )}
            {record && (
              <>
                <p>
                  工程: {record.value.state} / 開始したRun: {record.value.count}{" "}
                  / 固定定義: {record.value.definition.id}@
                  {record.value.definition.version}
                </p>
                {record.value.reason && (
                  <p>待機・停止理由: {record.value.reason}</p>
                )}
                {record.value.cost && (
                  <p>
                    費用観測: {record.value.cost.state} /{" "}
                    {record.value.cost.amount ?? "未観測"}{" "}
                    {record.value.cost.currency} / 制御: 追加開始のみ
                  </p>
                )}
                {page?.items.map((r) => (
                  <article key={r.id}>
                    <p>
                      {r.value.node}（{r.value.attempt}回目）: {r.value.state}{" "}
                      {r.value.outcome ?? ""}
                      {r.value.stale
                        ? "（採用変更により、この試行の判定は失効）"
                        : ""}
                    </p>
                    {r.value.run && (
                      <p>
                        {onOpenRun ? (
                          <button onClick={() => onOpenRun(r.value.run!)}>
                            この工程の入力・質問・結果を確認
                          </button>
                        ) : (
                          <>
                            Run: {r.value.run.id}@{r.value.run.version}
                            （Run一覧から入力・Pause・原出力を確認できます）
                          </>
                        )}
                      </p>
                    )}
                    {r.value.definition && (
                      <button
                        onClick={() =>
                          void act(async () => {
                            const p = (await request(
                              `/executions/${encodeURIComponent(record.id)}/preview`,
                              {
                                execution: ref(record),
                                node: r.value.node,
                                format: "source",
                              },
                            )) as { input_utf8: string; sources: Ref[] };
                            setPreview(
                              p.input_utf8 +
                                "\n\n固定参照:\n" +
                                JSON.stringify(p.sources, null, 2),
                            );
                          })
                        }
                      >
                        このRunの入力をプレビュー
                      </button>
                    )}
                    {canEdit &&
                      r.value.state === "decision" &&
                      graph?.nodes
                        .find((n) => n.id === r.value.node)
                        ?.choices?.map((choice) => (
                          <button
                            key={choice}
                            onClick={() =>
                              void act(async () => {
                                await mutate(
                                  `/executions/${encodeURIComponent(record.id)}/decide`,
                                  record.version,
                                  {
                                    node: r.value.node,
                                    attempt: r.value.attempt,
                                    choice,
                                  },
                                );
                                await load(record.id);
                                setNotice(
                                  "判断を記録しました。回答だけでは実行権限を付与しません。",
                                );
                              })
                            }
                          >
                            {choice}を回答
                          </button>
                        ))}
                  </article>
                ))}
                {cursor && (
                  <button
                    onClick={() =>
                      void act(async () => {
                        const p = (await request("/query", {
                          kind: "tasks",
                          id: record.id,
                          version: record.version,
                          after: cursor,
                          limit: 50,
                        })) as { data: Page };
                        setPage(p.data);
                        setCursor(p.data.next);
                      })
                    }
                  >
                    工程内の次のページ
                  </button>
                )}
                {canEdit && canExecute && definition && (
                  <button
                    onClick={() =>
                      void act(async () => {
                        setImpact(
                          (await request(
                            `/executions/${encodeURIComponent(record.id)}/impact`,
                            {
                              execution: ref(record),
                              adoption: ref(await adopted()),
                            },
                          )) as Impact,
                        );
                      })
                    }
                  >
                    現在の採用版を適用した場合の影響を確認
                  </button>
                )}
                {impact && (
                  <div>
                    <p>
                      影響する未開始ノード:{" "}
                      {impact.affected_unstarted.join(", ") || "なし"}
                    </p>
                    <p>
                      失効する判断:{" "}
                      {impact.invalidated_decisions.join(", ") || "なし"}
                    </p>
                    <button
                      onClick={() =>
                        void act(async () => {
                          await mutate(
                            `/executions/${encodeURIComponent(record.id)}/apply`,
                            record.version,
                            { impact },
                          );
                          await load(record.id);
                          setNotice(
                            "影響を確認した採用版を適用しました。開始済みの入力と過去の判断は履歴に残っています。",
                          );
                        })
                      }
                    >
                      確認した採用版を明示適用
                    </button>
                  </div>
                )}
                {native ? (
                  <button
                    onClick={() =>
                      void act(async () => {
                        const value = await invoke<{
                          complete: boolean;
                          path: string;
                        }>("export_workflow", {
                          workspace,
                          id: record.id,
                          version: record.version,
                        });
                        setNotice(
                          `${value.complete ? "記録を保存しました" : "取得が完了していません"}: ${value.path}`,
                        );
                      })
                    }
                  >
                    この実行版の記録をファイルへ保存
                  </button>
                ) : (
                  <a
                    download
                    href={`${base}/executions/${encodeURIComponent(record.id)}/export?version=${encodeURIComponent(record.version)}`}
                  >
                    この実行版の記録をファイルへ保存
                  </a>
                )}
              </>
            )}
          </fieldset>
          {preview && (
            <details open>
              <summary>選択したRunの入力</summary>
              <pre>{preview}</pre>
            </details>
          )}
          {notice && <p role="status">{notice}</p>}
        </section>
      }
    </ScreenContent>
  );
}
