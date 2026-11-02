import { useEffect, useState, type ReactNode } from "react";
import { invoke } from "@tauri-apps/api/core";

export function DesktopConnection({ children }: { children: ReactNode }) {
  const [profile, setProfile] = useState<{ api: string | null }>();
  const [api, setApi] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  useEffect(() => {
    invoke<{ api: string | null }>("connection_status")
      .then(setProfile)
      .catch(() => setError("接続設定を読み込めませんでした。"));
  }, []);
  if (profile?.api) return children;
  return (
    <main className="shell">
      <section className="panel">
        <h1>koujou-sanに接続する</h1>
        {error && <p role="alert">{error}</p>}
        {!profile ? (
          <p role="status">接続設定を取得中…</p>
        ) : (
          <form
            onSubmit={async (event) => {
              event.preventDefault();
              setBusy(true);
              setError("");
              try {
                setProfile(await invoke("connection_configure", { api }));
              } catch {
                setError(
                  "接続先を保存できませんでした。HTTPSのURLとサーバーの稼働状態・証明書を確認してください。",
                );
              } finally {
                setBusy(false);
              }
            }}
          >
            <label>
              共有サーバーURL
              <input
                type="url"
                value={api}
                required
                disabled={busy}
                onChange={(event) => setApi(event.target.value)}
              />
            </label>
            <button disabled={busy} aria-busy={busy}>
              {busy ? "接続先を確認中…" : "接続先を保存"}
            </button>
          </form>
        )}
      </section>
    </main>
  );
}
