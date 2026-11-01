import {
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { useScreenActive } from "./navigation";
import { StatusBadge } from "./status-badge";
import { DetailPane } from "./panes";

export type WorkflowRef = { id: string; version: string };
export type WorkflowNode = {
  id: string;
  definition?: WorkflowRef;
  choices?: string[];
  join?: string;
  depends?: { node: string; on: string[]; required?: boolean }[];
  bindings?: {
    from: string;
    kind: string;
    channel?: string;
    index?: number;
    path: string[];
    to: string;
    parameter?: string[];
  }[];
  inputs?: string[];
  retry?: Record<string, string>;
};
export type WorkflowGraph = {
  name?: string;
  nodes: WorkflowNode[];
  budget?: { max_runs?: number };
};
export type DefinitionLabel = WorkflowRef & {
  name?: string;
  kind?: string;
  inputs?: string[];
};
export const referenceKey = (ref: WorkflowRef) =>
  JSON.stringify([ref.id, ref.version]);
export const fixedWorkflowRef = (ref: WorkflowRef) =>
  !!ref.id && !!ref.version && ref.version !== "0" && ref.version !== "head";

// Column of each node: longest chain of existing control dependencies before it.
// A cycle is broken at its first node in definition order; static validation reports it.
export function nodeDepths(nodes: WorkflowNode[]): number[] {
  const index = new Map(nodes.map((n, i) => [n.id, i] as const));
  const out = nodes.map(() => [] as number[]);
  const remaining = nodes.map(() => 0);
  nodes.forEach((n, i) => {
    for (const dep of n.depends ?? []) {
      const j = index.get(dep.node);
      if (j === undefined || j === i) continue;
      out[j].push(i);
      remaining[i]++;
    }
  });
  const depths = nodes.map(() => 0);
  const done = nodes.map(() => false);
  const queue: number[] = [];
  const visit = (i: number) => {
    done[i] = true;
    queue.push(i);
  };
  remaining.forEach((r, i) => !r && visit(i));
  for (let head = 0, next = 0; ; head++) {
    if (head === queue.length) {
      while (next < nodes.length && done[next]) next++;
      if (next === nodes.length) break;
      visit(next);
    }
    for (const j of out[queue[head]]) {
      if (!done[j]) depths[j] = Math.max(depths[j], depths[queue[head]] + 1);
      if (--remaining[j] === 0 && !done[j]) visit(j);
    }
  }
  return depths;
}

type Edge = { from: string; to: string; label: string; back: boolean };
type Box = { x: number; y: number; w: number; h: number };
const PAGE = 50;

export function WorkflowMap({
  graph,
  selected,
  onSelect,
  loadLabels,
  children,
  status,
}: {
  graph: WorkflowGraph;
  selected: number;
  onSelect: (index: number) => void;
  loadLabels: (refs: WorkflowRef[]) => Promise<DefinitionLabel[]>;
  children?: (node: WorkflowNode, label?: DefinitionLabel) => ReactNode;
  status?: (node: WorkflowNode) => ReactNode;
}) {
  const active = useScreenActive();
  const [page, setPage] = useState(() => Math.floor(selected / PAGE));
  const [labels, setLabels] = useState<Record<string, DefinitionLabel>>({});
  const [notice, setNotice] = useState("");
  const canvas = useRef<HTMLDivElement>(null);
  const [boxes, setBoxes] = useState<Record<string, Box>>({});
  const [size, setSize] = useState({ w: 0, h: 0 });
  useEffect(() => {
    setPage(Math.floor(selected / PAGE));
  }, [selected]);
  const start =
    Math.min(page, Math.max(0, Math.ceil(graph.nodes.length / PAGE) - 1)) *
    PAGE;
  const nodes = graph.nodes.slice(start, start + PAGE);
  const onPage = (index: number) => index >= start && index < start + PAGE;
  const depths = nodeDepths(graph.nodes);
  const indexOf = new Map(graph.nodes.map((n, i) => [n.id, i] as const));
  const columns = [...new Set(nodes.map((_, i) => depths[start + i]))];
  const edges: Edge[] = [];
  const stubs = new Map<string, number>();
  nodes.forEach((n) => {
    for (const dep of n.depends ?? []) {
      const j = indexOf.get(dep.node);
      if (j === undefined) continue;
      if (!onPage(j)) stubs.set(dep.node, j);
      edges.push({
        from: dep.node,
        to: n.id,
        label:
          (dep.on?.join(" / ") || "結果条件未指定") +
          (dep.required ? "" : " · 任意"),
        back: false,
      });
    }
    const retries = new Map<string, string[]>();
    for (const [on, target] of Object.entries(n.retry ?? {}))
      retries.set(target, [...(retries.get(target) ?? []), on]);
    for (const [target, on] of retries) {
      const j = indexOf.get(target);
      if (j === undefined) continue;
      if (!onPage(j)) stubs.set(target, j);
      edges.push({
        from: n.id,
        to: target,
        label: `↩ ${on.join(" / ")}`,
        back: true,
      });
    }
  });
  for (const j of stubs.values())
    if (!columns.includes(depths[j])) columns.push(depths[j]);
  columns.sort((a, b) => a - b);
  const rows = new Map<number, number>();
  const place = (depth: number) => {
    const row = (rows.get(depth) ?? 0) + 1;
    rows.set(depth, row);
    return { gridColumn: columns.indexOf(depth) + 1, gridRow: row };
  };
  const refs = Array.from(
    new Map(
      nodes.flatMap((n) =>
        n.definition && fixedWorkflowRef(n.definition)
          ? [[referenceKey(n.definition), n.definition] as const]
          : [],
      ),
    ).values(),
  );
  const signature = JSON.stringify(refs);
  useEffect(() => {
    if (!active || !refs.length) return;
    let current = true;
    setNotice("");
    setLabels({});
    void loadLabels(refs).then(
      (values) => {
        if (current)
          setLabels(
            Object.fromEntries(values.map((v) => [referenceKey(v), v])),
          );
      },
      () => {
        if (current)
          setNotice("定義名を取得できません。固定参照は保持しています。");
      },
    );
    return () => {
      current = false;
    };
  }, [active, signature]);
  useLayoutEffect(() => {
    const element = canvas.current;
    if (!element || typeof ResizeObserver === "undefined") return;
    const measure = () => {
      const next: Record<string, Box> = {};
      element.querySelectorAll<HTMLElement>("[data-node]").forEach((li) => {
        next[li.dataset.node!] = {
          x: li.offsetLeft,
          y: li.offsetTop,
          w: li.offsetWidth,
          h: li.offsetHeight,
        };
      });
      setBoxes(next);
      setSize({ w: element.offsetWidth, h: element.offsetHeight });
    };
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(element);
    return () => observer.disconnect();
  }, [start, graph.nodes.length, edges.length]);
  const choose = (index: number) => {
    onSelect(index);
    requestAnimationFrame(() =>
      document.getElementById("workflow-node-settings")?.focus(),
    );
  };
  const path = (edge: Edge) => {
    const a = boxes[edge.from],
      b = boxes[edge.to];
    if (!a || !b) return undefined;
    if (edge.back) {
      const sx = a.x + a.w / 2,
        sy = a.y + a.h,
        ex = b.x + b.w / 2,
        ey = b.y + b.h;
      return {
        d: `M ${sx} ${sy} C ${sx} ${sy + 60}, ${ex} ${ey + 60}, ${ex} ${ey}`,
        lx: (sx + ex) / 2,
        ly: Math.max(sy, ey) + 48,
      };
    }
    const sx = a.x + a.w,
      sy = a.y + a.h / 2,
      ex = b.x,
      ey = b.y + b.h / 2,
      mx = (sx + ex) / 2;
    return {
      d: `M ${sx} ${sy} C ${mx} ${sy}, ${mx} ${ey}, ${ex} ${ey}`,
      lx: mx,
      ly: (sy + ey) / 2 - 6,
    };
  };
  const describe = (n: WorkflowNode) =>
    n.definition
      ? !fixedWorkflowRef(n.definition)
        ? "Run定義の固定版は未指定"
        : labels[referenceKey(n.definition)]?.kind
          ? labels[referenceKey(n.definition)]?.name || "定義名は未設定"
          : labels[referenceKey(n.definition)]
            ? "定義名を取得できません"
            : "定義名は未取得"
      : n.choices?.length
        ? "人の判断"
        : "実行内容未設定";
  const node = graph.nodes[selected];
  return (
    <section className="workflow-map" aria-label="工程の制御フロー">
      {graph.nodes.length > PAGE && (
        <div className="team-controls">
          <span aria-live="polite">
            {start + 1}–{Math.min(start + PAGE, graph.nodes.length)} /{" "}
            {graph.nodes.length}
          </span>
          <button disabled={!start} onClick={() => setPage(page - 1)}>
            ノードの前ページ
          </button>
          <button
            disabled={start + PAGE >= graph.nodes.length}
            onClick={() => setPage(page + 1)}
          >
            ノードの次ページ
          </button>
        </div>
      )}
      {notice && <p role="status">{notice}</p>}
      <div className="workflow-scroll">
        <div className="workflow-canvas" ref={canvas}>
          <svg
            className="workflow-edges"
            width={size.w}
            height={size.h}
            aria-hidden="true"
          >
            <defs>
              <marker
                id="workflow-arrow"
                viewBox="0 0 10 10"
                refX="9"
                refY="5"
                markerWidth="8"
                markerHeight="8"
                orient="auto-start-reverse"
              >
                <path d="M 0 0 L 10 5 L 0 10 z" />
              </marker>
            </defs>
            {edges.map((edge, i) => {
              // Geometry arrives from the layout effect before first paint.
              const p = path(edge);
              return (
                <g key={i} data-back={edge.back}>
                  {p && <path d={p.d} markerEnd="url(#workflow-arrow)" />}
                  <text x={p?.lx} y={p?.ly} textAnchor="middle">
                    {edge.label}
                  </text>
                </g>
              );
            })}
          </svg>
          <ol className="workflow-node-grid" start={start + 1}>
            {nodes.map((n, offset) => {
              const index = start + offset;
              return (
                <li
                  key={index}
                  data-node={n.id}
                  data-selected={selected === index}
                  style={place(depths[index])}
                >
                  <button
                    id={`workflow-node-${index}`}
                    aria-pressed={selected === index}
                    onClick={() => choose(index)}
                  >
                    {n.id || `識別名未設定（${index + 1}）`}
                  </button>
                  <p>{describe(n)}</p>
                  {(n.depends?.length ?? 0) > 1 && (
                    <small className="workflow-join">
                      合流 {n.join || "未指定"}
                    </small>
                  )}
                  {status?.(n)}
                </li>
              );
            })}
            {[...stubs].map(([id, j]) => (
              <li
                key={`stub:${j}`}
                data-node={id}
                data-stub="true"
                style={place(depths[j])}
              >
                <button
                  data-variant="secondary"
                  title="別ページのノードへ移動"
                  onClick={() => choose(j)}
                >
                  {id}
                </button>
              </li>
            ))}
          </ol>
        </div>
      </div>
      {node && (
        <DetailPane>
          <section
            id="workflow-node-settings"
            tabIndex={-1}
            aria-label="選択した工程の設定"
          >
            <h4>{node.id || "識別名未設定"}</h4>
            <button
              data-variant="secondary"
              onClick={() => {
                setPage(Math.floor(selected / PAGE));
                requestAnimationFrame(() =>
                  document.getElementById(`workflow-node-${selected}`)?.focus(),
                );
              }}
            >
              選択したノードをマップで確認
            </button>
            <StatusBadge
              label="種類"
              value={node.definition ? "Runノード" : "判断ノード"}
            />
            {node.definition &&
            labels[referenceKey(node.definition)]?.inputs?.length ? (
              <p>
                必須入力{" "}
                {labels[referenceKey(node.definition)]!.inputs!.join(" / ")}
              </p>
            ) : null}
            {children?.(
              node,
              node.definition
                ? labels[referenceKey(node.definition)]
                : undefined,
            )}
          </section>
        </DetailPane>
      )}
    </section>
  );
}
