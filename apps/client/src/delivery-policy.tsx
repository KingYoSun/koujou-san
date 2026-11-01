import { useEffect, useRef, useState } from "react";
import { apiRequest } from "./auth";

type Policy = { denied_tags: string[]; denied_scopes: string[] };
type Snapshot = { value: Policy; version: string };
const lines = (value: string) => [
  ...new Set(
    value
      .split(/\r?\n/)
      .map((v) => v.trim())
      .filter(Boolean),
  ),
];

export function DeliveryPolicy({
  native,
  workspace,
}: {
  native: boolean;
  workspace: string;
}) {
  const [snapshot, setSnapshot] = useState<Snapshot>();
  const [tags, setTags] = useState("");
  const [scopes, setScopes] = useState("");
  const [notice, setNotice] = useState("");
  const [busy, setBusy] = useState(false);
  const pending = useRef<
    | {
        operation_id: string;
        expected_version: string;
        value: Policy;
      }
    | undefined
  >(undefined);
  const path = `/mcp/control/workspaces/${encodeURIComponent(workspace)}/policy`;
  const load = async () => {
    const value = (await apiRequest(path, native)) as Snapshot;
    setSnapshot(value);
    setTags(value.value.denied_tags.join("\n"));
    setScopes(value.value.denied_scopes.join("\n"));
    pending.current = undefined;
  };
  useEffect(() => {
    let active = true;
    apiRequest(path, native)
      .then((value) => {
        if (!active) return;
        const snapshot = value as Snapshot;
        setSnapshot(snapshot);
        setTags(snapshot.value.denied_tags.join("\n"));
        setScopes(snapshot.value.denied_scopes.join("\n"));
      })
      .catch(() => {
        if (active) setNotice("送信方針を取得できませんでした。");
      });
    return () => {
      active = false;
    };
  }, [path, native]);
  return (
    <section aria-label="LLMへの送信方針">
      <h3>LLMへの送信方針</h3>
      {notice && <p role="status">{notice}</p>}
      <form
        className="auth-form"
        onSubmit={async (event) => {
          event.preventDefault();
          if (!snapshot) return;
          setBusy(true);
          setNotice("");
          const value = {
            denied_tags: lines(tags),
            denied_scopes: lines(scopes),
          };
          if (
            !pending.current ||
            JSON.stringify(pending.current.value) !== JSON.stringify(value)
          )
            pending.current = {
              operation_id: crypto.randomUUID(),
              expected_version: snapshot.version,
              value,
            };
          try {
            const result = (await apiRequest(path, native, {
              method: "POST",
              body: pending.current,
            })) as { data?: { version: string }; error?: { code: string } };
            if (!result.data || result.error)
              throw new Error(result.error?.code ?? "unavailable");
            setSnapshot({ value, version: result.data.version });
            pending.current = undefined;
            setNotice("送信方針を保存しました。以後の取得へ適用されます。");
          } catch (error) {
            const conflict = /更新されています|version_conflict/.test(
              String(error),
            );
            if (conflict) pending.current = undefined;
            setNotice(
              conflict
                ? "方針が更新されています。再読み込みして確認してください。"
                : "保存を確認できませんでした。同じ内容で再送できます。",
            );
          } finally {
            setBusy(false);
          }
        }}
      >
        <label>
          送信禁止タグ（1行に1つ）
          <textarea
            value={tags}
            disabled={busy || !snapshot}
            onChange={(event) => setTags(event.target.value)}
          />
        </label>
        <label>
          送信禁止ScopeのID（1行に1つ）
          <textarea
            value={scopes}
            disabled={busy || !snapshot}
            onChange={(event) => setScopes(event.target.value)}
          />
        </label>
        <button disabled={busy || !snapshot}>送信方針を保存</button>
        <button
          type="button"
          disabled={busy}
          onClick={async () => {
            setBusy(true);
            try {
              await load();
              setNotice("送信方針を再読み込みしました。");
            } catch {
              setNotice("送信方針を取得できませんでした。");
            } finally {
              setBusy(false);
            }
          }}
        >
          再読み込み
        </button>
      </form>
    </section>
  );
}
