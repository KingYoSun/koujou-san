import { useEffect, useState } from "react";
import { invoke } from "@tauri-apps/api/core";

type User = {
  id: string;
  email: string;
  display_name: string;
  administrator: boolean;
};
type Node = {
  type: string;
  group: string;
  attributes: Record<string, unknown>;
  meta?: { label?: { text: string } };
  messages?: { text: string }[];
};
type Flow = {
  id: string;
  state?: string;
  ui: {
    action: string;
    method: string;
    nodes: Node[];
    messages?: { text: string }[];
  };
};
const titles: Record<string, string> = {
  login: "ログイン",
  registration: "アカウント作成",
  settings: "アカウント設定",
  verification: "メール確認",
  recovery: "アカウント復旧",
  consent: "認証の確認",
  error: "認証エラー",
};
const labels: Record<string, string> = {
  identifier: "メールアドレス",
  password: "パスワード",
  "traits.email": "メールアドレス",
  "traits.name": "表示名",
  totp_code: "認証アプリのコード",
  code: "メールの確認コード",
  email: "メールアドレス",
};

let csrfRequest: Promise<{ csrf: string }> | undefined;
const brokerRequests = new Map<string, Promise<unknown>>();

export async function apiRequest(
  path: string,
  native: boolean,
  options: { method?: string; body?: unknown } = {},
) {
  if (native)
    return invoke<unknown>("api_request", {
      path,
      method: options.method ?? "GET",
      body: options.body ?? null,
    });
  const headers: Record<string, string> = {};
  if (options.method && options.method !== "GET") {
    csrfRequest ??= fetch("/auth/csrf")
      .then((r) => r.json() as Promise<{ csrf: string }>)
      .finally(() => {
        csrfRequest = undefined;
      });
    const csrf = await csrfRequest;
    headers["X-CSRF-Token"] = csrf.csrf;
    headers["Content-Type"] = "application/json";
  }
  const response = await fetch(path, {
    ...options,
    headers,
    body: options.body === undefined ? undefined : JSON.stringify(options.body),
  });
  if (!response.ok)
    throw new Error(
      response.status === 401
        ? "ログインが必要です。"
        : response.status === 403
          ? "この操作の権限がありません。"
          : response.status === 409
            ? "状態が更新されています。再読み込みして確認してください。"
            : "認証サービスに接続できません。",
    );
  return response.json() as Promise<unknown>;
}

function Field({ node }: { node: Node }) {
  const attributes = node.attributes;
  if (node.type === "img")
    return (
      <img
        className="totp-qr"
        src={String(attributes.src)}
        alt="認証アプリに登録するQRコード"
      />
    );
  if (node.type === "text") {
    const text = attributes.text as { text?: string } | undefined;
    return <p>{text?.text}</p>;
  }
  if (node.type !== "input") return null;
  const name = String(attributes.name ?? "");
  const type = String(attributes.type ?? "text");
  const value =
    attributes.value === undefined ? undefined : String(attributes.value);
  if (type === "hidden")
    return <input type="hidden" name={name} value={value ?? ""} />;
  if (type === "submit")
    return (
      <button
        type="submit"
        name={name}
        value={value}
        disabled={Boolean(attributes.disabled)}
      >
        {node.meta?.label?.text ?? "続ける"}
      </button>
    );
  return (
    <label className="auth-field">
      {labels[name] ?? node.meta?.label?.text ?? name}
      <input
        name={name}
        type={type}
        required={Boolean(attributes.required)}
        disabled={Boolean(attributes.disabled)}
        defaultValue={value}
        autoComplete={
          type === "password"
            ? "current-password"
            : name.includes("email") || name === "identifier"
              ? "email"
              : undefined
        }
      />
      {node.messages?.map((message, index) => (
        <small role="alert" key={index}>
          {message.text}
        </small>
      ))}
    </label>
  );
}

function IdentityFlow({
  kind,
  query,
}: {
  kind: string;
  query: URLSearchParams;
}) {
  const [flow, setFlow] = useState<Flow>();
  const [error, setError] = useState("");
  useEffect(() => {
    let active = true;
    fetch(`/auth/flow/${kind}?${query.toString()}`)
      .then(async (response) => {
        if (response.status === 403) {
          window.location.assign(
            `/auth/login?aal=aal2&return_to=${encodeURIComponent(window.location.href)}`,
          );
          return;
        }
        if (!response.ok)
          throw new Error(
            "認証フローが失効したか、ログインが必要です。もう一度開始してください。",
          );
        const value = (await response.json()) as Flow & { redirect?: string };
        if (value.redirect) {
          if (active) window.location.assign(value.redirect);
          return;
        }
        if (active) setFlow(value);
      })
      .catch((cause) => {
        if (active) setError(String(cause.message));
      });
    return () => {
      active = false;
    };
  }, [kind, query.toString()]);
  if (error) return <p role="alert">{error}</p>;
  if (!flow) return <p role="status">読み込んでいます…</p>;
  const groups = [
    ...new Set(
      flow.ui.nodes
        .map((node) => node.group)
        .filter((group) => group !== "default"),
    ),
  ];
  return (
    <>
      {flow.ui.messages?.map((message, index) => (
        <p role="status" key={index}>
          {message.text}
        </p>
      ))}
      {flow.state === "passed_challenge" && (
        <p role="status">確認できました。ログインして続けてください。</p>
      )}
      {(groups.length ? groups : ["default"]).map((group) => (
        <form
          className="auth-form"
          action={flow.ui.action}
          method="post"
          key={`${flow.id}-${group}`}
        >
          {groups.length > 1 && (
            <h2>
              {group === "totp"
                ? "認証アプリ（任意）"
                : group === "password"
                  ? "パスワード"
                  : group === "profile"
                    ? "プロフィール"
                    : titles[kind]}
            </h2>
          )}
          {flow.ui.nodes
            .filter(
              (node) =>
                node.group === group ||
                (group !== "default" && node.group === "default"),
            )
            .map((node, index) => (
              <Field node={node} key={index} />
            ))}
        </form>
      ))}
    </>
  );
}

export function Account({ native }: { native: boolean }) {
  const [user, setUser] = useState<User>();
  const [notice, setNotice] = useState("");
  const [busy, setBusy] = useState(false);
  const [status, setStatus] = useState("checking");
  const [required, setRequired] = useState("");
  const kind =
    typeof window === "undefined"
      ? ""
      : (window.location.pathname.split("/")[2] ?? "");
  const query = new URLSearchParams(
    typeof window === "undefined" ? "" : window.location.search,
  );
  const challenge = query.get(`${kind}_challenge`);
  useEffect(() => {
    apiRequest("/auth/me", native)
      .then((value) => {
        setUser((value as { user: User }).user);
        setStatus("ready");
      })
      .catch((cause: unknown) => {
        const message = String(cause);
        setStatus(
          message.includes("ログインが必要") ||
            message.includes("authentication_required")
            ? "signed_out"
            : "unavailable",
        );
      });
  }, [native]);
  useEffect(() => {
    if ((kind !== "login" && kind !== "consent") || !challenge) return;
    let active = true;
    const path = `/auth/broker/${kind}?challenge=${encodeURIComponent(challenge)}`;
    let request = brokerRequests.get(path);
    if (!request) {
      request = apiRequest(path, false, { method: "POST" });
      brokerRequests.set(path, request);
      void request.finally(() => brokerRequests.delete(path)).catch(() => {});
    }
    request
      .then((value) => {
        if (!active) return;
        const result = value as { redirect?: string; required?: string };
        setRequired(result.required ?? "");
        if (result.redirect) {
          window.location.assign(result.redirect);
          return;
        }
        if (result.required === "aal2" && !query.has("aal")) {
          window.location.assign(
            `/auth/login?aal=aal2&login_challenge=${encodeURIComponent(challenge)}`,
          );
        }
        if (result.required === "verification") {
          window.location.assign(
            `/auth/verification?login_challenge=${encodeURIComponent(challenge)}`,
          );
        }
      })
      .catch((cause) => {
        if (active) setNotice(cause.message);
      });
    return () => {
      active = false;
    };
  }, [kind, challenge]);
  async function login() {
    setBusy(true);
    setNotice("");
    try {
      if (native) {
        await invoke("login");
        const value = (await apiRequest("/auth/me", true)) as { user: User };
        setUser(value.user);
        setStatus("ready");
        window.dispatchEvent(new Event("koujou-auth-changed"));
      } else {
        window.location.assign("/auth/start");
      }
    } catch {
      setNotice(
        "ログインを完了できませんでした。接続先と認証状態を確認してください。",
      );
    } finally {
      setBusy(false);
    }
  }
  async function logout() {
    setBusy(true);
    try {
      const result = native
        ? await invoke<Record<string, string>>("logout")
        : ((await apiRequest("/auth/logout", false, {
            method: "POST",
          })) as Record<string, string>);
      setUser(undefined);
      setStatus("signed_out");
      window.dispatchEvent(new Event("koujou-auth-changed"));
      setNotice(
        `この端末のアプリからログアウトしました。OAuth: ${result.oauth_tokens} / ブラウザSSO: ${result.kratos_session}`,
      );
    } catch {
      setNotice("ログアウトの結果を確認できませんでした。");
    } finally {
      setBusy(false);
    }
  }
  const link = (target: string) =>
    `/auth/${target}${query.get("login_challenge") ? `?login_challenge=${encodeURIComponent(query.get("login_challenge")!)}` : ""}`;
  return (
    <section className="panel account" aria-labelledby="account-title">
      <h2 id="account-title">{titles[kind] ?? "アカウント"}</h2>
      {notice && <p role="status">{notice}</p>}
      {user ? (
        <>
          <p>{user.display_name || user.email}</p>
          <p className="note">
            {user.email}
            {user.administrator ? " · 初期管理者" : ""}
          </p>
          <div className="auth-actions">
            <button
              onClick={() =>
                native
                  ? invoke("open_account", { path: "settings" })
                  : window.location.assign("/auth/settings")
              }
            >
              アカウント設定
            </button>
            <button disabled={busy} onClick={logout}>
              この端末からログアウト
            </button>
          </div>
        </>
      ) : status === "checking" ? (
        <p role="status">認証状態を確認しています…</p>
      ) : status === "unavailable" ? (
        <p role="alert">
          認証状態を確認できません。サービスへの接続を確認して、再読み込みしてください。
        </p>
      ) : (
        <>
          <p className="note">
            メール確認後に利用できます。認証アプリは設定画面から追加できます。
          </p>
          <button disabled={busy} onClick={login}>
            {busy ? "ブラウザで認証しています…" : "ログイン"}
          </button>
        </>
      )}
      {!native &&
        titles[kind] &&
        kind !== "consent" &&
        kind !== "error" &&
        (!challenge ||
          required === "login" ||
          (required === "aal2" && query.get("aal") === "aal2")) && (
          <IdentityFlow kind={kind} query={query} />
        )}
      {!native && (
        <nav className="auth-links" aria-label="アカウント">
          <a href={link("login")}>ログイン</a>
          <a href={link("registration")}>アカウント作成</a>
          <a href={link("verification")}>メール確認</a>
          <a href={link("recovery")}>パスワードを忘れた場合</a>
          <a href="/">ホーム</a>
        </nav>
      )}
    </section>
  );
}
