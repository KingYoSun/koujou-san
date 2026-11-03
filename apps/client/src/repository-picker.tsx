import { useEffect, useState } from "react";
import { syncRequest } from "./auth";
import { NavigationLink, useScreenActive } from "./navigation";

export type Repository = {
  id: string;
  connection: string;
  connection_version: string;
  instance: string;
  full_name: string;
  repository_id: number;
  project?: string;
  private: boolean;
  auto_init: boolean;
  state: string;
  version: string;
};
export const repositoryStates: Record<string, string> = {
  registered: "登録済み",
  created: "作成済み",
  confirmed: "取得で確認済み",
  intent: "作成の送信意図を保存済み",
  unknown: "作成結果未確認",
  rejected: "作成拒否",
};
export function RepositoryPicker({
  native,
  workspace,
  value,
  onChange,
  project = "",
}: {
  native: boolean;
  workspace: string;
  value: string;
  onChange: (id: string) => void;
  project?: string;
}) {
  const active = useScreenActive();
  const [items, setItems] = useState<Repository[]>(),
    [selected, setSelected] = useState<Repository>();
  const [next, setNext] = useState(""),
    [after, setAfter] = useState(""),
    [back, setBack] = useState<string[]>([]),
    [notice, setNotice] = useState("");
  const base = `/forge/workspaces/${encodeURIComponent(workspace)}/repositories`;
  async function load(cursor = "") {
    try {
      const page = (await syncRequest(
        `${base}?project=${encodeURIComponent(project)}&after=${encodeURIComponent(cursor)}`,
        native,
        { method: "GET" },
      )) as { items: Repository[]; next?: string };
      setItems(page.items);
      setNext(page.next ?? "");
      setAfter(cursor);
      setNotice("");
    } catch {
      setItems(undefined);
      setNotice("リポジトリを取得できません。");
    }
  }
  useEffect(() => {
    if (active) void load();
  }, [active, project]);
  useEffect(() => {
    let alive = true;
    if (value && value !== selected?.id)
      void syncRequest(`${base}/${encodeURIComponent(value)}`, native, {
        method: "GET",
      })
        .then((result) => {
          if (alive) setSelected(result as Repository);
        })
        .catch(() => {
          if (alive) setSelected(undefined);
        });
    return () => {
      alive = false;
    };
  }, [value]);
  return (
    <div className="context-actions">
      <label>
        リポジトリ
        <select
          value={value}
          onChange={(event) => {
            onChange(event.target.value);
            setSelected(items?.find((item) => item.id === event.target.value));
          }}
        >
          <option value="">未指定</option>
          {value && !items?.some((item) => item.id === value) && (
            <option value={value}>{selected?.full_name ?? value}</option>
          )}
          {items
            ?.filter((item) => item.repository_id > 0)
            .map((item) => (
              <option key={item.id} value={item.id}>
                {item.full_name}
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
      <NavigationLink
        related
        to={{
          workspace,
          screen: "repositories",
          ...(project
            ? { target: { kind: "scope", id: project, version: "head" } }
            : {}),
        }}
      >
        リポジトリを設定
      </NavigationLink>
      {notice && <p role="status">{notice}</p>}
    </div>
  );
}
