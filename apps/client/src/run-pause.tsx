import { useEffect, useRef, useState } from "react";
import { syncRequest, workspaceAccessLost } from "./auth";
import { readAnswerDraft, saveAnswerDraft, syncStorage } from "./sync-store";
import type { AnswerDraft, SyncNamespace } from "./sync-store";
import { SyncHTTPError } from "./sync-error";

export type Pause = {
  response_kind?: string;
  response_origin?: { kind: string; id: string; source?: string };
  id: string;
  reason: string;
  request: unknown;
  state: string;
  response?: unknown;
  answered_by?: string;
  answered_at?: string;
  delivered_at?: string;
  resumed_at?: string;
};
type PauseRecord = { id: string; version: string; value: unknown };
type Choice = { value: string; label: string; impact?: string };
type Question = {
  question: string;
  background?: string;
  instructions?: string;
  waiting_for?: string;
  expertise?: string;
  response_format?: string;
  evidence_required?: boolean;
  options?: Choice[];
  target?: { label?: string; revision?: string; uri?: string };
  criteria?: { id: string; requirement: string }[];
};
const text = (v: unknown) => (typeof v === "string" ? v : undefined);
export function question(value: unknown): Question | undefined {
  if (!value || typeof value !== "object") return;
  const v = value as Record<string, unknown>;
  if (typeof v.question !== "string") return;
  return {
    question: v.question,
    background: text(v.background),
    instructions: text(v.instructions),
    waiting_for: text(v.waiting_for),
    expertise: text(v.expertise),
    response_format: text(v.response_format),
    evidence_required: v.evidence_required === true,
    options: Array.isArray(v.options)
      ? v.options.flatMap((entry): Choice[] => {
          if (typeof entry === "string")
            return [{ value: entry, label: entry }];
          if (
            !entry ||
            typeof entry !== "object" ||
            typeof entry.value !== "string" ||
            typeof entry.label !== "string"
          )
            return [];
          return [
            {
              value: entry.value,
              label: entry.label,
              impact: text(entry.impact),
            },
          ];
        })
      : [],
    target:
      v.target && typeof v.target === "object"
        ? {
            label: text((v.target as Record<string, unknown>).label),
            revision: text((v.target as Record<string, unknown>).revision),
            uri: text((v.target as Record<string, unknown>).uri),
          }
        : undefined,
    criteria: Array.isArray(v.criteria)
      ? v.criteria.filter(
          (c) =>
            c && typeof c.id === "string" && typeof c.requirement === "string",
        )
      : [],
  };
}
export function QuestionView({ value }: { value: unknown }) {
  const q = question(value);
  if (!q)
    return (
      <section aria-label="質問内容">
        <pre>{JSON.stringify(value, null, 2)}</pre>
      </section>
    );
  return (
    <section aria-label="判断に必要な情報">
      <h4>{q.question}</h4>
      {q.target && (
        <p>
          <strong>{q.target.label ?? "確認する成果物"}</strong>{" "}
          {q.target.uri && /^https?:\/\//.test(q.target.uri) && (
            <a href={q.target.uri} target="_blank" rel="noreferrer">
              対象の成果物を開く
            </a>
          )}
        </p>
      )}
      {q.instructions && (
        <p>
          <strong>確認する操作：</strong>
          {q.instructions}
        </p>
      )}
      {!!q.criteria?.length && (
        <ul>
          {q.criteria.map((c) => (
            <li key={c.id}>{c.requirement}</li>
          ))}
        </ul>
      )}
      <details>
        <summary>依頼の背景・回答の影響・確認する版</summary>
        {q.background && <p>{q.background}</p>}
        {q.waiting_for && <p>回答を待つ工程：{q.waiting_for}</p>}
        {q.expertise && <p>必要な専門知識：{q.expertise}</p>}
        {q.target?.revision && <p>成果物の固定版：{q.target.revision}</p>}
        {q.options?.map((c) => (
          <p key={c.value}>
            {c.label}：{c.impact ?? "影響の説明なし"}
          </p>
        ))}
      </details>
      <details>
        <summary>質問の原文・技術情報</summary>
        <pre>{JSON.stringify(value, null, 2)}</pre>
      </details>
    </section>
  );
}

export function PauseResponse({
  value,
  request,
}: {
  value: unknown;
  request: unknown;
}) {
  if (typeof value === "string") return <p>回答：{value}</p>;
  const v =
    value && typeof value === "object"
      ? (value as Record<string, unknown>)
      : undefined;
  if (
    question(request)?.response_format === "verdict" &&
    v &&
    typeof v.verdict === "string"
  )
    return (
      <section aria-label="記録した判断">
        <p>判断：{v.verdict}</p>
        {typeof v.reason === "string" && <p>理由：{v.reason}</p>}
        {typeof v.evidence === "string" && <p>証拠：{v.evidence}</p>}
        <details>
          <summary>回答の原文</summary>
          <pre>{JSON.stringify(value, null, 2)}</pre>
        </details>
      </section>
    );
  return <pre>{JSON.stringify(value, null, 2)}</pre>;
}

// One mutation path is shared by work details and the technical Run panel.
export async function submitPauseAnswer(
  native: boolean,
  namespace: SyncNamespace,
  runID: string,
  record: PauseRecord,
  draft: AnswerDraft,
  operationID: string,
) {
  const base = `/runs/workspaces/${encodeURIComponent(namespace.workspace)}`;
  const current = (await syncRequest(
    `${base}/records/${encodeURIComponent(runID)}?version=head`,
    native,
    { method: "GET" },
  )) as {
    version: string;
    value: { state: string; active_pause?: { id: string; version: string } };
  };
  if (
    current.value.state !== "paused" ||
    current.value.active_pause?.id !== record.id ||
    current.value.active_pause.version !== draft.pauseVersion
  )
    throw Error("質問の状態が変わりました。最新の質問を確認してください。");
  const result = (await syncRequest(
    `${base}/executions/${encodeURIComponent(runID)}/answer`,
    native,
    {
      method: "POST",
      body: {
        operation_id: operationID,
        expected_version: current.version,
        pause_id: (record.value as Pause).id,
        value: draft.json ? JSON.parse(draft.value) : draft.value,
      },
    },
  )) as { data?: { id: string; version: string } };
  if (!result.data)
    throw Error("回答の受付を確認できません。最新の状態を取得してください。");
  await saveAnswerDraft(native, namespace, record.id);
}

export function PauseAnswer({
  native,
  namespace,
  runID,
  record,
  busy = false,
  onAccepted,
  onDenied,
}: {
  native: boolean;
  namespace: SyncNamespace;
  runID: string;
  record: PauseRecord;
  busy?: boolean;
  onAccepted: () => Promise<void>;
  onDenied?: (error: unknown) => Promise<void>;
}) {
  const [draft, setDraft] = useState<AnswerDraft>();
  const [notice, setNotice] = useState("");
  const [sending, setSending] = useState(false);
  const pending = useRef<{ key: string; id: string } | undefined>(undefined);
  const { api, user, workspace } = namespace;
  const pause = record.value as Pause;
  const q = question(pause.request);
  useEffect(() => {
    let active = true;
    readAnswerDraft(native, { api, user, workspace }, record.id)
      .then((saved) => {
        if (active)
          setDraft(
            saved ?? { pauseVersion: record.version, value: "", json: false },
          );
      })
      .catch(() => {
        if (active) setNotice("端末内の回答案を読み戻せません。");
      });
    return () => {
      active = false;
    };
  }, [native, api, user, workspace, record.id, record.version]);
  if (!draft)
    return <p role="status">{notice || "端末内の回答案を確認しています…"}</p>;
  const stale =
    pause.state !== "waiting" || draft.pauseVersion !== record.version;
  if (stale && !draft.value) return null;
  const change = (value: AnswerDraft) => {
    setDraft(value);
    setNotice("端末へ保存しています…");
    void saveAnswerDraft(native, namespace, record.id, value)
      .then(() =>
        setNotice("この端末に回答案を保存しました。自動送信しません。"),
      )
      .catch(() => setNotice("回答案の保存を確認できません。"));
  };
  const human = q?.response_format === "verdict";
  let answer: { verdict?: string; reason?: string; evidence?: string } = {};
  if (human && draft.json) {
    try {
      const parsed = JSON.parse(draft.value);
      if (parsed && typeof parsed === "object")
        answer = {
          verdict: text(parsed.verdict),
          reason: text(parsed.reason),
          evidence: text(parsed.evidence),
        };
    } catch {
      /* Keep the saved draft for explicit replacement. */
    }
  }
  const humanChange = (field: string, value: string) =>
    change({
      ...draft,
      json: true,
      value: JSON.stringify({ ...answer, [field]: value }),
    });
  const ready = human
    ? !!answer.verdict &&
      !!answer.reason &&
      (!q?.evidence_required || !!answer.evidence)
    : !!draft.value;
  return (
    <fieldset disabled={busy || sending}>
      <legend>質問への回答</legend>
      {draft.value && !stale && (
        <p>未送信の回答案を端末に保存しています。API受付とは別です。</p>
      )}
      {stale && (
        <p>質問の状態が変わりました。保存済みの回答案は送信できません。</p>
      )}
      {human ? (
        <>
          <label>
            判断
            <select
              disabled={stale}
              value={answer.verdict ?? ""}
              onChange={(e) => humanChange("verdict", e.target.value)}
            >
              <option value="">判断を選択</option>
              {(q.options?.length
                ? q.options
                : [
                    { value: "PASS", label: "条件を満たした" },
                    { value: "FAIL", label: "条件を満たさない" },
                    { value: "INCONCLUSIVE", label: "判断できない" },
                  ]
              ).map((c) => (
                <option key={c.value} value={c.value}>
                  {c.label}
                </option>
              ))}
            </select>
          </label>
          <label>
            判断の理由
            <textarea
              readOnly={stale}
              value={answer.reason ?? ""}
              onChange={(e) => humanChange("reason", e.target.value)}
              rows={3}
            />
          </label>
          <label>
            確認した証拠・URL
            <textarea
              readOnly={stale}
              value={answer.evidence ?? ""}
              onChange={(e) => humanChange("evidence", e.target.value)}
              rows={2}
            />
          </label>
        </>
      ) : (
        <>
          {!!q?.options?.length && (
            <div className="team-controls">
              {q.options.map((c) => (
                <button
                  type="button"
                  key={c.value}
                  disabled={stale}
                  onClick={() =>
                    change({ ...draft, value: c.value, json: false })
                  }
                >
                  {c.label}
                </button>
              ))}
            </div>
          )}
          <label>
            質問への回答
            <textarea
              rows={3}
              readOnly={stale}
              value={draft.value}
              onChange={(e) => change({ ...draft, value: e.target.value })}
            />
          </label>
          <label>
            <input
              type="checkbox"
              disabled={stale}
              checked={draft.json}
              onChange={(e) => change({ ...draft, json: e.target.checked })}
            />
            構造化JSONで回答
          </label>
        </>
      )}
      {notice && <p role="status">{notice}</p>}
      {!stale && (
        <button
          disabled={!ready}
          onClick={async () => {
            setSending(true);
            try {
              const key = JSON.stringify(draft);
              if (pending.current?.key !== key)
                pending.current = { key, id: crypto.randomUUID() };
              await submitPauseAnswer(
                native,
                namespace,
                runID,
                record,
                draft,
                pending.current.id,
              );
              setDraft({
                pauseVersion: record.version,
                value: "",
                json: false,
              });
              await onAccepted();
              setNotice(
                "APIが回答を受領しました。配送・実際の継続は別に確認してください。",
              );
            } catch (error) {
              setNotice(String(error).replace(/^Error: /, ""));
              if (
                onDenied &&
                error instanceof SyncHTTPError &&
                (error.status === 401 || error.status === 403)
              )
                await onDenied(error);
              else if (await workspaceAccessLost(native, workspace, error))
                await syncStorage(native).remove(namespace);
            } finally {
              setSending(false);
            }
          }}
        >
          回答を送信
        </button>
      )}
      {draft.value && (
        <button
          onClick={() => {
            void saveAnswerDraft(native, namespace, record.id)
              .then(() => {
                setDraft({
                  pauseVersion: record.version,
                  value: "",
                  json: false,
                });
                setNotice("回答案を削除しました。");
              })
              .catch(() => setNotice("回答案の削除を確認できません。"));
          }}
        >
          この回答案を削除
        </button>
      )}
    </fieldset>
  );
}
