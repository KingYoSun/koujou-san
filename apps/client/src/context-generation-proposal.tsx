import { useEffect, useMemo, useState } from "react";
import { editField, fieldSource } from "./workflow-json";
import type { Ref } from "./context-ui";

export type ContextChange = {
  key: string;
  target: Ref;
  value_utf8: string;
  state: string;
  reason: string;
  references: Ref[];
};
export type ContextGrouping = {
  key: string;
  id?: string;
  scope: { kind: string; id: string };
  context: { kind: string; id: string };
  present: boolean;
  expected_version: string;
};
export type ContextChanges = {
  changes: ContextChange[];
  grouping: ContextGrouping[];
  candidates: { ref: Ref; relation: string; reason: string }[];
};
export const emptyContextChanges = JSON.stringify({
  changes: [],
  grouping: [],
  candidates: [],
});
const field = (text: string, path: string[]) => {
  try {
    return fieldSource(text, path);
  } catch {
    return "";
  }
};
const stringField = (text: string, path: string[]) => {
  try {
    const v = JSON.parse(field(text, path) || '""');
    return typeof v === "string" ? v : "";
  } catch {
    return "";
  }
};
export const contextStates: Record<string, string> = {
  observed: "元出力にある観測（独立検品ではありません）",
  inference: "モデルの推測",
  judgment: "採用する判断",
  proposal: "未確認の提案",
};
export function ContextGenerationProposal({
  value,
  goal,
  onPaths,
  onRevise,
  editable,
}: {
  value: string;
  goal: string;
  onPaths: (paths: string[]) => void;
  onRevise: (value: string) => void;
  editable: boolean;
}) {
  const [text, setText] = useState(value),
    [page, setPage] = useState(0),
    [candidatePage, setCandidatePage] = useState(0),
    [chosen, setChosen] = useState<string[]>([]),
    [error, setError] = useState("");
  const data = useMemo(() => {
    try {
      return JSON.parse(text) as ContextChanges;
    } catch {
      return undefined;
    }
  }, [text]);
  useEffect(() => {
    setText(value);
    setPage(0);
    setCandidatePage(0);
    const parsed = JSON.parse(value) as ContextChanges;
    const keys = [...parsed.changes, ...parsed.grouping].map((c) => c.key);
    setChosen(keys);
    onPaths(keys);
  }, [value]);
  const rows = [
    ...(data?.changes ?? []).map((c) => ({ key: c.key, change: c })),
    ...(data?.grouping ?? []).map((g) => ({ key: g.key, group: g })),
  ];
  const update = (change: ContextChange, path: string, raw: string) => {
    try {
      const next = {
        ...data!,
        changes: data!.changes.map((c) =>
          c.key === change.key
            ? { ...c, value_utf8: editField(c.value_utf8, [path], raw) }
            : c,
        ),
      };
      setText(JSON.stringify(next));
      setError("");
    } catch {
      setError("変更した項目の形式を確認してください。");
    }
  };
  return (
    <section aria-label="Contextの生成案と採否">
      <h4>原依頼と変更案</h4>
      <p>{goal}</p>
      <p>
        文書構成と所属を別の変更として扱います。類似候補だけでは統合・削除しません。開始済みRunと過去の判定・入力は保持します。
      </p>
      <ul>
        {rows.slice(page * 50, page * 50 + 50).map((row) => (
          <li key={row.key}>
            <label>
              <input
                type="checkbox"
                checked={chosen.includes(row.key)}
                onChange={(e) => {
                  const next = e.target.checked
                    ? [...chosen, row.key]
                    : chosen.filter((k) => k !== row.key);
                  setChosen(next);
                  onPaths(next);
                }}
              />
              この変更を採用: {row.key}
            </label>
            {"change" in row ? (
              <>
                <p>
                  {row.change.target.kind} ·{" "}
                  {field(row.change.value_utf8, ["name"]) ||
                    field(row.change.value_utf8, ["title"]) ||
                    row.change.target.id}{" "}
                  ·{" "}
                  {row.change.target.revision === "0"
                    ? "新規作成"
                    : "既存の新版"}
                </p>
                <p>
                  {contextStates[row.change.state] ?? row.change.state} ·{" "}
                  {row.change.reason}
                </p>
                {row.change.target.kind === "sentence" ? (
                  <label>
                    生成した本文
                    <textarea
                      disabled={!editable}
                      value={stringField(row.change.value_utf8, ["text"])}
                      onChange={(e) =>
                        update(
                          row.change,
                          "text",
                          JSON.stringify(e.target.value),
                        )
                      }
                    />
                  </label>
                ) : (
                  <>
                    <p>
                      順序付きの構成参照:{" "}
                      {field(row.change.value_utf8, ["children"])}
                    </p>
                    {row.change.target.kind !== "paragraph" && (
                      <label>
                        見出し
                        <input
                          disabled={!editable}
                          value={stringField(row.change.value_utf8, ["title"])}
                          onChange={(e) =>
                            update(
                              row.change,
                              "title",
                              JSON.stringify(e.target.value),
                            )
                          }
                        />
                      </label>
                    )}
                  </>
                )}
                <p>
                  参照した固定版:{" "}
                  {row.change.references
                    .map((r) => `${r.kind}:${r.id}@${r.revision}`)
                    .join("、") || "元出力のみ"}
                </p>
              </>
            ) : (
              <p>
                所属: {row.group.context.id} → {row.group.scope.id} ·{" "}
                {row.group.present ? "追加" : "解除"} · 元版{" "}
                {row.group.expected_version}
              </p>
            )}
          </li>
        ))}
      </ul>
      {rows.length > 50 && (
        <>
          <button disabled={page === 0} onClick={() => setPage(page - 1)}>
            変更の前の50件
          </button>
          <button
            disabled={(page + 1) * 50 >= rows.length}
            onClick={() => setPage(page + 1)}
          >
            変更の次の50件
          </button>
        </>
      )}
      {data?.candidates.length ? (
        <details>
          <summary>再利用・重複・矛盾の候補</summary>
          <ul>
            {data.candidates
              .slice(candidatePage * 50, candidatePage * 50 + 50)
              .map((c, i) => (
                <li key={i}>
                  {c.relation} · {c.ref.kind}:{c.ref.id}@{c.ref.revision} ·{" "}
                  {c.reason}
                </li>
              ))}
          </ul>
          {data.candidates.length > 50 && (
            <>
              <button
                disabled={candidatePage === 0}
                onClick={() => setCandidatePage(candidatePage - 1)}
              >
                候補の前の50件
              </button>
              <button
                disabled={(candidatePage + 1) * 50 >= data.candidates.length}
                onClick={() => setCandidatePage(candidatePage + 1)}
              >
                候補の次の50件
              </button>
            </>
          )}
        </details>
      ) : null}
      <details>
        <summary>変更案のJSONを編集</summary>
        <textarea
          aria-label="Context変更案のJSON"
          value={text}
          disabled={!editable}
          onChange={(e) => setText(e.target.value)}
        />
      </details>
      {editable && (
        <button
          onClick={() => {
            try {
              const parsed = JSON.parse(text) as ContextChanges;
              if (
                !Array.isArray(parsed.changes) ||
                !Array.isArray(parsed.grouping) ||
                !Array.isArray(parsed.candidates)
              )
                throw Error();
              onRevise(text);
              setError("");
            } catch {
              setError("変更案の形式を確認してください。");
            }
          }}
        >
          手動修正した案を保存（LLM要求なし）
        </button>
      )}
      <p role="status">{error}</p>
    </section>
  );
}
