import { useRef, useState } from "react";
import type { DefinitionLabel } from "./workflow-map";

export type DefinitionPage = {
  items: (DefinitionLabel & { value: { name?: string } })[];
  next: string;
};

export function DefinitionPicker({
  load,
  onChoose,
  label = "Run定義",
}: {
  load: (after: string) => Promise<DefinitionPage>;
  onChoose: (ref: { id: string; version: string }, name?: string) => void;
  label?: string;
}) {
  const dialog = useRef<HTMLDialogElement>(null);
  const [page, setPage] = useState<DefinitionPage>();
  const [query, setQuery] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const read = async (after: string) => {
    setBusy(true);
    setError("");
    try {
      setPage(await load(after));
      setQuery("");
    } catch {
      setError(
        "定義一覧を取得できません。現在の権限と接続を確認してください。",
      );
    } finally {
      setBusy(false);
    }
  };
  const items =
    page?.items.filter((r) => (r.value.name || r.id).includes(query)) ?? [];
  return (
    <>
      <button
        data-variant="secondary"
        onClick={() => {
          dialog.current?.showModal();
          void read("");
        }}
      >
        {label}を名前で選ぶ
      </button>
      <dialog
        ref={dialog}
        aria-label={`${label}の固定版を選ぶ`}
        className="reference-dialog"
      >
        <h4>{label}の固定版を選ぶ</h4>
        <label>
          このページの名前を絞る
          <input value={query} onChange={(e) => setQuery(e.target.value)} />
        </label>
        {busy && <p role="status">定義候補を取得中…</p>}
        {error && <p role="alert">{error}</p>}
        {page && (
          <p>
            {items.length}件を表示{page.next ? "（続きあり）" : ""}
          </p>
        )}
        {page && !items.length && <p>該当する定義はありません。</p>}
        <ul>
          {items.map((r) => (
            <li key={r.id}>
              <button
                onClick={() => {
                  onChoose({ id: r.id, version: r.version }, r.value.name);
                  dialog.current?.close();
                }}
              >
                {r.value.name || r.id} · {r.version}
              </button>
            </li>
          ))}
        </ul>
        {page?.next && (
          <button
            data-variant="secondary"
            disabled={busy}
            aria-busy={busy}
            onClick={() => void read(page.next)}
          >
            定義候補の次のページ
          </button>
        )}
        <button
          data-variant="secondary"
          onClick={() => dialog.current?.close()}
        >
          選択せず閉じる
        </button>
      </dialog>
    </>
  );
}
