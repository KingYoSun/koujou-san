import { expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { WorkflowMap, nodeDepths, type WorkflowGraph } from "./workflow-map";
import { editField, fieldSource } from "./workflow-json";

const first = {
  id: "実装",
  definition: {
    id: "run-definition:implementation",
    version: "90071992547409931234",
  },
};
const graphs: WorkflowGraph[] = [
  {
    name: "直列",
    nodes: [
      first,
      {
        id: "検品",
        depends: [{ node: "実装", on: ["completed"], required: true }],
      },
    ],
  },
  {
    name: "並列と合流",
    nodes: [
      first,
      { id: "別検品" },
      {
        id: "両方",
        join: "all",
        depends: [
          { node: "実装", on: ["PASS"] },
          { node: "別検品", on: ["PASS"] },
        ],
      },
      {
        id: "いずれか",
        join: "any",
        depends: [{ node: "実装", on: ["FAIL"] }],
      },
    ],
  },
  {
    name: "条件分岐",
    nodes: [
      first,
      {
        id: "人の判断",
        choices: ["承認", "差し戻し"],
        depends: [{ node: "実装", on: ["PASS"] }],
      },
    ],
  },
  {
    name: "有限の差し戻し",
    budget: { max_runs: 8 },
    nodes: [
      first,
      {
        id: "再検品",
        retry: { FAIL: "実装", STALE: "実装" },
        bindings: [
          {
            from: "実装",
            kind: "output",
            channel: "stdout",
            index: 1,
            path: ["results", "0"],
            to: "prompt",
          },
        ],
      },
    ],
  },
];
it("shows serial, parallel joins, branches and finite returns from the fixed JSON relationships", () => {
  const render = (graph: WorkflowGraph) =>
    renderToStaticMarkup(
      <WorkflowMap
        graph={graph}
        selected={0}
        onSelect={() => {}}
        loadLabels={async () => []}
      />,
    );
  const serial = render(graphs[0]);
  expect(serial).toContain("実装");
  expect(serial).toContain("検品");
  expect(serial).toContain("completed");
  const parallel = render(graphs[1]);
  expect(parallel).toContain("合流 all");
  expect(parallel).not.toContain("合流 any");
  expect(parallel).toContain("PASS · 任意");
  const branch = render(graphs[2]);
  expect(branch).toContain("人の判断");
  const returned = render(graphs[3]);
  expect(returned).toContain("↩ FAIL / STALE");
  expect(serial).not.toContain(first.definition.version);
});
it("renders only a page of a large definition and identifies a connection outside the page", () => {
  const graph = {
    nodes: Array.from({ length: 10000 }, (_, i) => ({
      id: `工程-${i}`,
      depends: i === 0 ? [{ node: "工程-9999", on: ["PASS"] }] : [],
    })),
  };
  const html = renderToStaticMarkup(
    <WorkflowMap
      graph={graph}
      selected={0}
      onSelect={() => {}}
      loadLabels={async () => []}
    />,
  );
  expect((html.match(/aria-pressed=/g) ?? []).length).toBe(50);
  expect(html).toContain("別ページ");
  expect(html).toContain('data-stub="true"');
  expect(html).toContain("10000");
  expect(html).not.toContain("工程-100</button>");
});
it("places nodes in columns by control dependency depth and tolerates long chains and cycles", () => {
  expect(nodeDepths(graphs[1].nodes)).toEqual([0, 0, 1, 1]);
  expect(nodeDepths(graphs[3].nodes)).toEqual([0, 0]);
  const chain = Array.from({ length: 10000 }, (_, i) => ({
    id: `工程-${i}`,
    depends: i ? [{ node: `工程-${i - 1}`, on: ["PASS"] }] : [],
  }));
  expect(nodeDepths(chain)[9999]).toBe(9999);
  expect(
    nodeDepths([
      { id: "a", depends: [{ node: "b", on: ["PASS"] }] },
      { id: "b", depends: [{ node: "a", on: ["PASS"] }] },
      { id: "c", depends: [{ node: "a", on: ["PASS"] }] },
    ]),
  ).toEqual([0, 1, 1]);
  const html = renderToStaticMarkup(
    <WorkflowMap
      graph={graphs[1]}
      selected={2}
      onSelect={() => {}}
      loadLabels={async () => []}
    />,
  );
  expect(html).toContain('style="grid-column:1;grid-row:2"');
  expect(html).toContain('style="grid-column:2;grid-row:2"');
  expect(html).toContain('id="workflow-node-settings"');
  expect(html).toContain("<h4>両方</h4>");
});
it("round-trips structured connection edits without rewriting unknown values or fixed references", () => {
  let text =
    '{"schema":"workflow-definition-v1","name":"接続","unknown":123456789012345678901234567890,"nodes":[{"id":"実装","definition":{"id":"run-definition:implementation","version":"90071992547409931234"},"extension":90071992547409931234},{"id":"検品","join":"all","depends":[{"node":"実装","on":["completed"],"required":true,"unknown":90071992547409931234}],"bindings":[],"retry":{"FAIL":"実装"}}],"budget":{"parallel":1,"max_runs":8}}';
  const fixed = fieldSource(text, ["nodes", 0]);
  text = editField(text, ["nodes", 1, "depends", 0, "on", 0], '"PASS"');
  text = editField(text, ["nodes", 1, "join"], '"any"');
  text = editField(
    text,
    ["nodes", 1, "bindings", 0],
    '{"from":"実装","kind":"output","channel":"stdout","index":0,"path":["items","1"],"to":"prompt"}',
  );
  text = editField(text, ["nodes", 1, "retry", "STALE"], '"実装"');
  text = editField(text, ["nodes", 2], '{"id":"最終判断","choices":["承認"]}');
  expect(fieldSource(text, ["nodes", 0]).replace(/\s/g, "")).toBe(
    fixed.replace(/\s/g, ""),
  );
  expect(fieldSource(text, ["unknown"])).toBe("123456789012345678901234567890");
  expect(fieldSource(text, ["nodes", 1, "depends", 0, "unknown"])).toBe(
    "90071992547409931234",
  );
  expect(JSON.parse(text).nodes[1]).toMatchObject({
    join: "any",
    depends: [{ node: "実装", on: ["PASS"] }],
    bindings: [{ path: ["items", "1"], to: "prompt" }],
    retry: { FAIL: "実装", STALE: "実装" },
  });
});
