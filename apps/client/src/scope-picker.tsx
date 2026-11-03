import { useEffect, useState } from "react";
import { syncRequest } from "./auth";
import { useScreenActive } from "./navigation";
import { labels, type Candidate, type Ref } from "./context-ui";

type Props = {
  native: boolean;
  workspace: string;
  kind: "project" | "issue";
  value: string;
  onChange: (value: string, ref?: Ref) => void;
  channels?: boolean;
};
export function ScopePicker(props: Props) {
  return useScopePicker(props);
}

// The chat owns this hook so its filters/page survive trips to related settings.
export function useScopePicker({
  native,
  workspace,
  kind,
  value,
  onChange,
  channels = false,
}: Props) {
  const active = useScreenActive();
  const [text, setText] = useState(""),
    [query, setQuery] = useState("");
  const [items, setItems] = useState<Candidate[]>();
  const [next, setNext] = useState(""),
    [after, setAfter] = useState(""),
    [back, setBack] = useState<string[]>([]);
  const [busy, setBusy] = useState(false),
    [notice, setNotice] = useState("");
  async function load(cursor = "", search = query) {
    setBusy(true);
    setNotice("");
    try {
      const page = (await syncRequest(
        `/contexts/workspaces/${encodeURIComponent(workspace)}/catalog`,
        native,
        {
          method: "POST",
          body: { kind: "scope", scope_kind: kind, search, after: cursor },
        },
      )) as { data: { items: Candidate[] }; meta: { next_cursor?: string } };
      setItems(page.data.items);
      setNext(page.meta.next_cursor ?? "");
      setAfter(cursor);
      setQuery(search);
    } catch {
      setItems(undefined);
      setNotice(`${labels[kind]}を取得できません。`);
    } finally {
      setBusy(false);
    }
  }
  useEffect(() => {
    if (active) void load(after, query);
  }, [active]);
  function search() {
    setBack([]);
    void load("", text);
  }
  return (
    <fieldset disabled={busy}>
      <legend>{labels[kind]}</legend>
      <label>
        {labels[kind]}名
        <input
          value={text}
          onChange={(event) => setText(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === "Enter") {
              event.preventDefault();
              search();
            }
          }}
        />
      </label>
      <button type="button" onClick={search}>
        {labels[kind]}を検索
      </button>
      {channels ? (
        <>
          <button type="button" onClick={() => void load("", query)}>
            {labels[kind]}を再取得
          </button>
          <ul className="chat-channels">
            {items?.map((item) => (
              <li key={item.ref.id}>
                <button
                  type="button"
                  aria-current={value === item.ref.id ? "true" : undefined}
                  onClick={() => onChange(item.ref.id, item.ref)}
                >
                  {item.name || item.ref.id}
                </button>
              </li>
            ))}
          </ul>
          {items && !items.length && (
            <p>一致する{labels[kind]}はありません。</p>
          )}
        </>
      ) : (
        <label>
          {labels[kind]}
          <select
            value={value}
            onChange={(event) =>
              onChange(
                event.target.value,
                items?.find((item) => item.ref.id === event.target.value)?.ref,
              )
            }
          >
            <option value="">未指定</option>
            {value && !items?.some((item) => item.ref.id === value) && (
              <option value={value}>{value}</option>
            )}
            {items?.map((item) => (
              <option key={item.ref.id} value={item.ref.id}>
                {item.name || item.ref.id}
              </option>
            ))}
          </select>
        </label>
      )}
      <button
        type="button"
        disabled={!back.length}
        onClick={() => {
          const cursor = back.at(-1)!;
          setBack(back.slice(0, -1));
          void load(cursor);
        }}
      >
        前のページ
      </button>
      <button
        type="button"
        disabled={!next}
        onClick={() => {
          setBack([...back, after]);
          void load(next);
        }}
      >
        次のページ
      </button>
      {notice && <p role="status">{notice}</p>}
    </fieldset>
  );
}
