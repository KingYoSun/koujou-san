import { useEffect, useReducer, useRef, useState } from "react";
import { syncRequest } from "./auth";
import { SyncEngine } from "./sync-engine";
import type { SyncItem } from "./sync-engine";
import { syncStorage } from "./sync-store";

const labels: Record<string, string> = {
  sentence: "文",
  paragraph: "段落",
  section: "節",
  document: "文書",
  scope: "Scope",
  membership: "所属",
  baseline: "基準版",
  manifest: "確定入力",
  addition: "追加情報",
  report: "実行報告",
};
function title(item: SyncItem) {
  const value = item.record.value;
  return String(
    value.name || value.title || value.text || item.record.ref.id,
  ).slice(0, 70);
}
function editableField(item: SyncItem): string | undefined {
  switch (item.record.ref.kind) {
    case "sentence":
      return typeof item.record.value.text === "string" ? "text" : undefined;
    case "scope":
    case "paragraph":
      return "name";
    case "section":
    case "document":
      return "title";
  }
}
function description(item: SyncItem) {
  const value = item.record.value;
  if (item.record.ref.kind === "membership")
    return value.present ? "所属しています" : "所属を解除しました";
  if (item.record.ref.kind === "report")
    return `連続して取得した報告: ${String(value.contiguous_through)} / 最後に観測した順序: ${String(value.highest_observed)}。${value.missing ? "欠落した報告があります。" : "観測済みの範囲に欠落はありません。"} 実行結果は未確認です。`;
  return String(
    value.text ||
      value.title ||
      value.name ||
      (value.external ? "固定した外部参照" : ""),
  );
}
export function SyncPanel({
  native,
  api,
  workspace,
  user,
  onRevoked,
}: {
  native: boolean;
  api: string;
  workspace: string;
  user: string;
  onRevoked: () => void;
}) {
  const [, render] = useReducer((n) => n + 1, 0);
  const [engine, setEngine] = useState<SyncEngine>();
  const [selected, setSelected] = useState("");
  const [storageError, setStorageError] = useState("");
  const revokedCallback = useRef(onRevoked);
  revokedCallback.current = onRevoked;
  useEffect(() => {
    let notified = false;
    const current = new SyncEngine(
      { api, user, workspace },
      syncStorage(native),
      (path, method, body) => syncRequest(path, native, { method, body }),
      () => {
        render();
        if (current.status === "revoked" && !notified) {
          notified = true;
          revokedCallback.current();
        }
      },
    );
    setEngine(current);
    void current.start();
    const timer = setInterval(() => void current.refresh(), 2000);
    const online = () => void current.refresh();
    window.addEventListener("online", online);
    return () => {
      clearInterval(timer);
      window.removeEventListener("online", online);
      void current.stop();
    };
  }, [api, native, user, workspace]);
  if (!engine)
    return (
      <section aria-label="共有と下書き">
        <h3>共有と下書き</h3>
        <p>保存した状態を確認しています…</p>
      </section>
    );
  const state = engine.state;
  const keys = [
    ...new Set([...Object.keys(state.items), ...Object.keys(state.drafts)]),
  ].sort();
  const key = keys.includes(selected) ? selected : (keys[0] ?? "");
  const current = state.items[key] ?? state.drafts[key]?.base;
  const draft = state.drafts[key];
  const field = current ? editableField(current) : undefined;
  const value = draft?.value ?? current?.record.value;
  const canEdit = Boolean(
    current?.editable && !draft?.pending && !engine.sending.has(key),
  );
  const error = () =>
    setStorageError(
      "この端末への下書き保存を確認できません。保存領域を確認してください。",
    );
  const submit = (reapply = false) => {
    setStorageError("");
    void engine.send(key, reapply).catch(error);
  };
  return (
    <section className="shared-sync" aria-labelledby="sync-title">
      <h3 id="sync-title">共有と下書き</h3>
      <p role="status">
        {engine.status === "current"
          ? "同期済み"
          : engine.status === "checking"
            ? "同期を確認中"
            : engine.status === "revoked"
              ? "利用権限が失効しました"
              : "接続できません。キャッシュは古い可能性があります。"}
      </p>
      <p>
        最終同期:{" "}
        {state.confirmedAt
          ? new Date(state.confirmedAt).toLocaleString()
          : "未確認"}
      </p>
      {engine.notice && <p role="status">{engine.notice}</p>}
      {storageError && <p role="alert">{storageError}</p>}
      {!engine.canStart() && (
        <p>接続を確認できるまで、新しい実行・外部起票を開始できません。</p>
      )}
      <div className="team-controls">
        <button
          disabled={engine.busy || engine.status === "revoked"}
          onClick={() => void engine.refresh()}
        >
          接続と更新を確認
        </button>
        <button
          disabled={engine.busy || engine.status !== "current"}
          onClick={() => void engine.refresh(true)}
        >
          {engine.available ? "一覧の更新を確認" : "現在の一覧を取得し直す"}
        </button>
        {state.nextCursor && (
          <button
            disabled={engine.busy || engine.status !== "current"}
            onClick={() => void engine.more()}
          >
            さらに表示
          </button>
        )}
      </div>
      {state.nextCursor && (
        <p>一覧の一部を表示しています。続きは「さらに表示」で取得できます。</p>
      )}
      {keys.length ? (
        <>
          <label>
            同期する項目
            <select
              aria-label="同期する項目"
              value={key}
              onChange={(e) => setSelected(e.target.value)}
            >
              {keys.map((k) => {
                const item = state.items[k] ?? state.drafts[k]!.base;
                return (
                  <option value={k} key={k}>
                    {labels[item.record.ref.kind]}: {title(item)}
                    {state.drafts[k] ? "（下書きあり）" : ""}
                  </option>
                );
              })}
            </select>
          </label>
          {current && (
            <>
              <p>
                確認した版: <code>{current.record.ref.revision}</code>
              </p>
              <p className="synced-content">{description(current)}</p>
              {Array.isArray(current.record.value.children) && (
                <p>子の参照: {current.record.value.children.length}件</p>
              )}
              {field && (
                <label>
                  {field === "text"
                    ? "本文の下書き"
                    : field === "title"
                      ? "見出しの下書き"
                      : "名前の下書き"}
                  <textarea
                    rows={5}
                    disabled={!canEdit}
                    value={String(value?.[field] ?? "")}
                    onChange={(e) =>
                      void engine
                        .edit(key, { ...value, [field]: e.target.value })
                        .catch(error)
                    }
                  />
                </label>
              )}
              {current.record.ref.kind === "membership" && (
                <label>
                  <input
                    type="checkbox"
                    checked={Boolean(value?.present)}
                    disabled={!canEdit}
                    onChange={(e) =>
                      void engine
                        .edit(key, { ...value, present: e.target.checked })
                        .catch(error)
                    }
                  />
                  このScopeへ所属させる（下書き）
                </label>
              )}
              {!current.editable && <p>この項目は閲覧のみです。</p>}
              {draft && (
                <div className="sync-draft">
                  {draft.error && <p role="alert">{draft.error}</p>}
                  <p>
                    {draft.pending
                      ? "保存結果が未確認です。内容と操作IDを保持しています。"
                      : draft.latest
                        ? "競合しています。下書きは共有状態へ反映していません。"
                        : "未送信の下書き（この端末のみ）"}
                  </p>
                  {draft.latest && (
                    <>
                      <h4>サーバーの最新内容</h4>
                      <p className="synced-content">
                        {description(draft.latest)}
                      </p>
                      <button
                        disabled={
                          engine.status !== "current" ||
                          engine.sending.has(key) ||
                          !draft.base.editable
                        }
                        onClick={() => submit(true)}
                      >
                        この下書きを最新の版へ再適用して送信
                      </button>
                    </>
                  )}
                  {!draft.latest && (
                    <button
                      disabled={
                        engine.status !== "current" ||
                        engine.sending.has(key) ||
                        !draft.base.editable
                      }
                      onClick={() => submit()}
                    >
                      {draft.pending
                        ? "同じ保存要求を再送して結果を確認"
                        : "内容を確認して送信"}
                    </button>
                  )}
                </div>
              )}
              {state.receipts[key] && (
                <p>
                  最後に受付を確認した保存版:{" "}
                  <code>{state.receipts[key]!.revision}</code>
                </p>
              )}
            </>
          )}
        </>
      ) : (
        <p>この一覧に表示する項目はありません。</p>
      )}
      <p>
        再接続しても下書きは自動送信しません。サインアウト・取得権限の失効時は、該当する本文キャッシュと下書きを削除します。
      </p>
    </section>
  );
}
