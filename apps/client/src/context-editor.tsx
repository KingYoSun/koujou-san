import { useEffect, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useStore } from "zustand";
import type { ContextRecord } from "./context-contract";
import type { UI, Ref, Identity, Candidate } from "./context-ui";
import {
  contextPath,
  identityKey,
  identityLabel,
  refKey,
  refLabel,
  labels,
  kinds,
  scopeKinds,
} from "./context-ui";
import { ContextGenerationOrigin } from "./context-generation-origin";
import { EnvelopeStatus, ListCount } from "./context-status";
import { Disclosure } from "./disclosure";
import { StatusBadge } from "./status-badge";
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
  const queryClient = useQueryClient();
  useEffect(() => {
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
        <p>Contextが選ばれていません。</p>
      </section>
    );
  return (
    <section className="context-editor" aria-label="Contextを編集">
      <h3 id="context-editor-title" tabIndex={-1}>
        {labels[selected.kind]}を編集
      </h3>
      {ref && <p className="context-meta">{refLabel(ref)}</p>}
      {historical && (
        <>
          <EnvelopeStatus value={fixed.data} error={fixed.error} />
          <button
            data-variant="secondary"
            onClick={() => ui.store.setState({ historical: undefined })}
          >
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
          <p>
            <StatusBadge
              label="状態"
              tone={draft ? "warning" : "neutral"}
              value={
                historical
                  ? "過去の固定版"
                  : draft?.pending
                    ? "保存結果が未確認"
                    : draft?.latest
                      ? "競合・未送信の下書き"
                      : draft
                        ? "未送信の下書き"
                        : ui.engine.status === "current"
                          ? "共有版を確認済み"
                          : "最後に確認した本文キャッシュ"
              }
            />
            {!current?.editable && (
              <StatusBadge label="権限" value="閲覧のみ" />
            )}
          </p>
          {selected.kind === "membership" ? (
            <>
              <p>Scope：{identityLabel(value.scope as Identity)}</p>
              <p>Context：{identityLabel(value.context as Identity)}</p>
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
                    rows={8}
                    disabled={!editable || Boolean(value.external)}
                    value={String(value.text ?? "")}
                    onChange={(event) => edit({ text: event.target.value })}
                  />
                </label>
              )}
              {kinds.includes(selected.kind) && (
                <label>
                  分類タグ
                  <textarea
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
                  <ListCount
                    label="子"
                    items={(value.children ?? []) as Ref[]}
                  />
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
                              data-variant="secondary"
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
                              data-variant="secondary"
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
                              data-variant="secondary"
                              disabled={!editable}
                              onClick={() =>
                                edit({
                                  children: array.filter((_, i) => i !== index),
                                })
                              }
                            >
                              参照を外す
                            </button>
                            <button
                              data-variant="secondary"
                              onClick={() => state.add(child, true)}
                            >
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
                      参照候補：{state.reference.name} ·{" "}
                      {refLabel(state.reference.ref)}
                    </p>
                  )}
                </section>
              )}
              {selected.kind === "scope" && (
                <>
                  <p>種類：{labels[String(value.kind)]}</p>
                  <p>
                    親Scope：
                    {value.parent
                      ? identityLabel(value.parent as Identity)
                      : "なし"}
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
                    data-variant="secondary"
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
            <Disclosure title="由来・固定参照・添付・取得状態" defaultOpen>
              {() => (
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
              )}
            </Disclosure>
          )}
          {!historical && draft && (
            <>
              {draft.error && <p role="alert">{draft.error}</p>}
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
                disabled={
                  ui.engine.status !== "current" ||
                  (!editable && !draft.pending) ||
                  ui.engine.sending.has(key) ||
                  !current?.editable
                }
                onClick={() => void save(Boolean(draft.latest))}
              >
                {draft.latest
                  ? "下書きを最新の版へ再適用"
                  : draft.pending
                    ? "保存結果を確認"
                    : "下書きを共有版へ保存"}
              </button>
            </>
          )}
          {ref && ref.revision !== "0" && kinds.includes(ref.kind) && (
            <div className="context-actions">
              <button onClick={() => state.add(ref)}>
                この固定版を入力へ追加
              </button>
              <button
                data-variant="secondary"
                onClick={() => state.add(ref, true)}
              >
                この固定版を入力から除外
              </button>
            </div>
          )}
          {ui.engine.state.receipts[key] && (
            <p className="context-meta">
              保存済みの版：{ui.engine.state.receipts[key]!.revision}
            </p>
          )}
        </>
      )}
      <Disclosure title="版履歴">
        {() => (
          <VersionHistory
            key={key}
            ui={ui}
            selected={selected}
            revision={current?.record.ref.revision}
          />
        )}
      </Disclosure>
    </section>
  );
}
function VersionHistory({
  ui,
  selected,
  revision,
}: {
  ui: UI;
  selected: Identity;
  revision?: string;
}) {
  const [after, setAfter] = useState("");
  const [back, setBack] = useState<string[]>([]);
  const history = useQuery({
    queryKey: [...ui.prefix, "history", identityKey(selected), after, revision],
    enabled: revision !== "0",
    retry: false,
    queryFn: () =>
      ui.request<{ items: Candidate[] }>(
        `/contexts/workspaces/${encodeURIComponent(ui.workspace)}/catalog`,
        { kind: selected.kind, id: selected.id, after },
      ),
  });
  return (
    <>
      <EnvelopeStatus value={history.data} error={history.error} />
      <ListCount
        label="版履歴"
        items={history.data?.data?.items}
        more={history.data?.meta.next_cursor}
      />
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
        data-variant="secondary"
        disabled={!back.length}
        onClick={() => {
          setAfter(back.at(-1)!);
          setBack(back.slice(0, -1));
        }}
      >
        版履歴の前のページ
      </button>
      <button
        data-variant="secondary"
        disabled={!history.data?.meta.next_cursor}
        onClick={() => {
          setBack([...back, after]);
          setAfter(history.data!.meta.next_cursor!);
        }}
      >
        版履歴の次のページ
      </button>
    </>
  );
}
