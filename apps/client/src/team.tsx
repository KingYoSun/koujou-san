import { useEffect, useRef, useState } from "react";
import { invoke } from "@tauri-apps/api/core";
import { apiRequest } from "./auth";

type Workspace = {
  id: string;
  name: string;
  role: string;
  can_execute: boolean;
  version: string;
};
type Member = {
  id: string;
  name: string;
  role: string;
  can_execute: boolean;
  active: boolean;
  version: string;
};
type Invite = {
  id: string;
  role: string;
  expires_at: string;
  cancelled: boolean;
  accepted: boolean;
  version: string;
};
type Device = {
  id: string;
  name: string;
  identifier: string;
  registered_by: string;
  enabled: boolean;
  can_input: boolean;
  can_report: boolean;
  last_seen: string | null;
  version: string;
};
type Page<T> = { items: T[]; next_cursor?: string };
type Envelope<T> = { data: T };
const roles: Record<string, string> = {
  admin: "管理者",
  editor: "編集者",
  viewer: "閲覧者",
};
const empty = <T,>(): Page<T> => ({ items: [] });
function message(error: unknown) {
  const text = String(error);
  if (text.includes("device_not_registered"))
    return "この端末の登録が必要です。";
  if (text.includes("device_contact_failed"))
    return "接続確認を完了できませんでした。状態を再読み込みして、もう一度確認してください。";
  if (/credential_storage|credential_protection/.test(text))
    return "端末の安全な保存領域を確認してください。";
  if (text.includes("api_unavailable")) return "APIへ接続できません。";
  return text.includes("forbidden")
    ? "この操作の権限がありません。"
    : text.includes("version_conflict")
      ? "状態が更新されています。再読み込みして確認してください。"
      : text.includes("device_access_revoked")
        ? "この端末の資格情報または許可が失効しています。"
        : /[ぁ-んァ-ヶ一-龠]/.test(text)
          ? text.replace(/^Error: /, "")
          : "操作を完了できませんでした。接続と現在の状態を確認してください。";
}

export function Team({ native }: { native: boolean }) {
  const [me, setMe] = useState<{ id: string }>();
  const [status, setStatus] = useState("checking");
  const [workspaces, setWorkspaces] = useState<Page<Workspace>>(empty);
  const [selected, setSelected] = useState("");
  const [members, setMembers] = useState<Page<Member>>(empty);
  const [invites, setInvites] = useState<Page<Invite>>(empty);
  const [devices, setDevices] = useState<Page<Device>>(empty);
  const [notice, setNotice] = useState("");
  const [busy, setBusy] = useState(false);
  const [revision, setRevision] = useState(0);
  const [name, setName] = useState("");
  const [deviceName, setDeviceName] = useState("Windowsの端末");
  const [inviteRole, setInviteRole] = useState("viewer");
  const [link, setLink] = useState("");
  const [publicSite, setPublicSite] = useState("");
  const [identifier, setIdentifier] = useState("");
  const [invitation, setInvitation] = useState("");
  const pending = useRef(new Map<string, Record<string, unknown>>());
  const current = workspaces.items.find((w) => w.id === selected);
  const admin = current?.role === "admin";
  const mine = devices.items.find(
    (d) => d.identifier === identifier && d.registered_by === me?.id,
  );
  async function root(after = "", append = false) {
    const value = (await apiRequest(
      `/access/workspaces${after ? `?after=${encodeURIComponent(after)}` : ""}`,
      native,
    )) as Page<Workspace>;
    setWorkspaces((previous) => ({
      ...value,
      items: append ? [...previous.items, ...value.items] : value.items,
    }));
    if (!append)
      setSelected((previous) =>
        value.items.some((w) => w.id === previous)
          ? previous
          : (value.items[0]?.id ?? ""),
      );
  }
  useEffect(() => {
    if (typeof window === "undefined") return;
    const raw = new URLSearchParams(window.location.hash.slice(1)).get("join");
    if (raw) sessionStorage.setItem("koujou.pending-invite", raw);
    setInvitation(sessionStorage.getItem("koujou.pending-invite") ?? "");
    let active = true;
    const load = () => {
      apiRequest("/auth/me", native)
        .then(async (value) => {
          if (!active) return;
          setMe((value as { user: { id: string } }).user);
          setStatus("ready");
          const config = (await apiRequest("/auth/config", native)) as {
            site_url: string;
          };
          if (!active) return;
          setPublicSite(config.site_url);
          if (native) setIdentifier(await invoke<string>("device_identifier"));
          await root();
        })
        .catch((error) => {
          if (!active) return;
          setMe(undefined);
          setStatus(
            /ログインが必要|authentication_required/.test(String(error))
              ? "signed_out"
              : "unavailable",
          );
        });
    };
    load();
    window.addEventListener("koujou-auth-changed", load);
    return () => {
      active = false;
      window.removeEventListener("koujou-auth-changed", load);
    };
  }, [native]);
  async function page<T>(kind: string, after = ""): Promise<Page<T>> {
    const response = (await apiRequest(
      `/access/workspaces/${encodeURIComponent(selected)}/${kind}${after ? `?after=${encodeURIComponent(after)}` : ""}`,
      native,
    )) as Envelope<Page<T>>;
    return response.data;
  }
  useEffect(() => {
    setMembers(empty());
    setDevices(empty());
    setInvites(empty());
    if (!selected) return;
    let active = true;
    Promise.all([
      page<Member>("members"),
      page<Device>("devices"),
      admin ? page<Invite>("invites") : Promise.resolve(empty<Invite>()),
    ])
      .then(([m, d, i]) => {
        if (active) {
          setMembers(m);
          setDevices(d);
          setInvites(i);
        }
      })
      .catch((error) => {
        if (active) setNotice(message(error));
      });
    return () => {
      active = false;
    };
  }, [selected, admin, revision]);
  useEffect(() => setLink(""), [selected, admin]);
  async function act(
    kind: string,
    body: Record<string, unknown>,
    id: string | undefined,
    version = "0",
    workspace = selected,
  ) {
    setBusy(true);
    setNotice("");
    const key = workspace + kind + JSON.stringify(body) + (id ?? "");
    let request = pending.current.get(key);
    if (!request) {
      request = {
        ...body,
        id: id ?? crypto.randomUUID(),
        operation_id: crypto.randomUUID(),
        expected_version: version,
      };
      pending.current.set(key, request);
    }
    const target = kind === "create" ? String(request.id) : workspace;
    try {
      const result = (await apiRequest(
        `/access/workspaces/${encodeURIComponent(target)}/${kind}`,
        native,
        { method: "POST", body: request },
      )) as { invitation?: string };
      pending.current.delete(key);
      if (result.invitation)
        setLink(`${publicSite}/#join=${result.invitation}`);
      if (kind === "invite-accept") {
        sessionStorage.removeItem("koujou.pending-invite");
        setInvitation("");
        history.replaceState(null, "", location.pathname + location.search);
      }
      await root();
      setRevision((v) => v + 1);
      if (kind === "create") {
        setName("");
        setSelected(target);
      }
      return true;
    } catch (error) {
      if (/更新されています|version_conflict/.test(String(error)))
        pending.current.delete(key);
      setNotice(message(error));
      return false;
    } finally {
      setBusy(false);
    }
  }
  async function accept() {
    const parts = invitation.split(".");
    if (parts.length !== 3) {
      setNotice("招待リンクを確認してください。");
      return;
    }
    // Read its current version without exposing metadata to a non-member. The
    // server validates this single-use capability when resolving acceptance.
    setBusy(true);
    setNotice("");
    try {
      const result = (await apiRequest(
        `/access/workspaces/${encodeURIComponent(parts[0])}/invite-preview`,
        native,
        { method: "POST", body: { token: parts.slice(1).join(".") } },
      )) as { version: string };
      await act(
        "invite-accept",
        { token: parts.slice(1).join(".") },
        parts[1],
        result.version,
        parts[0],
      );
    } catch (error) {
      setNotice(message(error));
    } finally {
      setBusy(false);
    }
  }
  async function nativeAction(kind: "enroll" | "contact") {
    setBusy(true);
    setNotice("");
    try {
      if (kind === "enroll")
        await invoke("device_enroll", {
          workspace: selected,
          name: deviceName,
        });
      else
        await invoke("device_contact", { workspace: selected, user: me?.id });
      setNotice(
        kind === "enroll"
          ? "端末資格情報を保存しました。"
          : "端末からの接続を確認しました。",
      );
      setRevision((v) => v + 1);
    } catch (error) {
      setNotice(message(error));
    } finally {
      setBusy(false);
    }
  }
  if (!me)
    return (
      <section className="panel" aria-label="チームと端末">
        <h2>チームと端末</h2>
        <p>
          {status === "checking"
            ? "認証状態を確認しています…"
            : status === "unavailable"
              ? "認証サービスへの接続を確認してください。"
              : "ログインするとWorkspaceへの参加・作成ができます。"}
        </p>
        {invitation && (
          <p>招待リンクを保持しています。ログイン後に受諾してください。</p>
        )}
      </section>
    );
  const selectRole = (value: string, onChange: (role: string) => void) => (
    <select
      aria-label="役割"
      value={value}
      disabled={busy}
      onChange={(e) => onChange(e.target.value)}
    >
      {Object.entries(roles).map(([key, label]) => (
        <option value={key} key={key}>
          {label}
        </option>
      ))}
    </select>
  );
  return (
    <section className="panel team" aria-labelledby="team-title">
      <h2 id="team-title">チームと端末</h2>
      {notice && <p role="status">{notice}</p>}
      {invitation && (
        <div>
          <p>招待リンクの所持者として参加できます。</p>
          <button disabled={busy} onClick={accept}>
            招待を受諾
          </button>
        </div>
      )}
      <form
        className="auth-form"
        onSubmit={(e) => {
          e.preventDefault();
          void act("create", { name }, undefined);
        }}
      >
        <label>
          新しいWorkspaceの名前
          <input
            required
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
        </label>
        <button disabled={busy}>Workspaceを作成</button>
      </form>
      {workspaces.items.length > 0 && (
        <label>
          Workspace
          <select
            value={selected}
            onChange={(e) => setSelected(e.target.value)}
            disabled={busy}
          >
            {workspaces.items.map((w) => (
              <option key={w.id} value={w.id}>
                {w.name}（{roles[w.role]}）
              </option>
            ))}
          </select>
        </label>
      )}
      {workspaces.next_cursor && (
        <button onClick={() => root(workspaces.next_cursor, true)}>
          さらにWorkspaceを表示
        </button>
      )}
      {current && (
        <>
          <p>
            あなたの役割: {roles[current.role]} / 実行依頼:{" "}
            {current.can_execute ? "許可" : "不許可"}
          </p>
          <button
            disabled={busy}
            onClick={() => {
              const own = members.items.find((m) => m.id === me.id);
              if (own) void act("leave", {}, me.id, own.version);
            }}
          >
            このWorkspaceから離脱
          </button>
          <h3>参加者</h3>
          <ul>
            {members.items.map((m) => (
              <li key={m.id}>
                <span>
                  {m.name} · {m.active ? roles[m.role] : "利用停止"}
                </span>
                {admin && (
                  <div className="team-controls">
                    {selectRole(m.role, (role) => {
                      void act(
                        "member-update",
                        { role, can_execute: m.can_execute, active: m.active },
                        m.id,
                        m.version,
                      );
                    })}
                    <label>
                      <input
                        type="checkbox"
                        checked={m.can_execute}
                        disabled={busy}
                        onChange={(e) => {
                          void act(
                            "member-update",
                            {
                              role: m.role,
                              active: m.active,
                              can_execute: e.target.checked,
                            },
                            m.id,
                            m.version,
                          );
                        }}
                      />
                      実行依頼を許可
                    </label>
                    <button
                      disabled={busy}
                      onClick={() =>
                        act(
                          "member-update",
                          {
                            role: m.role,
                            can_execute: m.can_execute,
                            active: !m.active,
                          },
                          m.id,
                          m.version,
                        )
                      }
                    >
                      {m.active ? "利用を停止" : "利用を再開"}
                    </button>
                  </div>
                )}
              </li>
            ))}
          </ul>
          {members.next_cursor && (
            <button
              onClick={async () => {
                const value = await page<Member>(
                  "members",
                  members.next_cursor,
                );
                setMembers({
                  ...value,
                  items: [...members.items, ...value.items],
                });
              }}
            >
              さらに参加者を表示
            </button>
          )}
          {admin && (
            <>
              <h3>招待</h3>
              <p>
                ログイン済みのリンク所持者が1回受諾できます。リンクは7日間有効で、転送先も参加できます。
              </p>
              {selectRole(inviteRole, setInviteRole)}
              <button
                disabled={busy}
                onClick={() =>
                  act("invite-create", { role: inviteRole }, undefined)
                }
              >
                招待リンクを作成
              </button>
              {link && (
                <label>
                  招待リンク
                  <input
                    readOnly
                    value={link}
                    onFocus={(e) => e.target.select()}
                  />
                </label>
              )}
              <ul>
                {invites.items.map((i) => (
                  <li key={i.id}>
                    {roles[i.role]} ·{" "}
                    {i.cancelled
                      ? "取消済み"
                      : i.accepted
                        ? "受諾済み"
                        : new Date(i.expires_at) < new Date()
                          ? "期限切れ"
                          : `有効期限 ${new Date(i.expires_at).toLocaleString()}`}
                    <button
                      disabled={busy || i.cancelled || i.accepted}
                      onClick={() => act("invite-cancel", {}, i.id, i.version)}
                    >
                      招待を取り消す
                    </button>
                  </li>
                ))}
              </ul>
              {invites.next_cursor && (
                <button
                  onClick={async () => {
                    const value = await page<Invite>(
                      "invites",
                      invites.next_cursor,
                    );
                    setInvites({
                      ...value,
                      items: [...invites.items, ...value.items],
                    });
                  }}
                >
                  さらに招待を表示
                </button>
              )}
            </>
          )}
          <h3>端末</h3>
          <p>
            端末登録と実行依頼の許可は別です。最終接続時刻から、CLIの稼働・停止は判断できません。
          </p>
          {native && current.role !== "viewer" && (
            <div className="team-controls">
              <label>
                この端末の名前
                <input
                  disabled={busy || Boolean(mine)}
                  value={deviceName}
                  onChange={(e) => setDeviceName(e.target.value)}
                />
              </label>
              <button disabled={busy} onClick={() => nativeAction("enroll")}>
                {mine ? "この端末の資格情報を更新" : "この端末を登録"}
              </button>
              {mine && (
                <button disabled={busy} onClick={() => nativeAction("contact")}>
                  この端末の接続を確認
                </button>
              )}
            </div>
          )}
          {!native && (
            <p>
              この端末の登録と資格情報の保存はWindowsデスクトップ版から行います。
            </p>
          )}
          <ul>
            {devices.items.map((d) => (
              <li key={d.id}>
                <strong>{d.name}</strong> · {d.enabled ? "登録済み" : "失効"}
                <p>
                  入力取得: {d.can_input ? "許可" : "不許可"} / 実行報告:{" "}
                  {d.can_report ? "許可" : "不許可"} / 最終接続:{" "}
                  {d.last_seen
                    ? new Date(d.last_seen).toLocaleString()
                    : "報告なし"}
                </p>
                {admin && (
                  <div className="team-controls">
                    <label>
                      <input
                        type="checkbox"
                        checked={d.can_input}
                        disabled={busy || !d.enabled}
                        onChange={(e) => {
                          void act(
                            "device-update",
                            {
                              active: d.enabled,
                              can_input: e.target.checked,
                              can_report: d.can_report,
                            },
                            d.id,
                            d.version,
                          );
                        }}
                      />
                      入力取得を許可
                    </label>
                    <label>
                      <input
                        type="checkbox"
                        checked={d.can_report}
                        disabled={busy || !d.enabled}
                        onChange={(e) => {
                          void act(
                            "device-update",
                            {
                              active: d.enabled,
                              can_input: d.can_input,
                              can_report: e.target.checked,
                            },
                            d.id,
                            d.version,
                          );
                        }}
                      />
                      実行報告を許可
                    </label>
                    <button
                      disabled={busy || !d.enabled}
                      onClick={() =>
                        act(
                          "device-update",
                          {
                            active: false,
                            can_input: false,
                            can_report: false,
                          },
                          d.id,
                          d.version,
                        )
                      }
                    >
                      端末を失効
                    </button>
                  </div>
                )}
              </li>
            ))}
          </ul>
          {devices.next_cursor && (
            <button
              onClick={async () => {
                const value = await page<Device>(
                  "devices",
                  devices.next_cursor,
                );
                setDevices({
                  ...value,
                  items: [...devices.items, ...value.items],
                });
              }}
            >
              さらに端末を表示
            </button>
          )}
        </>
      )}
    </section>
  );
}
