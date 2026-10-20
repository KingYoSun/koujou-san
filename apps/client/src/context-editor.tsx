import { useEffect, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useStore } from "zustand";
import type { ContextRecord } from "./context-contract";
import type { UI, Ref, Candidate } from "./context-ui";
import {
  contextPath,
  identityKey,
  refKey,
  refLabel,
  labels,
  kinds,
  scopeKinds,
} from "./context-ui";
import { ContextGenerationOrigin } from "./context-generation-origin";
import { EnvelopeStatus } from "./context-status";
import { useScreenActive } from "./navigation";
export function ContextEditor({
  ui,
  active = true,
}: {
  ui: UI;
  active?: boolean;
}) {
  const visible = useScreenActive() && active;
  const state = useStore(ui.store);
  const selected = state.selected;
  const key = selected ? identityKey(selected) : "";
  const [historyAfter, setHistoryAfter] = useState("");
  const [historyBack, setHistoryBack] = useState<string[]>([]);
  const queryClient = useQueryClient();
  useEffect(() => {
    setHistoryAfter("");
    setHistoryBack([]);
    if (visible && selected) void ui.engine.open(selected).catch(ui.fail);
  }, [key, ui.engine, visible]);
  const current =
    ui.engine.state.items[key] ?? ui.engine.state.drafts[key]?.base;
  const draft = ui.engine.state.drafts[key];
  const historical = state.historical;
  const fixed = useQuery({
    queryKey: [...ui.prefix, "body", historical],
    enabled: visible && Boolean(historical),
    retry: false,
    queryFn: () =>
      ui.request<ContextRecord<Record<string, unknown>>>(
        `${contextPath(ui.workspace, historical!)}?revision=${encodeURIComponent(historical!.revision)}`,
      ),
  });
  const history = useQuery({
    queryKey: [
      ...ui.prefix,
      "history",
      key,
      historyAfter,
      current?.record.ref.revision,
    ],
    enabled:
      visible && Boolean(selected && current?.record.ref.revision !== "0"),
    retry: false,
    queryFn: () =>
      ui.request<{ items: Candidate[] }>(
        `/contexts/workspaces/${encodeURIComponent(ui.workspace)}/catalog`,
        { kind: selected!.kind, id: selected!.id, after: historyAfter },
      ),
  });
  const fixedRecord = historical ? fixed.data?.data : undefined;
  const value = historical
    ? fixedRecord?.value
    : (draft?.value ?? current?.record.value);
  const ref = historical ?? current?.record.ref;
  const editable = Boolean(
    current?.editable &&
    !historical &&
    !draft?.pending &&
    !ui.engine.sending.has(key) &&
    ui.engine.status !== "revoked",
  );
  function edit(changes: Record<string, unknown>) {
    if (value)
      void ui.engine.edit(key, { ...value, ...changes }).catch(ui.fail);
  }
  async function save(reapply = false) {
    try {
      if (draft && !draft.pending && Array.isArray(draft.value.tags))
        await ui.engine.edit(key, {
          ...draft.value,
          tags: (draft.value.tags as string[])
            .map((tag) => tag.trim())
            .filter(Boolean),
        });
      await ui.engine.send(key, reapply);
      await queryClient.invalidateQueries({ queryKey: ui.prefix });
    } catch (error) {
      ui.fail(error);
    }
  }
  if (!visible) return null;
  if (!selected)
    return (
      <section className="context-editor">
        <h3>Contextを編集</h3>
        <p>検索からContextを開くか、新しく作成してください。</p>
      </section>
    );
  return (
    <section className="context-editor" aria-label="Contextを編集">
      <h3>{labels[selected.kind]}を編集</h3>
      {ref && <p className="context-meta">{refLabel(ref)}</p>}
      {historical && (
        <>
          <p className="context-warning">固定した過去の版を表示しています。</p>
          <EnvelopeStatus value={fixed.data} error={fixed.error} />
          <button onClick={() => ui.store.setState({ historical: undefined })}>
            最新の版へ戻る
          </button>
          {fixedRecord && current?.editable && (
            <button
              onClick={() => {
                void ui.engine
                  .edit(key, { ...fixedRecord.value })
                  .then(() => ui.store.setState({ historical: undefined }))
                  .catch(ui.fail);
              }}
            >
              この版の内容を下書きへ採用
            </button>
          )}
        </>
      )}
      {!value && (
        <p role="status">
          {ui.engine.status === "offline"
            ? "本文を取得できません。保存済みの本文もありません。"
            : ui.engine.busy
              ? "本文を取得中…"
              : "本文の取得権限または参照先を確認できません。"}
        </p>
      )}
      {value && (
        <>
          <p className={draft ? "context-warning" : "context-meta"}>
            {historical
              ? "過去の固定版"
              : draft?.pending
                ? "保存結果が未確認（同じ要求IDで再送）"
                : draft?.latest
                  ? "競合・未送信の下書き"
                  : draft
                    ? "未送信の下書き"
                    : ui.engine.status === "current"
                      ? "共有版を確認済み"
                      : "最後に確認した本文キャッシュ"}
            {!current?.editable && " · 閲覧のみ"}
          </p>
          {selected.kind === "membership" ? (
            <>
              <p>Scope: {JSON.stringify(value.scope)}</p>
              <p>Context: {JSON.stringify(value.context)}</p>
              <label>
                <input
                  type="checkbox"
                  disabled={!editable}
                  checked={Boolean(value.present)}
                  onChange={(event) => edit({ present: event.target.checked })}
                />
                直接所属する
              </label>
            </>
          ) : (
            <>
              <label>
                {selected.kind === "sentence" ||
                selected.kind === "paragraph" ||
                selected.kind === "scope"
                  ? "名前"
                  : "タイトル"}
                <input
                  aria-label="Contextの名前"
                  disabled={!editable}
                  value={String(
                    selected.kind === "section" || selected.kind === "document"
                      ? (value.title ?? value.name ?? "")
                      : (value.name ?? ""),
                  )}
                  onChange={(event) =>
                    edit({
                      [selected.kind === "sentence" ||
                      selected.kind === "paragraph" ||
                      selected.kind === "scope"
                        ? "name"
                        : "title"]: event.target.value,
                    })
                  }
                />
              </label>
              {selected.kind === "sentence" && (
                <label>
                  本文
                  <textarea
                    aria-label="Contextの本文"
                    rows={8}
                    disabled={!editable || Boolean(value.external)}
                    value={String(value.text ?? "")}
                    onChange={(event) => edit({ text: event.target.value })}
                  />
                </label>
              )}
              {kinds.includes(selected.kind) && (
                <label>
                  分類タグ（1行に1件）
                  <textarea
                    aria-label="Contextの分類タグ"
                    disabled={!editable}
                    value={((value.tags ?? []) as string[]).join("\n")}
                    onChange={(event) =>
                      edit({ tags: event.target.value.split("\n") })
                    }
                  />
                </label>
              )}
              {kinds.indexOf(selected.kind) > 0 && (
                <section>
                  <h4>子の参照・順序</h4>
                  <p className="context-meta">
                    {labels[kinds[kinds.indexOf(selected.kind) - 1]!]}
                    の固定版を参照します。
                  </p>
                  <ol className="context-ref-list">
                    {((value.children ?? []) as Ref[]).map(
                      (child, index, array) => (
                        <li key={`${refKey(child)}:${index}`}>
                          <button
                            className="link-button"
                            onClick={() =>
                              ui.store.setState({
                                selected: child,
                                historical: child,
                              })
                            }
                          >
                            {refLabel(child)}
                          </button>
                          <div className="context-actions">
                            <button
                              disabled={!editable || index === 0}
                              aria-label={`子${index + 1}を上へ`}
                              onClick={() => {
                                const children = [...array];
                                [children[index - 1], children[index]] = [
                                  children[index]!,
                                  children[index - 1]!,
                                ];
                                edit({ children });
                              }}
                            >
                              ↑
                            </button>
                            <button
                              disabled={!editable || index === array.length - 1}
                              aria-label={`子${index + 1}を下へ`}
                              onClick={() => {
                                const children = [...array];
                                [children[index + 1], children[index]] = [
                                  children[index]!,
                                  children[index + 1]!,
                                ];
                                edit({ children });
                              }}
                            >
                              ↓
                            </button>
                            <button
                              disabled={!editable}
                              onClick={() =>
                                edit({
                                  children: array.filter((_, i) => i !== index),
                                })
                              }
                            >
                              参照を外す
                            </button>
                            <button onClick={() => state.add(child, true)}>
                              入力から除外
                            </button>
                          </div>
                        </li>
                      ),
                    )}
                  </ol>
                  <button
                    disabled={
                      !editable ||
                      state.reference?.ref.kind !==
                        kinds[kinds.indexOf(selected.kind) - 1]
                    }
                    onClick={() =>
                      edit({
                        children: [
                          ...((value.children ?? []) as Ref[]),
                          state.reference!.ref,
                        ],
                      })
                    }
                  >
                    候補の固定版を子として追加
                  </button>
                  {state.reference && (
                    <p className="context-meta">
                      参照候補: {state.reference.name} ·{" "}
                      {refLabel(state.reference.ref)}
                    </p>
                  )}
                </section>
              )}
              {selected.kind === "scope" && (
                <>
                  <p>種類: {labels[String(value.kind)]}（階層は任意）</p>
                  <p>
                    親Scope:{" "}
                    {value.parent ? JSON.stringify(value.parent) : "なし"}
                  </p>
                  <button
                    disabled={
                      !editable ||
                      !state.reference?.scope_kind ||
                      scopeKinds.indexOf(state.reference?.scope_kind ?? "") !==
                        scopeKinds.indexOf(String(value.kind)) - 1 ||
                      state.reference?.ref.id === selected.id
                    }
                    onClick={() =>
                      edit({
                        parent: {
                          kind: state.reference!.scope_kind,
                          id: state.reference!.ref.id,
                        },
                      })
                    }
                  >
                    候補のScopeを親へ設定
                  </button>
                  <button
                    disabled={!editable}
                    onClick={() => edit({ parent: undefined })}
                  >
                    親を外す
                  </button>
                </>
              )}
            </>
          )}
          {ref &&
            ref.revision !== "0" &&
            (value.origin as { kind?: string })?.kind === "run-output" &&
            typeof (value.origin as { operation_id?: unknown })
              ?.operation_id === "string" && (
              <ContextGenerationOrigin
                ui={ui}
                refValue={ref}
                operation={
                  (value.origin as { operation_id: string }).operation_id
                }
              />
            )}
          {(value.origin || value.external || value.attachments) && (
            <details open>
              <summary>由来・固定参照・添付・取得状態</summary>
              <pre>
                {JSON.stringify(
                  {
                    origin: value.origin,
                    external: value.external,
                    attachments: value.attachments,
                  },
                  null,
                  2,
                )}
              </pre>
              <p className="context-meta">
                固定済みの内容を保持します。外部からの取得は始まりません。
              </p>
            </details>
          )}
          {!historical && draft && (
            <>
              <p role="alert">{draft.error}</p>
              {draft.latest && (
                <>
                  <h4>競合した変更を比較</h4>
                  <div className="context-diff">
                    <div>
                      <h5>下書きの基準版</h5>
                      <pre>
                        {JSON.stringify(draft.base.record.value, null, 2)}
                      </pre>
                    </div>
                    <div>
                      <h5>現在の共有版</h5>
                      <pre>
                        {JSON.stringify(draft.latest.record.value, null, 2)}
                      </pre>
                    </div>
                    <div>
                      <h5>未送信の下書き</h5>
                      <pre>{JSON.stringify(draft.value, null, 2)}</pre>
                    </div>
                  </div>
                </>
              )}
              <button
                className="primary"
                disabled={
                  ui.engine.status !== "current" ||
                  (!editable && !draft.pending) ||
                  ui.engine.sending.has(key) ||
                  !current?.editable
                }
                onClick={() => void save(Boolean(draft.latest))}
              >
                {draft.latest
                  ? "差分を確認し、この下書きを最新の版へ再適用"
                  : draft.pending
                    ? "同じ要求IDで保存結果を確認"
                    : "下書きを共有版へ保存"}
              </button>
            </>
          )}
          {ref && ref.revision !== "0" && kinds.includes(ref.kind) && (
            <div className="context-actions">
              <button onClick={() => state.add(ref)}>
                この固定版を入力へ追加
              </button>
              <button onClick={() => state.add(ref, true)}>
                この固定版を入力から除外
              </button>
            </div>
          )}
          {ui.engine.state.receipts[key] && (
            <p className="context-meta">
              保存済みの版: {ui.engine.state.receipts[key]!.revision}
            </p>
          )}
        </>
      )}
      <details>
        <summary>版履歴</summary>
        <EnvelopeStatus value={history.data} error={history.error} />
        <ul>
          {history.data?.data?.items.map((candidate) => (
            <li key={refKey(candidate.ref)}>
              <button
                className="link-button"
                onClick={() => ui.store.setState({ historical: candidate.ref })}
              >
                {candidate.name || candidate.ref.id} · {candidate.ref.revision}
              </button>
            </li>
          ))}
        </ul>
        <button
          disabled={!historyBack.length}
          onClick={() => {
            setHistoryAfter(historyBack.at(-1)!);
            setHistoryBack(historyBack.slice(0, -1));
          }}
        >
          新しい版へ
        </button>
        <button
          disabled={!history.data?.meta.next_cursor}
          onClick={() => {
            setHistoryBack([...historyBack, historyAfter]);
            setHistoryAfter(history.data!.meta.next_cursor!);
          }}
        >
          古い版へ
        </button>
      </details>
    </section>
  );
}
