import { useEffect, useRef, useState } from "react";
import { invoke } from "@tauri-apps/api/core";
import { syncRequest } from "./auth";
import { SyncHTTPError } from "./sync-error";
import { useScreenActive } from "./navigation";

type App = {
  id: string;
  name: string;
  instance: string;
  client_id: string;
  version: string;
};
type Page = { items: App[]; next?: string; callback_url?: string };
type Session = {
  id: string;
  state: string;
  account?: { login: string };
  connection?: string;
};
type Connection = {
  id: string;
  name: string;
  instance: string;
  version: string;
  allow_create: boolean;
};
export function GiteaOAuth({
  native,
  workspace,
  serverAdmin,
  canManage = true,
  selected,
  onSaved,
}: {
  native: boolean;
  workspace: string;
  serverAdmin: boolean;
  canManage?: boolean;
  selected?: Connection;
  onSaved: () => Promise<void>;
}) {
  const active = useScreenActive();
  const [apps, setApps] = useState<Page>(),
    [app, setApp] = useState("");
  const [session, setSession] = useState<Session>(),
    [name, setName] = useState(""),
    [allow, setAllow] = useState(false);
  const [target, setTarget] = useState<Connection>();
  const [draft, setDraft] = useState<App>(),
    [secret, setSecret] = useState(""),
    [ca, setCA] = useState("");
  const [busy, setBusy] = useState(false),
    [notice, setNotice] = useState("");
  const op = useRef("");
  const base = `/forge/workspaces/${encodeURIComponent(workspace)}/oauth`;
  const request = async <T,>(path: string, body?: unknown): Promise<T> =>
    (await syncRequest(path, native, {
      method: body === undefined ? "GET" : "POST",
      body,
    })) as T;
  async function work(action: () => Promise<void>) {
    setBusy(true);
    setNotice("");
    try {
      await action();
    } catch (error) {
      if (error instanceof SyncHTTPError && [401, 403].includes(error.status)) {
        setApps(undefined);
        setSession(undefined);
        setDraft(undefined);
        setSecret("");
        setCA("");
      }
      setNotice(
        "Gitea認証を確認できません。接続・権限・現在版を確認してください。",
      );
    } finally {
      setBusy(false);
    }
  }
  async function load(after = "") {
    const page = await request<Page>(
      `${serverAdmin ? "/forge/oauth" : base}/apps?after=${encodeURIComponent(after)}`,
    );
    setApps(page);
    setApp(page.items[0]?.id ?? "");
  }
  useEffect(() => {
    if (active) void work(() => load());
  }, [active]);
  useEffect(() => {
    if (
      !active ||
      !canManage ||
      !session ||
      !["started", "exchanging"].includes(session.state)
    )
      return;
    let cancelled = false;
    const timer = setTimeout(
      () =>
        void request<Session>(
          `${base}/sessions/${encodeURIComponent(session.id)}`,
        )
          .then((value) => {
            if (!cancelled) setSession(value);
          })
          .catch(() => {
            if (!cancelled) {
              setSession(undefined);
              setNotice("Gitea認証状態を取得できません。");
            }
          }),
      1000,
    );
    return () => {
      cancelled = true;
      clearTimeout(timer);
    };
  }, [active, session, canManage]);
  async function start(reconnect?: Connection) {
    const chosen = apps?.items.find((item) => item.id === app);
    if (!chosen) return;
    // Open synchronously for browsers that block popups after awaited requests.
    const popup = native ? null : window.open("about:blank", "_blank");
    if (!native && !popup) {
      setNotice("認証画面を開けません。ポップアップを許可してください。");
      return;
    }
    if (popup) popup.opener = null;
    await work(async () => {
      try {
        const result = await request<{ id: string; authorization_url: string }>(
          `${base}/${encodeURIComponent(app)}/start`,
          reconnect
            ? { connection: reconnect.id, expected_version: reconnect.version }
            : {},
        );
        setTarget(reconnect);
        setName(reconnect?.name ?? chosen.name);
        setAllow(reconnect?.allow_create ?? false);
        op.current = crypto.randomUUID();
        setSession({ id: result.id, state: "started" });
        if (native)
          await invoke("open_forge", {
            url: result.authorization_url,
            instance: chosen.instance,
          });
        else popup!.location.replace(result.authorization_url);
      } catch (error) {
        popup?.close();
        throw error;
      }
    });
  }
  async function accept() {
    if (!session) return;
    try {
      await request(
        `${base}/sessions/${encodeURIComponent(session.id)}/accept`,
        {
          name,
          allow_create: allow,
          operation_id: op.current,
          expected_version: target?.version ?? "0",
        },
      );
    } catch (error) {
      const current = await request<Session>(
        `${base}/sessions/${encodeURIComponent(session.id)}`,
      );
      if (current.state !== "accepted") throw error;
    }
    setSession(undefined);
    await onSaved();
    setNotice("Gitea共有接続を保存しました。");
  }
  async function saveApp() {
    if (!draft) return;
    await request(`/forge/oauth/apps/${encodeURIComponent(draft.id)}`, {
      name: draft.name,
      instance: draft.instance,
      client_id: draft.client_id,
      client_secret: secret,
      ca,
      expected_version: draft.version,
    });
    setDraft(undefined);
    setSecret("");
    setCA("");
    await load();
  }
  return (
    <section aria-label="Gitea OAuth">
      {canManage && (
        <>
          <label>
            Gitea
            <select
              value={app}
              disabled={busy}
              onChange={(e) => setApp(e.target.value)}
            >
              <option value="">選択してください</option>
              {apps?.items.map((item) => (
                <option key={item.id} value={item.id}>
                  {item.name}
                </option>
              ))}
            </select>
          </label>
          <button disabled={busy || !app} onClick={() => void start()}>
            Giteaにログイン
          </button>
          {selected &&
            apps?.items.some(
              (item) => item.id === app && item.instance === selected.instance,
            ) && (
              <button disabled={busy} onClick={() => void start(selected)}>
                選択した接続を再認証
              </button>
            )}
          {session?.state === "authenticated" && (
            <form
              onSubmit={(e) => {
                e.preventDefault();
                void work(accept);
              }}
            >
              <fieldset disabled={busy}>
                <legend>共有するGiteaアカウント</legend>
                <output>{session.account?.login}</output>
                <label>
                  共有接続名
                  <input
                    required
                    value={name}
                    onChange={(e) => setName(e.target.value)}
                  />
                </label>
                <label>
                  <input
                    type="checkbox"
                    checked={allow}
                    onChange={(e) => setAllow(e.target.checked)}
                  />
                  このWorkspaceでリポジトリ作成を許可
                </label>
                <button>認証した接続を共有</button>
              </fieldset>
            </form>
          )}
          {session && ["started", "exchanging"].includes(session.state) && (
            <p role="status">Giteaのログインを待っています。</p>
          )}
          {session && ["failed", "expired"].includes(session.state) && (
            <p role="status">
              Gitea認証が完了していません。ログインをやり直してください。
            </p>
          )}
        </>
      )}
      {apps?.next && (
        <button
          disabled={busy}
          onClick={() => void work(() => load(apps.next))}
        >
          Giteaの次のページ
        </button>
      )}
      {serverAdmin && (
        <details>
          <summary>Gitea OAuthアプリ</summary>
          <label>
            コールバックURL
            <input readOnly value={apps?.callback_url ?? ""} />
          </label>
          {apps?.items.map((item) => (
            <button
              key={item.id}
              disabled={busy}
              onClick={() => {
                setDraft(item);
                setSecret("");
                setCA("");
              }}
            >
              {item.name}を編集
            </button>
          ))}
          <button
            disabled={busy}
            onClick={() => {
              setDraft({
                id: crypto.randomUUID(),
                name: "",
                instance: "",
                client_id: "",
                version: "0",
              });
              setSecret("");
              setCA("");
            }}
          >
            OAuthアプリを追加
          </button>
          {draft && (
            <form
              onSubmit={(e) => {
                e.preventDefault();
                void work(saveApp);
              }}
            >
              <fieldset disabled={busy}>
                <legend>OAuthアプリ</legend>
                <label>
                  アプリ名
                  <input
                    required
                    value={draft.name}
                    onChange={(e) =>
                      setDraft({ ...draft, name: e.target.value })
                    }
                  />
                </label>
                <label>
                  Gitea URL
                  <input
                    required
                    type="url"
                    readOnly={draft.version !== "0"}
                    value={draft.instance}
                    onChange={(e) =>
                      setDraft({ ...draft, instance: e.target.value })
                    }
                  />
                </label>
                <label>
                  クライアントID
                  <input
                    required
                    readOnly={draft.version !== "0"}
                    value={draft.client_id}
                    onChange={(e) =>
                      setDraft({ ...draft, client_id: e.target.value })
                    }
                  />
                </label>
                <label>
                  クライアントシークレット
                  <input
                    required={draft.version === "0"}
                    type="password"
                    autoComplete="new-password"
                    value={secret}
                    onChange={(e) => setSecret(e.target.value)}
                  />
                </label>
                <label>
                  CA証明書（PEM）
                  <textarea
                    value={ca}
                    onChange={(e) => setCA(e.target.value)}
                  />
                </label>
                <button>OAuthアプリを保存</button>
              </fieldset>
            </form>
          )}
        </details>
      )}
      {notice && <p role="status">{notice}</p>}
    </section>
  );
}
