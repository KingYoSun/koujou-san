import { useMemo, useState } from "react";
import { editField, fieldSource } from "./workflow-json";
import type { Ref } from "./context-ui";
import { Disclosure } from "./disclosure";
import { jsonError } from "./field";
import { PageList } from "./page-list";
import { StatusBadge } from "./status-badge";

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
  observed: "元出力の観測",
  inference: "モデルの推測",
  judgment: "採用する判断",
  proposal: "未確認の提案",
};
export type ContextProposalDraft = { text: string; chosen: string[] };
export function ContextGenerationProposal({
  draft,
  goal,
  onDraft,
  onRevise,
  editable,
}: {
  draft: ContextProposalDraft;
  goal: string;
  onDraft: (draft: ContextProposalDraft) => void;
  onRevise: (value: string) => void;
  editable: boolean;
}) {
  const { text, chosen } = draft;
  const setText = (text: string) => onDraft({ ...draft, text });
  const setChosen = (chosen: string[]) => onDraft({ ...draft, chosen });
  const [error, setError] = useState("");
  const data = useMemo(() => {
    try {
      return JSON.parse(text) as ContextChanges;
    } catch {
      return undefined;
    }
  }, [text]);
  const rows = [
    ...(data?.changes ?? []).map((c) => ({ key: c.key, change: c })),
    ...(data?.grouping ?? []).map((g) => ({ key: g.key, group: g })),
  ];
  const shapeError = "変更案の形式を確認してください。";
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
      setError(jsonError(change.value_utf8) ?? shapeError);
    }
  };
  return (
    <section aria-label="Contextの生成案と採否">
      <h4>原依頼と変更案</h4>
      <p>{goal}</p>
      <PageList label="変更案" items={rows}>
        {(row) => (
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
                }}
              />
              この変更を採用：{row.key}
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
                  <StatusBadge
                    label="状態"
                    value={contextStates[row.change.state] ?? row.change.state}
                  />{" "}
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
                      順序付きの構成参照：
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
                  参照した固定版：
                  {row.change.references
                    .map((r) => `${r.kind}:${r.id}@${r.revision}`)
                    .join("、") || "元出力のみ"}
                </p>
              </>
            ) : (
              <p>
                所属：{row.group.context.id} → {row.group.scope.id} ·{" "}
                {row.group.present ? "追加" : "解除"} · 元版{" "}
                {row.group.expected_version}
              </p>
            )}
          </li>
        )}
      </PageList>
      {data?.candidates.length ? (
        <details>
          <summary>再利用・重複・矛盾の候補</summary>
          <PageList label="候補" items={data.candidates}>
            {(c, i) => (
              <li key={i}>
                {c.relation} · {c.ref.kind}:{c.ref.id}@{c.ref.revision} ·{" "}
                {c.reason}
              </li>
            )}
          </PageList>
        </details>
      ) : null}
      <Disclosure title="変更案のJSONを編集">
        {() => (
          <textarea
            aria-label="Context変更案のJSON"
            value={text}
            disabled={!editable}
            onChange={(e) => setText(e.target.value)}
          />
        )}
      </Disclosure>
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
              setError(jsonError(text) ?? shapeError);
            }
          }}
        >
          手動修正した案を保存
        </button>
      )}
      <p role="alert">{error}</p>
    </section>
  );
}
