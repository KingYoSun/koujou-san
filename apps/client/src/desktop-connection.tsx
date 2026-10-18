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
        <p>
          管理者から案内された共有サーバーのURLを入力してください。この端末に接続先を保存し、ログインへ進みます。
        </p>
        {error && <p role="alert">{error}</p>}
        {!profile ? (
          <p role="status">接続設定を確認しています。</p>
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
                  "接続できませんでした。HTTPSのURLと、サーバーの稼働状態・証明書を確認してください。",
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
                placeholder="https://koujou.example.com/"
                required
                disabled={busy}
                onChange={(event) => setApi(event.target.value)}
              />
            </label>
            <button disabled={busy}>
              {busy ? "接続を確認中…" : "接続先を保存する"}
            </button>
          </form>
        )}
      </section>
    </main>
  );
}
