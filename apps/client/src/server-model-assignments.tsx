import { useEffect, useState } from "react";
import { syncRequest } from "./auth";
import { useScreenActive } from "./navigation";
import type { ModelConnection } from "./server-model";

type Assignment = ModelConnection & { allowed: boolean; version: string };
type Page = { items: Assignment[]; next?: string };
export function ServerModelAssignments({
  native,
  workspace,
  onSaved,
}: {
  native: boolean;
  workspace: string;
  onSaved: () => Promise<void>;
}) {
  const active = useScreenActive();
  const [page, setPage] = useState<Page>(),
    [busy, setBusy] = useState(false),
    [notice, setNotice] = useState("");
  const base = `/server-models/workspaces/${encodeURIComponent(workspace)}`;
  async function load(after = "") {
    setPage(
      (await syncRequest(`${base}?after=${encodeURIComponent(after)}`, native, {
        method: "GET",
      })) as Page,
    );
  }
  async function work(action: () => Promise<void>) {
    setBusy(true);
    setNotice("");
    try {
      await action();
    } catch {
      setPage(undefined);
      setNotice(
        "サーバーLLMの割当を確認できません。権限・現在版を確認してください。",
      );
    } finally {
      setBusy(false);
    }
  }
  useEffect(() => {
    if (active) void work(() => load());
  }, [active]);
  async function assign(item: Assignment) {
    const result = (await syncRequest(
      `${base}/${encodeURIComponent(item.id)}`,
      native,
      {
        method: "POST",
        body: { allowed: !item.allowed, expected_version: item.version },
      },
    )) as { version: string };
    setPage(
      (current) =>
        current && {
          ...current,
          items: current.items.map((c) =>
            c.id === item.id
              ? { ...c, allowed: !item.allowed, version: result.version }
              : c,
          ),
        },
    );
    await onSaved();
  }
  return (
    <section aria-label="WorkspaceへのLLM割当">
      <h3>Workspaceへの割当</h3>
      <button disabled={busy} onClick={() => void work(() => load())}>
        登録済みLLM接続を再取得
      </button>
      {page?.items.length === 0 && (
        <p role="status">登録済みのサーバーLLM接続はありません。</p>
      )}
      <ul>
        {page?.items.map((item) => (
          <li key={item.id}>
            <span>
              {item.name} · {item.allowed ? "割当済み" : "未割当"}
            </span>
            <button
              disabled={busy}
              onClick={() => void work(() => assign(item))}
            >
              {item.allowed
                ? `${item.name}の割当を取り消す`
                : `${item.name}を割り当てる`}
            </button>
          </li>
        ))}
      </ul>
      {page?.next && (
        <button
          disabled={busy}
          onClick={() => void work(() => load(page.next))}
        >
          登録済みLLM接続の次のページ
        </button>
      )}
      {notice && <p role="status">{notice}</p>}
    </section>
  );
}
