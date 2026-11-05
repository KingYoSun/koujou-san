import { useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import type { Ref, UI } from "./context-ui";
import { NavigationLink, useScreenActive } from "./navigation";

export function ProjectBootstrap({
  ui,
  active = true,
}: {
  ui: UI;
  active?: boolean;
}) {
  const visible = useScreenActive() && active;
  const [name, setName] = useState("");
  const [text, setText] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [result, setResult] = useState<{ context: Ref }>();
  const keys = useRef<string[]>([]);
  const saved = useRef<Ref[]>([]);
  const working = useRef(false);
  const client = useQueryClient();

  async function create() {
    if (working.current || !ui.canCreate || !name.trim() || !text.trim())
      return;
    working.current = true;
    setBusy(true);
    setError("");
    try {
      async function save(
        index: number,
        kind: string,
        value: Record<string, unknown>,
      ): Promise<Ref> {
        if (saved.current[index]) return saved.current[index];
        const key = (keys.current[index] ??= await ui.engine.create(
          kind,
          value,
        ));
        if (!ui.engine.state.receipts[key]) await ui.engine.send(key);
        const receipt = ui.engine.state.receipts[key];
        if (!receipt)
          throw Error(
            ui.engine.state.drafts[key]?.error ?? "保存結果を確認できません。",
          );
        return (saved.current[index] = {
          kind,
          id: key.slice(key.indexOf(":") + 1),
          revision: receipt.revision,
        });
      }
      const context = await save(0, "sentence", {
        kind: "sentence",
        name: `${name.trim()}の初期Context`,
        text,
      });
      const project = await save(1, "scope", {
        kind: "project",
        name: name.trim(),
      });
      await save(2, "membership", {
        scope: { kind: "project", id: project.id },
        context: { kind: context.kind, id: context.id },
        present: true,
      });
      setResult({ context });
      ui.store.getState().choose(project);
      await client.invalidateQueries({ queryKey: ui.prefix });
    } catch (cause) {
      setError(`作成は未完了です。${String(cause)}`);
    } finally {
      working.current = false;
      setBusy(false);
    }
  }

  if (!visible) return null;
  return (
    <section className="context-editor" aria-label="初期ContextとProjectの作成">
      <h3>初期ContextとProjectを作成</h3>
      {result ? (
        <>
          <p role="status">初期ContextとProjectへの所属を保存しました。</p>
          <NavigationLink
            to={{
              workspace: ui.workspace,
              screen: "context",
              view: "documents",
              target: {
                kind: result.context.kind,
                id: result.context.id,
                version: result.context.revision,
              },
            }}
          >
            初期Contextを編集
          </NavigationLink>
        </>
      ) : (
        <form
          onSubmit={(event) => {
            event.preventDefault();
            void create();
          }}
        >
          {!ui.canCreate && <p role="status">編集権限がありません。</p>}
          <fieldset disabled={!ui.canCreate || busy || keys.current.length > 0}>
            <legend>Projectと初期Context</legend>
            <label>
              Project名
              <input
                required
                value={name}
                onChange={(event) => setName(event.target.value)}
              />
            </label>
            <label>
              入力ファイル（.txt・.md・.html）
              <input
                type="file"
                accept=".txt,.md,.html"
                onChange={async (event) => {
                  const file = event.target.files?.[0];
                  event.target.value = "";
                  if (!file) return;
                  setError("");
                  if (!/\.(txt|md|html)$/i.test(file.name)) {
                    setError(".txt・.md・.htmlのファイルを選んでください。");
                    return;
                  }
                  setBusy(true);
                  working.current = true;
                  try {
                    setText(await file.text());
                    if (!name.trim())
                      setName(file.name.replace(/\.[^.]+$/, ""));
                  } catch {
                    setError("入力ファイルを読み込めませんでした。");
                  } finally {
                    working.current = false;
                    setBusy(false);
                  }
                }}
              />
            </label>
            <label>
              初期Contextの本文
              <textarea
                required
                rows={8}
                value={text}
                onChange={(event) => setText(event.target.value)}
              />
            </label>
          </fieldset>
          {error && <p role="alert">{error}</p>}
          <button
            disabled={
              !ui.canCreate ||
              busy ||
              ui.engine.status !== "current" ||
              !name.trim() ||
              !text.trim()
            }
            aria-busy={busy}
          >
            {busy
              ? "作成中…"
              : keys.current.length
                ? "作成結果を確認して続ける"
                : "初期ContextとProjectを保存"}
          </button>
        </form>
      )}
    </section>
  );
}
