import { useEffect, useState, type ReactNode } from "react";
import { invoke } from "@tauri-apps/api/core";

export function DesktopConnection({
  children,
  settings = false,
}: {
  children?: ReactNode;
  settings?: boolean;
}) {
  const [profile, setProfile] = useState<{
    api: string | null;
    managed?: boolean;
  }>();
  const [api, setApi] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  useEffect(() => {
    invoke<{ api: string | null }>("connection_status")
      .then((value) => {
        setProfile(value);
        setApi(value.api ?? "");
      })
      .catch(() => setError("接続設定を読み込めませんでした。"));
  }, []);
  if (profile?.api && !settings) return children;
  const content = (
    <section className="panel">
      {settings ? <h3>共有サーバー</h3> : <h1>koujou-sanに接続する</h1>}
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
              disabled={busy || profile.managed}
              onChange={(event) => setApi(event.target.value)}
            />
          </label>
          <button disabled={busy || profile.managed} aria-busy={busy}>
            {busy
              ? "接続先を確認中…"
              : settings
                ? "接続先を保存して再起動"
                : "接続先を保存"}
          </button>
          {profile.managed && (
            <p role="status">接続先は環境変数で設定されています。</p>
          )}
        </form>
      )}
    </section>
  );
  return settings ? content : <main className="shell">{content}</main>;
}
