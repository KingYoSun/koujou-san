import { useEffect, useState } from "react";
import { syncRequest } from "./auth";
import { NavigationLink, useScreenActive } from "./navigation";

type Connection = { id: string; repository: string; instance?: string };
export function PublicationConnectionPicker({
  native,
  workspace,
  value,
  onChange,
}: {
  native: boolean;
  workspace: string;
  value: string;
  onChange: (id: string) => void;
}) {
  const active = useScreenActive();
  const [items, setItems] = useState<Connection[]>(),
    [selected, setSelected] = useState<Connection>();
  const [next, setNext] = useState(""),
    [after, setAfter] = useState(""),
    [back, setBack] = useState<string[]>([]),
    [notice, setNotice] = useState("");
  async function load(cursor = "") {
    try {
      const page = (await syncRequest(
        `/publications/workspaces/${encodeURIComponent(workspace)}/connections?after=${encodeURIComponent(cursor)}`,
        native,
        { method: "GET" },
      )) as { items: Connection[]; next?: string };
      setItems(page.items);
      const current = page.items.find((item) => item.id === value);
      if (current) setSelected(current);
      setNext(page.next ?? "");
      setAfter(cursor);
      setNotice("");
    } catch {
      setItems(undefined);
      setSelected(undefined);
      setNotice("起票接続を取得できません。");
    }
  }
  useEffect(() => {
    if (active && !items) void load();
  }, [active]);
  return (
    <div className="context-actions">
      <label>
        起票先リポジトリ
        <select
          value={value}
          onChange={(event) => {
            onChange(event.target.value);
            setSelected(items?.find((item) => item.id === event.target.value));
          }}
        >
          <option value="">未選択</option>
          {value && !items?.some((item) => item.id === value) && (
            <option value={value}>
              {selected?.id === value ? selected.repository : value}
            </option>
          )}
          {items?.map((item) => (
            <option key={item.id} value={item.id}>
              {item.repository} · {item.instance ?? "GitHub"}
            </option>
          ))}
        </select>
      </label>
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
      <button
        type="button"
        onClick={() => {
          setBack([]);
          void load();
        }}
      >
        起票接続を再取得
      </button>
      <NavigationLink related to={{ workspace, screen: "repositories" }}>
        起票接続を設定
      </NavigationLink>
      {notice && <p role="status">{notice}</p>}
    </div>
  );
}
