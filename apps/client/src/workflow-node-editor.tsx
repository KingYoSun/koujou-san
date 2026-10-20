import { useState, useId } from "react";
import type { JSONPath } from "jsonc-parser";
import { editField, fieldSource } from "./workflow-json";
import { Field } from "./field";
import {
  WorkflowMap,
  type DefinitionLabel,
  type WorkflowGraph,
  type WorkflowRef,
} from "./workflow-map";
import { PageList } from "./page-list";
import { DefinitionPicker, type DefinitionPage } from "./definition-picker";

// Parsed values are for display only. All writes edit one path in the original source.
export function WorkflowNodeEditor({
  text,
  onChange,
  selected,
  onSelect,
  loadLabels,
  loadDefinitions,
  disabled = false,
}: {
  text: string | undefined;
  disabled?: boolean;
  onChange: (text: string) => void;
  selected: number;
  onSelect: (index: number) => void;
  loadLabels: (refs: WorkflowRef[]) => Promise<DefinitionLabel[]>;
  loadDefinitions: (after: string) => Promise<DefinitionPage>;
}) {
  const [error, setError] = useState("");
  let graph: WorkflowGraph;
  try {
    if (!text) throw Error();
    graph = JSON.parse(text) as WorkflowGraph;
    if (!Array.isArray(graph.nodes)) throw Error();
  } catch {
    return (
      <p role="status">
        構造を表示するには定義のJSON構文を修正してください。原文は保持しています。
      </p>
    );
  }
  const put = (path: JSONPath, value: unknown) => {
    try {
      onChange(
        editField(
          text,
          path,
          value === undefined ? undefined : JSON.stringify(value),
        ),
      );
      setError("");
    } catch {
      setError("入力を反映できません。JSON原文の構文を確認してください。");
    }
  };
  const strings = (label: string, path: JSONPath) => {
    let values: string[] = [];
    try {
      values = JSON.parse(fieldSource(text, path) || "[]") as string[];
    } catch {
      /* raw editor remains available */
    }
    return (
      <div>
        <p>{label}</p>
        <PageList label={label} items={values}>
          {(value, i) => (
            <li key={i}>
              <Field
                label={`${label} ${i + 1}`}
                value={value}
                onChange={(v) => put([...path, i], v)}
              />
              <button
                data-variant="secondary"
                onClick={() => put([...path, i], undefined)}
              >
                この項目を削除
              </button>
            </li>
          )}
        </PageList>
        <button
          data-variant="secondary"
          onClick={() => put([...path, values.length], "")}
        >
          {label}を追加
        </button>
      </div>
    );
  };
  const node = graph.nodes[selected];
  const path: JSONPath = ["nodes", selected];
  const selectNode = (
    label: string,
    value: string,
    onSelectValue: (id: string) => void,
  ) => (
    <NodeChoice
      label={label}
      value={value}
      nodes={graph.nodes}
      onChange={onSelectValue}
    />
  );
  return (
    <>
      <WorkflowMap
        graph={graph}
        selected={selected}
        onSelect={onSelect}
        loadLabels={loadLabels}
      >
        {() =>
          node && (
            <fieldset disabled={disabled}>
              <Field
                label="工程の識別名"
                value={node.id}
                onChange={(v) => put([...path, "id"], v)}
                hint="参照している他ノードの識別名は自動変更しません。保存後の静的検証で確認します。"
              />
              <details>
                <summary>Run定義の固定参照・判断の選択肢</summary>
                <DefinitionPicker
                  load={loadDefinitions}
                  onChoose={(ref) => put([...path, "definition"], ref)}
                />
                <Field
                  label="Run定義ID"
                  value={node.definition?.id ?? ""}
                  onChange={(v) => put([...path, "definition", "id"], v)}
                />
                <Field
                  label="Run定義の固定版"
                  value={node.definition?.version ?? ""}
                  onChange={(v) => put([...path, "definition", "version"], v)}
                />
                <button
                  data-variant="secondary"
                  onClick={() => put([...path, "definition"], undefined)}
                >
                  Run定義の指定を外す（判断ノード用）
                </button>
                {strings("判断の選択肢", [...path, "choices"])}
              </details>
              <h5>制御の接続</h5>
              <label>
                合流の条件
                <select
                  value={node.join ?? ""}
                  onChange={(e) => put([...path, "join"], e.target.value)}
                >
                  <option value="">未指定</option>
                  <option value="all">すべての先行結果（all）</option>
                  <option value="any">いずれかの先行結果（any）</option>
                </select>
              </label>
              <PageList label="先行工程" items={node.depends ?? []}>
                {(dep, i) => (
                  <li key={i}>
                    {selectNode(`先行工程 ${i + 1}`, dep.node, (v) =>
                      put([...path, "depends", i, "node"], v),
                    )}
                    {strings(`結果条件 ${i + 1}`, [
                      ...path,
                      "depends",
                      i,
                      "on",
                    ])}
                    <label>
                      <input
                        type="checkbox"
                        checked={!!dep.required}
                        onChange={(e) =>
                          put(
                            [...path, "depends", i, "required"],
                            e.target.checked,
                          )
                        }
                      />
                      必須の先行工程
                    </label>
                    <button
                      data-variant="secondary"
                      onClick={() => put([...path, "depends", i], undefined)}
                    >
                      この依存を削除
                    </button>
                  </li>
                )}
              </PageList>
              <button
                data-variant="secondary"
                onClick={() =>
                  put([...path, "depends", node.depends?.length ?? 0], {
                    node: "",
                    on: [],
                    required: true,
                  })
                }
              >
                先行工程を追加
              </button>
              <h5>入力の接続（制御依存とは別）</h5>
              <PageList label="入力接続" items={node.bindings ?? []}>
                {(b, i) => (
                  <li key={i}>
                    {selectNode(`入力元の工程 ${i + 1}`, b.from, (v) =>
                      put([...path, "bindings", i, "from"], v),
                    )}
                    <label>
                      入力元の種類
                      <select
                        value={b.kind}
                        onChange={(e) =>
                          put([...path, "bindings", i, "kind"], e.target.value)
                        }
                      >
                        <option value="output">原出力（output）</option>
                        <option value="result">派生結果（result）</option>
                      </select>
                    </label>
                    <Field
                      label={`出力チャンネル ${i + 1}`}
                      value={b.channel ?? ""}
                      onChange={(v) =>
                        put([...path, "bindings", i, "channel"], v)
                      }
                    />
                    <Field
                      label={`出力の配列位置 ${i + 1}`}
                      value={String(b.index ?? 0)}
                      onChange={(v) => {
                        if (/^\d+$/.test(v))
                          put([...path, "bindings", i, "index"], Number(v));
                      }}
                    />
                    {strings(`出力キー・配列位置 ${i + 1}`, [
                      ...path,
                      "bindings",
                      i,
                      "path",
                    ])}
                    <label>
                      入力先
                      <select
                        value={b.to}
                        onChange={(e) =>
                          put([...path, "bindings", i, "to"], e.target.value)
                        }
                      >
                        {["prompt", "context", "command"].map((v) => (
                          <option key={v}>{v}</option>
                        ))}
                      </select>
                    </label>
                    {b.to === "command" &&
                      strings(`commandの入力先キー ${i + 1}`, [
                        ...path,
                        "bindings",
                        i,
                        "parameter",
                      ])}
                    <button
                      data-variant="secondary"
                      onClick={() => put([...path, "bindings", i], undefined)}
                    >
                      この入力接続を削除
                    </button>
                  </li>
                )}
              </PageList>
              <button
                data-variant="secondary"
                onClick={() =>
                  put([...path, "bindings", node.bindings?.length ?? 0], {
                    from: "",
                    kind: "output",
                    channel: "stdout",
                    index: 0,
                    path: [],
                    to: "prompt",
                  })
                }
              >
                入力接続を追加
              </button>
              {strings("開始時に明示する入力", [...path, "inputs"])}
              <h5>結果による差し戻し</h5>
              <p>
                戻り先にはRunノードを指定します。有限のRun数上限は工程全体の予算で設定します。
              </p>
              <PageList
                label="差し戻し"
                items={Object.entries(node.retry ?? {})}
              >
                {([on, target], index) => (
                  <li key={index}>
                    <Field
                      label="戻す結果条件"
                      value={on}
                      onChange={(v) => {
                        if (v && v !== on && !(v in (node.retry ?? {})))
                          put(
                            [...path, "retry"],
                            Object.fromEntries(
                              Object.entries(node.retry ?? {}).map(
                                ([key, value]) => [key === on ? v : key, value],
                              ),
                            ),
                          );
                      }}
                    />
                    {selectNode(`戻り先（${on}）`, target, (v) =>
                      put([...path, "retry", on], v),
                    )}
                    <button
                      data-variant="secondary"
                      onClick={() => put([...path, "retry", on], undefined)}
                    >
                      この差し戻しを削除
                    </button>
                  </li>
                )}
              </PageList>
              <button
                data-variant="secondary"
                onClick={() => {
                  let key = "FAIL";
                  while (key in (node.retry ?? {})) key += "_";
                  put([...path, "retry", key], "");
                }}
              >
                差し戻しを追加
              </button>
            </fieldset>
          )
        }
      </WorkflowMap>
      <button
        disabled={disabled}
        onClick={() => {
          let id = `node-${graph.nodes.length + 1}`;
          while (graph.nodes.some((n) => n.id === id)) id += "_";
          put(["nodes", graph.nodes.length], { id, join: "all" });
          onSelect(graph.nodes.length);
        }}
      >
        工程を追加
      </button>
      {error && <p role="alert">{error}</p>}
    </>
  );
}

function NodeChoice({
  label,
  value,
  nodes,
  onChange,
}: {
  label: string;
  value: string;
  nodes: WorkflowGraph["nodes"];
  onChange: (id: string) => void;
}) {
  const id = useId();
  const [query, setQuery] = useState("");
  // Only node metadata from this fixed definition is searched, never Workspace data.
  const candidates = nodes.filter((n) => n.id.includes(query));
  return (
    <details>
      <summary>
        {label}：{value || "未指定"}
      </summary>
      <Field
        label={`${label}の識別名`}
        value={value}
        onChange={onChange}
        hint="未定義の識別名も入力できます。存在確認は共有保存後の静的検証で行います。"
      />
      <label htmlFor={id}>
        名前で候補を絞る
        <input
          id={id}
          value={query}
          onChange={(e) => setQuery(e.target.value)}
        />
      </label>
      <PageList label={`${label}の候補`} items={candidates}>
        {(n, i) => (
          <li key={i}>
            <button data-variant="secondary" onClick={() => onChange(n.id)}>
              {n.id || "識別名未設定"}
            </button>
          </li>
        )}
      </PageList>
    </details>
  );
}
