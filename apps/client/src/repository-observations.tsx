import { useEffect, useState } from "react";
import { NavigationLink, ScreenContent, useScreenActive } from "./navigation";

type Draft = {
  id: string;
  version: string;
  active: boolean;
  kind: string;
  external_id: string;
  workflow: string;
  event: string;
  expected_commit: string;
  required_jobs: string;
};
type Config = {
  target: {
    id: string;
    kind: string;
    external_id: string;
    workflow?: string;
    event?: string;
  };
  binding: { expected_commit: string; required_jobs?: string[] };
  active: boolean;
  version: string;
};
export function RepositoryObservations({
  id,
  workspace,
  enabled,
  busy,
  call,
  write,
  work,
}: {
  id: string;
  workspace: string;
  enabled: boolean;
  busy: boolean;
  call: <T>(path: string) => Promise<T>;
  write: (path: string, version: string, value: object) => Promise<string>;
  work: (action: () => Promise<void>) => Promise<void>;
}) {
  const active = useScreenActive() && enabled;
  const [items, setItems] = useState<Config[]>(),
    [next, setNext] = useState("");
  const [draft, setDraft] = useState<Draft>();
  const [notice, setNotice] = useState("");
  const base = `/repositories/${encodeURIComponent(id)}/observations`;
  async function load(after = "") {
    const page = await call<{ items: Config[]; next?: string }>(
      `${base}?after=${encodeURIComponent(after)}`,
    );
    setItems(page.items);
    setNext(page.next ?? "");
  }
  useEffect(() => {
    if (active && !items) void work(() => load());
  }, [active]);
  function choose(value: Config) {
    setDraft({
      id: value.target.id.replace(/^gitea:/, ""),
      version: value.version,
      active: value.active,
      kind: value.target.kind,
      external_id: value.target.external_id,
      workflow: value.target.workflow ?? "",
      event: value.target.event ?? "",
      expected_commit: value.binding.expected_commit,
      required_jobs: (value.binding.required_jobs ?? []).join("\n"),
    });
    setNotice("");
  }
  function fresh() {
    setDraft({
      id: crypto.randomUUID(),
      version: "0",
      active: true,
      kind: "actions",
      external_id: "",
      workflow: "",
      event: "",
      expected_commit: "",
      required_jobs: "",
    });
    setNotice("");
  }
  async function save() {
    if (!draft) return;
    const version = await write(
      `${base}/${encodeURIComponent(draft.id)}`,
      draft.version,
      {
        active: draft.active,
        kind: draft.kind,
        external_id: draft.external_id,
        workflow: draft.workflow,
        event: draft.event,
        expected_commit: draft.expected_commit,
        required_jobs: draft.required_jobs.split(/\r?\n/).filter(Boolean),
      },
    );
    setDraft({ ...draft, version });
    await load();
    setNotice("観測対象を保存しました。");
  }
  return (
    <ScreenContent>
      {enabled && (
        <section aria-label="リポジトリの観測設定">
          <h3>観測対象</h3>
          <button disabled={busy} onClick={() => void work(() => load())}>
            観測対象を再取得
          </button>
          <ul>
            {items?.map((item) => (
              <li key={item.target.id}>
                <button onClick={() => choose(item)}>
                  {item.target.kind} ·{" "}
                  {item.target.workflow || item.target.external_id} ·{" "}
                  {item.active ? "有効" : "停止中"}
                </button>
              </li>
            ))}
          </ul>
          {next && (
            <button disabled={busy} onClick={() => void work(() => load(next))}>
              観測対象の次のページ
            </button>
          )}
          <button disabled={busy} onClick={fresh}>
            観測対象を追加
          </button>
          {draft && (
            <fieldset disabled={busy}>
              <legend>
                {draft.version === "0" ? "新しい観測対象" : "観測対象の編集"}
              </legend>
              <label>
                <input
                  type="checkbox"
                  checked={draft.active}
                  onChange={(event) =>
                    setDraft({ ...draft, active: event.target.checked })
                  }
                />
                観測を有効にする
              </label>
              <label>
                種類
                <select
                  value={draft.kind}
                  disabled={draft.version !== "0"}
                  onChange={(event) =>
                    setDraft({
                      ...draft,
                      kind: event.target.value,
                      external_id: "",
                      workflow: "",
                      event: "",
                    })
                  }
                >
                  {[
                    ["issue", "Issue"],
                    ["pr", "PR"],
                    ["commit", "commit"],
                    ["actions", "Actions"],
                    ["status", "commitのstatus"],
                  ].map(([kind, label]) => (
                    <option key={kind} value={kind}>
                      {label}
                    </option>
                  ))}
                </select>
              </label>
              <label>
                外部対象ID
                <input
                  disabled={draft.version !== "0"}
                  value={draft.external_id}
                  onChange={(event) =>
                    setDraft({ ...draft, external_id: event.target.value })
                  }
                />
              </label>
              {draft.kind === "actions" && (
                <>
                  <label>
                    Workflowファイル
                    <input
                      disabled={draft.version !== "0"}
                      value={draft.workflow}
                      onChange={(event) =>
                        setDraft({ ...draft, workflow: event.target.value })
                      }
                    />
                  </label>
                  <label>
                    イベント
                    <input
                      disabled={draft.version !== "0"}
                      value={draft.event}
                      onChange={(event) =>
                        setDraft({ ...draft, event: event.target.value })
                      }
                    />
                  </label>
                </>
              )}
              <label>
                期待するcommit
                <input
                  value={draft.expected_commit}
                  onChange={(event) =>
                    setDraft({ ...draft, expected_commit: event.target.value })
                  }
                />
              </label>
              <label>
                必須job
                <textarea
                  rows={3}
                  value={draft.required_jobs}
                  onChange={(event) =>
                    setDraft({ ...draft, required_jobs: event.target.value })
                  }
                />
              </label>
              <button
                disabled={!draft.external_id && !draft.workflow}
                onClick={() => void work(save)}
              >
                観測対象を保存
              </button>
            </fieldset>
          )}
          <NavigationLink related to={{ workspace, screen: "github" }}>
            観測を開く
          </NavigationLink>
          {notice && <p role="status">{notice}</p>}
        </section>
      )}
    </ScreenContent>
  );
}
