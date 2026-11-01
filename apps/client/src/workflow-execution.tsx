import { ScreenContent, NavigationLink, useScreenActive } from "./navigation";
import {
  WorkflowMap,
  type WorkflowGraph,
  type DefinitionLabel,
  fixedWorkflowRef,
} from "./workflow-map";
import { StatusBadge } from "./status-badge";
import { DefinitionPicker, type DefinitionPage } from "./definition-picker";
import {
  WorkflowProgress,
  workflowFocus,
  type WorkflowTask,
} from "./workflow-progress";
import { useEffect, useRef, useState, type ReactNode } from "react";
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
type Task = WorkflowTask;
type Graph = WorkflowGraph;
type Page = { items: Record<Task>[]; next: string };
type Impact = {
  execution: Ref;
  adoption: Ref;
  affected_unstarted: string[];
  invalidated_decisions: string[];
};
const taskStates: { [state: string]: string } = {
  pending: "開始条件待ち",
  prepared: "実行準備中",
  running: "Runの報告待ち",
  decision: "人の判断待ち",
  done: "試行完了",
  skipped: "条件不成立で省略",
};
const executionStates: { [state: string]: string } = {
  running: "工程進行中",
  waiting: "待機",
  completed: "工程完了",
  stopped: "追加開始を停止",
};
const waitReasons: { [reason: string]: string } = {
  human_decision: "人の判断待ち",
  run_budget: "Run数の上限に到達",
  time_budget_no_further_start: "工程の制御時間に到達",
};
const runOutcomes: { [state: string]: string } = {
  succeeded: "正常終了",
  failed: "実行失敗",
  cancelled: "取消",
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
  onSelect,
  render,
  visible = true,
}: {
  native: boolean;
  api: string;
  user: string;
  workspace: string;
  definition?: Ref;
  canEdit: boolean;
  canExecute: boolean;
  selected?: Ref;
  onSelect?: (ref: Ref) => void;
  render?: (content: ReactNode) => ReactNode;
  visible?: boolean;
}) {
  const active = useScreenActive();
  const owner = JSON.stringify([
    selected?.id,
    selected?.version,
    definition?.id,
    definition?.version,
  ]);
  const ownerRef = useRef(owner);
  ownerRef.current = owner;
  const activeRef = useRef(active);
  activeRef.current = active;
  const running = useRef(0);
  const loadEpoch = useRef(0);
  const [nodeIndex, setNodeIndex] = useState(0);
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
  const [impactDefinition, setImpactDefinition] = useState<Ref>();
  const [candidate, setCandidate] = useState<{
    owner: string;
    ref: Ref;
    name?: string;
  }>();
  const [busy, setBusy] = useState(false);
  const [notification, setNotification] = useState<{
    owner: string;
    text: string;
  }>();
  const notice = notification?.owner === owner ? notification.text : "";
  const setNotice = (text: string) => {
    if (ownerRef.current === owner) setNotification({ owner, text });
  };
  const [requestedExecution, setRequestedExecution] = useState<{
    owner: string;
    ref: Ref;
  }>();
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
    running.current++;
    setBusy(true);
    setNotice("");
    try {
      await work();
    } catch (error) {
      if (ownerRef.current !== owner) return;
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
        setRequestedExecution(undefined);
        setExecutions(undefined);
        if (await workspaceAccessLost(native, workspace, error))
          await syncStorage(native).remove({ api, user, workspace });
      }
    } finally {
      running.current--;
      setBusy(running.current > 0);
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
  const load = async (target: string, version = "head") => {
    if (owner !== ownerRef.current) return;
    const epoch = ++loadEpoch.current;
    const value = (await request(
      `/records/${encodeURIComponent(target)}?version=${encodeURIComponent(version)}`,
    )) as Record<Execution>;
    if (onSelect && !selected) {
      if (
        epoch === loadEpoch.current &&
        owner === ownerRef.current &&
        activeRef.current
      )
        onSelect(ref(value));
      return;
    }
    const def = (await request(
      `/records/${encodeURIComponent(value.value.definition.id)}?version=${encodeURIComponent(value.value.definition.version)}`,
    )) as Record<Graph>;
    const tasks = (await request("/query", {
      kind: "tasks",
      id: value.id,
      version: value.version,
      limit: 50,
    })) as { data: Page };
    if (epoch !== loadEpoch.current || owner !== ownerRef.current) return;
    setRecord(value);
    setID(value.id);
    setGraph(def.value);
    setNodeIndex(
      workflowFocus(
        def.value,
        tasks.data.items.map((t) => t.value),
      ),
    );
    setPage(tasks.data);
    setCursor(tasks.data.next);
    setImpact(undefined);
    setImpactDefinition(undefined);
    setPreview("");
    if (version === "head" && activeRef.current) onSelect?.(ref(value));
  };
  const adopted = async () => {
    const target =
      candidate && record && candidate.owner === record.id
        ? candidate.ref
        : (definition ?? record?.value.definition);
    if (!target) throw new Error("definition_required");
    return (await request(
      `/records/${encodeURIComponent("adoption:" + target.id)}?version=head`,
    )) as Record<{ definition: Ref }>;
  };
  useEffect(() => {
    if (
      active &&
      visible &&
      selected &&
      (record?.id !== selected.id || record?.version !== selected.version)
    ) {
      setRecord(undefined);
      setPage(undefined);
      setGraph(undefined);
      void act(() => load(selected.id, selected.version));
    }
  }, [active, visible, selected?.id, selected?.version]);
  const content = (
    <ScreenContent>
      {
        <section aria-label="工程の実行と判断">
          {!selected && <h3>工程の状況</h3>}
          <fieldset disabled={busy}>
            {!selected && (
              <details>
                <summary>工程実行を探す・IDで開く</summary>
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
              </details>
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
                        setRequestedExecution({ owner, ref: ref(result.data) });
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
                {!selected && (
                  <p>
                    工程：
                    {executionStates[record.value.state] ?? record.value.state}
                  </p>
                )}
                {record.value.reason && (
                  <p>
                    待機・停止理由:{" "}
                    {waitReasons[record.value.reason] ?? record.value.reason}
                  </p>
                )}
                {graph && (
                  <WorkflowProgress
                    graph={graph}
                    tasks={page?.items.map((t) => t.value) ?? []}
                    workspace={workspace}
                    onDetails={(index) => {
                      setNodeIndex(index);
                      requestAnimationFrame(() =>
                        document
                          .getElementById("workflow-node-settings")
                          ?.focus(),
                      );
                    }}
                  />
                )}
                {cursor && <p role="status">未取得の試行があります。</p>}
                <section aria-label="工程図・全試行・入力の接続">
                  {record.value.cost && (
                    <p>
                      費用観測: {record.value.cost.state} /{" "}
                      {record.value.cost.amount ?? "未観測"}{" "}
                      {record.value.cost.currency}
                    </p>
                  )}
                  {graph && (
                    <WorkflowMap
                      graph={graph}
                      selected={nodeIndex}
                      onSelect={setNodeIndex}
                      loadLabels={async (refs) =>
                        (await request("/labels", refs)) as DefinitionLabel[]
                      }
                      status={(node) => {
                        const tasks =
                          page?.items.filter((t) => t.value.node === node.id) ??
                          [];
                        return tasks.length ? (
                          tasks.map((t) => (
                            <p key={t.id}>
                              <StatusBadge
                                label={`試行 ${t.value.attempt}`}
                                value={
                                  taskStates[t.value.state] ?? t.value.state
                                }
                              />
                              <StatusBadge
                                label={
                                  node.choices?.length
                                    ? "人の選択"
                                    : [
                                          "PASS",
                                          "FAIL",
                                          "INCONCLUSIVE",
                                          "STALE",
                                        ].includes(t.value.outcome ?? "")
                                      ? "判定"
                                      : "実行結果"
                                }
                                value={
                                  t.value.outcome
                                    ? (runOutcomes[t.value.outcome] ??
                                      t.value.outcome)
                                    : "未確認"
                                }
                              />
                              {t.value.stale && (
                                <StatusBadge
                                  label="有効性"
                                  value="STALE（失効）"
                                  tone="warning"
                                />
                              )}
                            </p>
                          ))
                        ) : (
                          <p>試行未取得</p>
                        );
                      }}
                    >
                      {(node) => (
                        <>
                          <h5>入力の接続</h5>
                          {node.bindings?.length ? (
                            <ul>
                              {node.bindings.map((b, i) => (
                                <li key={i}>
                                  {b.from} の {b.kind} /{" "}
                                  {b.channel ?? "派生結果"}[{b.index ?? 0}] /{" "}
                                  {b.path?.join(" → ") || "全体"} → {node.id} の{" "}
                                  {b.to}
                                  {b.to === "command"
                                    ? ` / ${b.parameter?.join(" → ") || "全体"}`
                                    : ""}
                                </li>
                              ))}
                            </ul>
                          ) : null}
                          {page?.items
                            .filter(
                              (r) =>
                                r.value.node === graph?.nodes[nodeIndex]?.id,
                            )
                            .map((r) => (
                              <article key={r.id}>
                                <p>
                                  {r.value.node}（{r.value.attempt}回目）:{" "}
                                  {taskStates[r.value.state] ?? r.value.state}{" "}
                                  {r.value.outcome ?? ""}
                                  {r.value.stale
                                    ? "（採用変更により、この試行の判定は失効）"
                                    : ""}
                                </p>
                                {r.value.definition && (
                                  <p>
                                    この試行で使用したRun定義の固定版：
                                    {r.value.definition.id}@
                                    {r.value.definition.version}
                                  </p>
                                )}
                                {r.value.run &&
                                  fixedWorkflowRef(r.value.run) && (
                                    <p>
                                      <NavigationLink
                                        to={{
                                          workspace,
                                          screen: "work",
                                          target: {
                                            ...r.value.run,
                                            kind: "run",
                                            name: `${r.value.node}（${r.value.attempt}回目）`,
                                          },
                                          view: "evidence",
                                        }}
                                        related
                                      >
                                        この試行の結果・証拠を確認
                                      </NavigationLink>
                                    </p>
                                  )}
                                {r.value.run &&
                                  !fixedWorkflowRef(r.value.run) && (
                                    <p>
                                      Runの固定版はまだ未確定です。共有記録・起動の確認後に詳細を開けます。
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
                                        )) as {
                                          input_utf8: string;
                                          sources: Ref[];
                                        };
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
                        </>
                      )}
                    </WorkflowMap>
                  )}
                  {cursor && (
                    <button
                      onClick={() =>
                        void act(async () => {
                          const epoch = loadEpoch.current;
                          const p = (await request("/query", {
                            kind: "tasks",
                            id: record.id,
                            version: record.version,
                            after: cursor,
                            limit: 50,
                          })) as { data: Page };
                          if (epoch === loadEpoch.current) {
                            setPage(p.data);
                            setCursor(p.data.next);
                          }
                        })
                      }
                    >
                      工程内の次のページ
                    </button>
                  )}
                </section>
                <details>
                  <summary>定義の変更・固定版・記録の保存</summary>
                  <p>
                    Run数 {record.value.count} · 定義{" "}
                    {record.value.definition.id}@
                    {record.value.definition.version}
                  </p>
                  {canEdit && canExecute && (
                    <section aria-label="採用版の適用">
                      <DefinitionPicker
                        label="適用候補の工程定義"
                        load={async (after) =>
                          (
                            (await request("/query", {
                              kind: "heads",
                              type: "workflow-definition",
                              after,
                              limit: 50,
                            })) as { data: DefinitionPage }
                          ).data
                        }
                        onChoose={(ref, name) => {
                          setCandidate({ owner: record.id, ref, name });
                          setImpact(undefined);
                          setImpactDefinition(undefined);
                        }}
                      />
                      <p>
                        候補の定義：
                        {candidate?.owner === record.id
                          ? candidate.name || candidate.ref.id
                          : graph?.name || record.value.definition.id}
                      </p>
                      <button
                        onClick={() =>
                          void act(async () => {
                            const epoch = loadEpoch.current;
                            const adoption = await adopted();
                            const value = (await request(
                              `/executions/${encodeURIComponent(record.id)}/impact`,
                              {
                                execution: ref(record),
                                adoption: ref(adoption),
                              },
                            )) as Impact;
                            if (epoch === loadEpoch.current) {
                              setImpact(value);
                              setImpactDefinition(adoption.value.definition);
                            }
                          })
                        }
                      >
                        現在の採用版を適用した場合の影響を確認
                      </button>
                    </section>
                  )}
                  {impact && (
                    <div>
                      {impactDefinition && (
                        <p>
                          適用する定義の固定版：
                          <NavigationLink
                            to={{
                              workspace,
                              screen: "workflows",
                              target: {
                                ...impactDefinition,
                                kind: "workflow-definition",
                              },
                              view: "edit",
                            }}
                            related
                          >
                            {impactDefinition.id}@{impactDefinition.version}
                          </NavigationLink>
                        </p>
                      )}
                      <p>
                        採用の固定参照：{impact.adoption.id}@
                        {impact.adoption.version}
                      </p>
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
                </details>
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
          {requestedExecution?.owner === owner && (
            <NavigationLink
              to={{
                workspace,
                screen: "work",
                target: { ...requestedExecution.ref, kind: "execution" },
                view: "overview",
              }}
              related
            >
              依頼した工程実行を確認
            </NavigationLink>
          )}
        </section>
      }
    </ScreenContent>
  );
  return render ? render(content) : content;
}
