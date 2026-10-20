import { expect, test } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import {
  WorkflowProgress,
  workflowFocus,
  taskResult,
  type WorkflowTask,
} from "./workflow-progress";
import type { WorkflowGraph } from "./workflow-map";

const graph: WorkflowGraph = {
  nodes: [
    { id: "実装" },
    { id: "検品", retry: { FAIL: "実装" } },
    { id: "承認", choices: ["承認", "差し戻し"] },
  ],
};
const task = (
  node: string,
  attempt: number,
  outcome?: string,
): WorkflowTask => ({
  node,
  attempt,
  state: "done",
  outcome,
  run: { id: `run:${node}-${attempt}`, version: `fixed-${attempt}` },
});

test("the problem, correction and reinspection lead directly to their fixed evidence without exposing all task metadata", () => {
  const tasks = [
    task("実装", 1, "succeeded"),
    task("検品", 1, "FAIL"),
    task("実装", 2, "succeeded"),
    task("検品", 2, "PASS"),
  ];
  expect(workflowFocus(graph, tasks)).toBe(1);
  const html = renderToStaticMarkup(
    <WorkflowProgress
      graph={graph}
      tasks={tasks}
      workspace="one"
      onDetails={() => {}}
    />,
  );
  for (const text of [
    "以前の問題",
    "不合格（FAIL）",
    "差し戻し先",
    "戻り先の修正",
    "直近の再検品",
    "合格（PASS）",
  ])
    expect(html).toContain(text);
  expect(html).toContain("id=run%3A%E6%A4%9C%E5%93%81-2&amp;version=fixed-2");
  expect(html).toContain("view=evidence");
  expect(html).not.toContain("view=questions");
});
test("a current human decision takes precedence over an older failure", () => {
  const waiting = { ...task("承認", 1), state: "decision" };
  expect(workflowFocus(graph, [task("検品", 1, "FAIL"), waiting])).toBe(2);
  const html = renderToStaticMarkup(
    <WorkflowProgress
      graph={graph}
      tasks={[waiting]}
      workspace="one"
      onDetails={() => {}}
    />,
  );
  expect(html).toContain("承認への回答が必要です");
  expect(html).toContain("判断の内容を確認して回答する");
});
test("an expired PASS is not presented as a usable passing result", () => {
  expect(taskResult({ ...task("検品", 1, "PASS"), stale: true })).toBe(
    "判定は失効（STALE）",
  );
});
test("a prepared Run without a fixed version is not offered as evidence", () => {
  const pending = {
    ...task("実装", 1),
    state: "prepared",
    run: { id: "run:pending", version: "0" },
  };
  const html = renderToStaticMarkup(
    <WorkflowProgress
      graph={graph}
      tasks={[pending]}
      workspace="one"
      onDetails={() => {}}
    />,
  );
  expect(html).toContain("起動未確認");
  expect(html).not.toContain("<a ");
});

test("reinspection returning to the same node is shown once as the new result", () => {
  const graph: WorkflowGraph = {
    nodes: [{ id: "検品", retry: { STALE: "検品" } }],
  };
  const old = { ...task("検品", 1, "STALE"), stale: true };
  const html = renderToStaticMarkup(
    <WorkflowProgress
      graph={graph}
      tasks={[old, task("検品", 2, "PASS")]}
      workspace="one"
      onDetails={() => {}}
    />,
  );
  expect(html).toContain("直近の再検品");
  expect(html).not.toContain("戻り先の修正");
  expect(html.match(/<a /g)).toHaveLength(2);
});
