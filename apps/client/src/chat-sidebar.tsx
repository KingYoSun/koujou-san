import { useEffect, useState } from "react";
import { Disclosure } from "./disclosure";
import { NavigationLink } from "./navigation";
import { ScopePicker } from "./scope-picker";
import type { ChatPage, ChatThread } from "./chat-api";
import type { VersionRef } from "./model-connections";

export function ChatSidebar({
  native,
  workspace,
  role,
  project,
  projectName,
  thread,
  threads,
  busy,
  embedded,
  onProject,
  onNew,
  onOpen,
  onList,
}: {
  native: boolean;
  workspace: string;
  role: string;
  project?: VersionRef;
  projectName: string;
  thread?: VersionRef;
  threads?: ChatPage;
  busy: boolean;
  embedded: boolean;
  onProject: (ref: VersionRef) => void;
  onNew: () => void;
  onOpen: (ref: VersionRef) => void;
  onList: (query: string, after?: ChatPage) => void;
}) {
  const [search, setSearch] = useState("");
  const [historyOpen, setHistoryOpen] = useState(false);
  useEffect(() => {
    const media = window.matchMedia("(min-width: 1121px)");
    const resize = () => setHistoryOpen(!embedded && media.matches);
    resize();
    media.addEventListener("change", resize);
    return () => media.removeEventListener("change", resize);
  }, [embedded]);
  const canEdit = role !== "viewer";
  return (
    <aside className="chat-sidebar" aria-label="Projectとスレッド">
      <Disclosure title={projectName || "Projectを選択"}>
        {() => (
          <>
            <ScopePicker
              native={native}
              workspace={workspace}
              kind="project"
              value={project?.id.replace(/^scope:/, "") ?? ""}
              channels
              onChange={(id, ref) =>
                onProject({
                  id: "scope:" + id,
                  version: ref?.revision ?? "head",
                })
              }
            />
            {canEdit && (
              <NavigationLink
                related
                to={{
                  workspace,
                  screen: "context",
                  view: "scopes",
                  query: "project",
                }}
              >
                Projectを作成
              </NavigationLink>
            )}
          </>
        )}
      </Disclosure>
      {canEdit && (
        <button
          className="chat-new"
          disabled={busy || !project}
          onClick={onNew}
        >
          ＋ 新しい相談
        </button>
      )}
      <details
        className="chat-history"
        open={historyOpen}
        onToggle={(e) => setHistoryOpen(e.currentTarget.open)}
      >
        <summary>相談履歴</summary>
        <form
          className="chat-search"
          onSubmit={(e) => {
            e.preventDefault();
            onList(search);
          }}
        >
          <label>
            相談を検索
            <input value={search} onChange={(e) => setSearch(e.target.value)} />
          </label>
          <button data-variant="secondary" disabled={busy || !project}>
            検索
          </button>
        </form>
        <ul className="chat-channels">
          {threads?.items.map((r) => (
            <li key={r.id}>
              <button
                disabled={busy}
                title={(r.value as ChatThread).name}
                aria-current={thread?.id === r.id ? "true" : undefined}
                onClick={() => onOpen(r)}
              >
                {(r.value as ChatThread).name}
              </button>
            </li>
          ))}
        </ul>
        {threads && !threads.items.length && (
          <p className="chat-source">まだ相談はありません。</p>
        )}
        {threads?.items.length === 50 && (
          <button
            data-variant="secondary"
            disabled={busy}
            onClick={() => onList(search, threads)}
          >
            以前の相談を表示
          </button>
        )}
      </details>
      {project && (
        <Disclosure title="Projectの設定">
          {() => (
            <div className="chat-settings-links">
              <NavigationLink
                related
                to={{
                  workspace,
                  screen: "repositories",
                  target: {
                    kind: "scope",
                    id: project.id.replace(/^scope:/, ""),
                    version: project.version,
                  },
                }}
              >
                リポジトリ接続
              </NavigationLink>
              <NavigationLink related to={{ workspace, screen: "execution" }}>
                CLI・Runner
              </NavigationLink>
              <NavigationLink related to={{ workspace, screen: "devices" }}>
                端末・共有ポリシー
              </NavigationLink>
              {role === "admin" && (
                <NavigationLink
                  related
                  to={{
                    workspace,
                    screen: "workspace",
                    view: "resource-policy",
                    target: {
                      kind: "resource",
                      id: project.id,
                      version: "head",
                    },
                  }}
                >
                  Projectの共有設定
                </NavigationLink>
              )}
            </div>
          )}
        </Disclosure>
      )}
    </aside>
  );
}
