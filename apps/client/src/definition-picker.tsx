import { useRef, useState } from "react";
import type { DefinitionLabel } from "./workflow-map";

export type DefinitionPage = {
  items: (DefinitionLabel & { value: { name?: string } })[];
  next: string;
};

export function DefinitionPicker({
  load,
  onChoose,
}: {
  load: (after: string) => Promise<DefinitionPage>;
  onChoose: (ref: { id: string; version: string }) => void;
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
  return (
    <>
      <button
        data-variant="secondary"
        onClick={() => {
          dialog.current?.showModal();
          void read("");
        }}
      >
        Run定義を名前で選ぶ
      </button>
      <dialog
        ref={dialog}
        aria-label="Run定義の固定版を選ぶ"
        className="reference-dialog"
      >
        <h4>Run定義の固定版を選ぶ</h4>
        <p>
          50件ずつ取得します。選択では工程の端末内下書きだけを更新し、保存・採用・実行はしません。
        </p>
        <label>
          このページの名前を絞る
          <input value={query} onChange={(e) => setQuery(e.target.value)} />
        </label>
        {busy && <p role="status">取得中</p>}
        {error && <p role="alert">{error}</p>}
        <ul>
          {!busy &&
            page?.items
              .filter((r) => (r.value.name || r.id).includes(query))
              .map((r) => (
                <li key={r.id}>
                  <button
                    onClick={() => {
                      onChoose({ id: r.id, version: r.version });
                      dialog.current?.close();
                    }}
                  >
                    {r.value.name || r.id} · {r.version}
                  </button>
                </li>
              ))}
        </ul>
        {page?.next && (
          <button disabled={busy} onClick={() => void read(page.next)}>
            定義候補の次ページ
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
