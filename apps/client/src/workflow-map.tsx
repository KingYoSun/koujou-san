import { useEffect, useState, type ReactNode } from "react";
import { useScreenActive } from "./navigation";
import { StatusBadge } from "./status-badge";

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
  const [page, setPage] = useState(() => Math.floor(selected / 50));
  const [labels, setLabels] = useState<Record<string, DefinitionLabel>>({});
  const [notice, setNotice] = useState("");
  useEffect(() => {
    setPage(Math.floor(selected / 50));
  }, [selected]);
  const start =
    Math.min(page, Math.max(0, Math.ceil(graph.nodes.length / 50) - 1)) * 50;
  const nodes = graph.nodes.slice(start, start + 50);
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
  const choose = (index: number) => {
    onSelect(index);
    requestAnimationFrame(() =>
      document.getElementById("workflow-node-settings")?.focus(),
    );
  };
  const link = (id: string) => {
    const index = graph.nodes.findIndex((n) => n.id === id);
    return index < 0 ? (
      <span>{id || "接続先未指定"}（定義内に存在しません）</span>
    ) : (
      <button data-variant="secondary" onClick={() => choose(index)}>
        {id}
        {index < start || index >= start + 50 ? "（別ページ）" : ""}
      </button>
    );
  };
  const node = graph.nodes[selected];
  return (
    <section className="workflow-map" aria-label="工程の制御フロー">
      <p>
        制御フロー：{graph.name || "名前未設定の工程"}
        。矢印は実行の条件、入力の接続は選択ノードの詳細に表示します。
      </p>
      <p aria-live="polite">
        ノード{" "}
        {graph.nodes.length
          ? `${start + 1}–${Math.min(start + 50, graph.nodes.length)} / ${graph.nodes.length}`
          : "0件"}
        。別ページの接続先も選択できます。
      </p>
      {graph.nodes.length > 50 && (
        <div className="team-controls">
          <button disabled={!start} onClick={() => setPage(page - 1)}>
            ノードの前ページ
          </button>
          <button
            disabled={start + 50 >= graph.nodes.length}
            onClick={() => setPage(page + 1)}
          >
            ノードの次ページ
          </button>
        </div>
      )}
      {notice && <p role="status">{notice}</p>}
      <ol className="workflow-node-list" start={start + 1}>
        {nodes.map((n, offset) => (
          <li key={start + offset} data-selected={selected === start + offset}>
            <button
              id={`workflow-node-${start + offset}`}
              aria-pressed={selected === start + offset}
              onClick={() => choose(start + offset)}
            >
              {n.id || `識別名未設定（${start + offset + 1}）`}
            </button>
            <p>
              {n.definition
                ? !fixedWorkflowRef(n.definition)
                  ? "Run定義の固定版は未指定"
                  : labels[referenceKey(n.definition)]?.kind
                    ? labels[referenceKey(n.definition)]?.name ||
                      "定義名は未設定"
                    : labels[referenceKey(n.definition)]
                      ? "定義名を取得できません"
                      : "定義名は未取得"
                : n.choices?.length
                  ? "人の判断"
                  : "実行内容未設定"}
            </p>
            {n.definition && (
              <small>
                {n.definition.id}@{n.definition.version}
              </small>
            )}
            {status?.(n)}
            <div className="workflow-edges">
              <p>
                {n.depends?.length
                  ? `合流：${n.join === "all" ? "すべて（all）" : n.join === "any" ? "いずれか（any）" : "未指定"}`
                  : "起点（先行工程なし）"}
              </p>
              {n.depends?.map((dep, i) => (
                <p key={i}>
                  {link(dep.node)} → {n.id}：
                  {dep.on?.join(" / ") || "結果条件未指定"} ·{" "}
                  {dep.required ? "必須" : "任意"}
                </p>
              ))}
              {Object.entries(n.retry ?? {}).map(([on, target]) => (
                <p className="workflow-return" key={on}>
                  ↩ 差し戻し：{on} → {link(target)} · Run数上限{" "}
                  {graph.budget?.max_runs || "未設定"}
                </p>
              ))}
            </div>
          </li>
        ))}
      </ol>
      {node && (
        <section
          id="workflow-node-settings"
          tabIndex={-1}
          aria-label="選択した工程の設定"
        >
          <h4>{node.id || "識別名未設定"}の詳細</h4>
          <button
            data-variant="secondary"
            onClick={() => {
              setPage(Math.floor(selected / 50));
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
          {node.definition && (
            <p>
              必須入力：
              {labels[referenceKey(node.definition)]?.kind
                ? labels[referenceKey(node.definition)]?.inputs?.join(" / ") ||
                  "なし"
                : "未取得・閲覧不可（静的検証で確認）"}
            </p>
          )}
          {children?.(
            node,
            node.definition ? labels[referenceKey(node.definition)] : undefined,
          )}
        </section>
      )}
    </section>
  );
}
