import { useId, useState, type ReactNode } from "react";

export function PageList<T>({
  label,
  items,
  children,
  listClassName,
}: {
  label: string;
  items: readonly T[];
  children: (item: T, index: number) => ReactNode;
  listClassName?: string;
}) {
  const id = useId();
  const [page, setPage] = useState(0);
  const start =
    Math.min(page, Math.max(0, Math.ceil(items.length / 100) - 1)) * 100;
  return (
    <div className="page-list">
      <p aria-live="polite">
        {label}：
        {items.length
          ? `${start + 1}–${Math.min(start + 100, items.length)} / ${items.length}件`
          : "0件"}
      </p>

      {items.length > 100 && (
        <div className="team-controls">
          <button
            data-variant="secondary"
            aria-controls={id}
            disabled={start === 0}
            onClick={() => setPage(Math.max(0, start / 100 - 1))}
          >
            {label}の前のページ
          </button>
          <button
            data-variant="secondary"
            aria-controls={id}
            disabled={start + 100 >= items.length}
            onClick={() => setPage(start / 100 + 1)}
          >
            {label}の次のページ
          </button>
        </div>
      )}
      <ol id={id} className={listClassName} start={start + 1}>
        {items
          .slice(start, start + 100)
          .map((item, index) => children(item, start + index))}
      </ol>
    </div>
  );
}
