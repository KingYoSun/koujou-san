import { NavigationLink } from "./navigation";
import { fixedWorkflowRef, type WorkflowGraph } from "./workflow-map";

export type WorkflowTask = {
  node: string;
  attempt: number;
  state: string;
  outcome?: string;
  run?: { id: string; version: string };
  definition?: { id: string; version: string };
  stale?: boolean;
};

export const taskResult = (task: WorkflowTask) =>
  task.stale
    ? "判定は失効（STALE）"
    : ({
        PASS: "合格（PASS）",
        FAIL: "不合格（FAIL）",
        INCONCLUSIVE: "判断不能（INCONCLUSIVE）",
        STALE: "判定は失効（STALE）",
        succeeded: "正常終了",
        failed: "実行失敗",
        cancelled: "取消",
      }[task.outcome ?? ""] ??
      task.outcome ??
      {
        pending: "開始条件待ち",
        prepared: "実行準備中・起動未確認",
        running: "Runの報告待ち",
        decision: "判断への回答待ち",
        done: "試行完了・結果未確認",
        skipped: "条件不成立で省略",
      }[task.state] ??
      task.state);

const problem = (task: WorkflowTask) =>
  task.stale ||
  ["FAIL", "INCONCLUSIVE", "STALE", "failed"].includes(task.outcome ?? "");

export function workflowFocus(graph: WorkflowGraph, tasks: WorkflowTask[]) {
  const latest = graph.nodes.map(
    (node) =>
      tasks
        .filter((task) => task.node === node.id)
        .sort((a, b) => b.attempt - a.attempt)[0],
  );
  const focus =
    latest.find((task) => task?.state === "decision" && !task.stale) ??
    latest.find((task) => task && problem(task)) ??
    tasks.find(problem) ??
    latest.find(
      (task) => task && ["running", "prepared"].includes(task.state),
    ) ??
    [...latest].reverse().find((task) => task?.outcome === "PASS") ??
    latest.find(Boolean);
  return Math.max(
    0,
    graph.nodes.findIndex((node) => node.id === focus?.node),
  );
}

export function WorkflowProgress({
  graph,
  tasks,
  workspace,
  onDetails,
}: {
  graph: WorkflowGraph;
  tasks: WorkflowTask[];
  workspace: string;
  onDetails: (index: number) => void;
}) {
  const index = workflowFocus(graph, tasks);
  const node = graph.nodes[index];
  const trials = tasks
    .filter((task) => task.node === node?.id)
    .sort((a, b) => a.attempt - b.attempt);
  const issue = trials.find(problem);
  const current = trials.at(-1);
  const back = issue?.outcome && node?.retry?.[issue.outcome];
  const correction =
    back &&
    tasks
      .filter(
        (task) => task.node === back && task.attempt > (issue?.attempt ?? 0),
      )
      .sort((a, b) => b.attempt - a.attempt)[0];
  const returned = tasks
    .filter(
      (task) =>
        graph.nodes.find((node) => node.id === task.node)?.retry?.[
          task.outcome ?? ""
        ],
    )
    .sort((a, b) => b.attempt - a.attempt)[0];
  const result = (label: string, task: WorkflowTask) => (
    <li key={label}>
      <strong>{label}：</strong>{" "}
      {task.run && fixedWorkflowRef(task.run) ? (
        <NavigationLink
          to={{
            workspace,
            screen: "work",
            target: {
              ...task.run,
              kind: "run",
              name: `${task.node}（${task.attempt}回目）`,
            },
            view: "evidence",
          }}
          related
        >
          {task.node}（{task.attempt}回目）：{taskResult(task)} —
          結果・証拠を見る
        </NavigationLink>
      ) : (
        <span>
          {task.node}（{task.attempt}回目）：{taskResult(task)}
        </span>
      )}
    </li>
  );
  return (
    <section className="workflow-progress" aria-label="問題・修正・再検品">
      <h4>
        {current?.state === "decision"
          ? `${current.node}への回答が必要です`
          : issue
            ? `${node?.id}の問題と、その後の結果`
            : "いま確認する結果"}
      </h4>
      {current ? (
        <>
          <ol>
            {issue &&
              result(issue === current ? "現在の問題" : "以前の問題", issue)}
            {back && (
              <li>
                <strong>差し戻し先：</strong> {issue?.outcome}のとき、{node?.id}
                から{back}へ戻る条件です。
              </li>
            )}
            {correction &&
              correction !== current &&
              result("戻り先の修正", correction)}
            {current !== issue &&
              result(
                issue &&
                  ["PASS", "FAIL", "INCONCLUSIVE", "STALE"].includes(
                    current.outcome ?? "",
                  )
                  ? "直近の再検品"
                  : "現在の試行",
                current,
              )}
          </ol>
          {returned && returned.node !== node?.id && (
            <details>
              <summary>途中の判断を確認</summary>
              <p>
                途中の判断：{returned.node}（{returned.attempt}回目）は「
                {returned.outcome}」。戻り先は
                {
                  graph.nodes.find((node) => node.id === returned.node)
                    ?.retry?.[returned.outcome ?? ""]
                }
                です。
              </p>
            </details>
          )}
        </>
      ) : (
        <p>この工程の試行は表示ページに未取得です。</p>
      )}
      <button data-variant="secondary" onClick={() => onDetails(index)}>
        {current?.state === "decision"
          ? "判断の内容を確認して回答する"
          : "工程の関係・試行を詳しく見る"}
      </button>
    </section>
  );
}
