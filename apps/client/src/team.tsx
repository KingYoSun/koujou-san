import type { ReactNode } from "react";
import {
  NavigationProvider,
  ScreenContent,
  routeHref,
  historyActorMatches,
  type Screen,
} from "./navigation";
import { WorkspaceShell } from "./workspace-shell";
import { useEffect, useRef, useState } from "react";
import { invoke } from "@tauri-apps/api/core";
import { apiRequest } from "./auth";
import { DeliveryPolicy } from "./delivery-policy";
import { discardSyncWorkspace } from "./sync-engine";
import { syncStorage } from "./sync-store";
import { Field } from "./field";
import { StatusBadge } from "./status-badge";

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
const count = (label: string, list: Page<unknown>) => (
  <p aria-live="polite">
    {list.items.length
      ? `${label}：${list.items.length}件を表示${list.next_cursor ? "（続きあり）" : ""}`
      : `${label}はありません。`}
  </p>
);
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

export function Team({
  native,
  account,
  connection,
  health,
}: {
  native: boolean;
  account: ReactNode;
  connection: ReactNode;
  health: string;
}) {
  const [me, setMe] = useState<{ id: string }>();
  const [workspaces, setWorkspaces] = useState<Page<Workspace>>(empty);
  const [selected, setSelected] = useState("");
  const selectedRef = useRef(selected);
  selectedRef.current = selected;
  const [screen, setScreen] = useState<Screen>("work");
  const [members, setMembers] = useState<Page<Member>>(empty);
  const [invites, setInvites] = useState<Page<Invite>>(empty);
  const [devices, setDevices] = useState<Page<Device>>(empty);
  const [pageWorkspace, setPageWorkspace] = useState("");
  const [notice, setNotice] = useState("");
  const [busy, setBusy] = useState(false);
  const [revision, setRevision] = useState(0);
  const [name, setName] = useState("");
  const [deviceName, setDeviceName] = useState("Windowsの端末");
  const [inviteRole, setInviteRole] = useState("viewer");
  const [link, setLink] = useState("");
  const [publicSite, setPublicSite] = useState("");
  const [apiIdentity, setApiIdentity] = useState("");
  const [identifier, setIdentifier] = useState("");
  const [invitation, setInvitation] = useState("");
  const pending = useRef(new Map<string, Record<string, unknown>>());
  const authEpoch = useRef(0);
  function selectWorkspace(id: string) {
    history.pushState(
      null,
      "",
      routeHref({ workspace: id, screen: "work" }) + location.hash,
    );
    setSelected(id);
  }
  useEffect(() => {
    const restore = () => {
      const scope = history.state?.koujou?.scope as string | undefined;
      if (scope && me && !historyActorMatches(scope, apiIdentity, me.id)) {
        history.replaceState(
          null,
          "",
          routeHref({ workspace: selected, screen: "work" }) + location.hash,
        );
        return;
      }
      const id = new URLSearchParams(location.search).get("workspace");
      if (id && workspaces.items.some((w) => w.id === id)) setSelected(id);
    };
    window.addEventListener("popstate", restore);
    return () => window.removeEventListener("popstate", restore);
  }, [workspaces, me, apiIdentity, selected]);
  const current = workspaces.items.find((w) => w.id === selected);
  const admin = current?.role === "admin";
  const mine = devices.items.find(
    (d) => d.identifier === identifier && d.registered_by === me?.id,
  );
  async function root(after = "", append = false) {
    const epoch = authEpoch.current;
    const value = (await apiRequest(
      `/access/workspaces${after ? `?after=${encodeURIComponent(after)}` : ""}`,
      native,
    )) as Page<Workspace>;
    if (epoch !== authEpoch.current) return;
    setWorkspaces((previous) => ({
      ...value,
      items: append ? [...previous.items, ...value.items] : value.items,
    }));
    if (!append)
      setSelected((previous) =>
        value.items.some((w) => w.id === previous)
          ? previous
          : (value.items.find(
              (w) =>
                w.id === new URLSearchParams(location.search).get("workspace"),
            )?.id ??
            value.items[0]?.id ??
            ""),
      );
  }
  useEffect(() => {
    if (typeof window === "undefined") return;
    const readInvitation = () => {
      const raw = new URLSearchParams(window.location.hash.slice(1)).get(
        "join",
      );
      if (raw) sessionStorage.setItem("koujou.pending-invite", raw);
      setInvitation(sessionStorage.getItem("koujou.pending-invite") ?? "");
    };
    readInvitation();
    window.addEventListener("hashchange", readInvitation);
    let active = true;
    const load = (event?: Event) => {
      const epoch = ++authEpoch.current;
      setNotice(
        (event as CustomEvent<{ notice?: string }> | undefined)?.detail
          ?.notice ?? "",
      );
      setBusy(false);
      setInviteRole("viewer");
      setDeviceName("Windowsの端末");
      setMe(undefined);
      setWorkspaces(empty());
      setSelected("");
      setMembers(empty());
      setInvites(empty());
      setDevices(empty());
      setPageWorkspace("");
      setName("");
      setLink("");
      pending.current.clear();
      apiRequest("/auth/me", native)
        .then(async (value) => {
          if (!active || epoch !== authEpoch.current) return;
          setMe((value as { user: { id: string } }).user);
          const config = (await apiRequest("/auth/config", native)) as {
            site_url: string;
            api_url: string;
          };
          if (!active || epoch !== authEpoch.current) return;
          const previousScope = history.state?.koujou?.scope as
            string | undefined;
          if (previousScope) {
            const previousActor = JSON.parse(previousScope) as string[];
            if (
              previousActor[0] !== config.api_url ||
              previousActor[1] !== (value as { user: { id: string } }).user.id
            )
              history.replaceState(null, "", location.pathname + location.hash);
          }
          setPublicSite(config.site_url);
          setApiIdentity(config.api_url);
          if (native) setIdentifier(await invoke<string>("device_identifier"));
          await root();
        })
        .catch(() => {
          if (!active || epoch !== authEpoch.current) return;
          setMe(undefined);
        });
    };
    load();
    window.addEventListener("koujou-auth-changed", load);
    return () => {
      active = false;
      window.removeEventListener("hashchange", readInvitation);
      window.removeEventListener("koujou-auth-changed", load);
    };
  }, [native]);
  async function page<T>(
    kind: string,
    after = "",
  ): Promise<Page<T> | undefined> {
    const epoch = authEpoch.current;
    const owner = selected;
    const response = (await apiRequest(
      `/access/workspaces/${encodeURIComponent(selected)}/${kind}${after ? `?after=${encodeURIComponent(after)}` : ""}`,
      native,
    )) as Envelope<Page<T>>;
    if (epoch === authEpoch.current && owner === selectedRef.current)
      return response.data;
  }
  async function more<T>(
    kind: string,
    list: Page<T>,
    set: (value: Page<T>) => void,
  ) {
    setBusy(true);
    try {
      const value = await page<T>(kind, list.next_cursor);
      if (value) set({ ...value, items: [...list.items, ...value.items] });
    } catch (error) {
      setNotice(message(error));
    } finally {
      setBusy(false);
    }
  }
  useEffect(() => {
    setMembers(empty());
    setDevices(empty());
    setInvites(empty());
    if (!selected || !["work", "workspace", "devices"].includes(screen)) return;
    let active = true;
    const epoch = authEpoch.current;
    Promise.all([
      screen === "work" || screen === "workspace"
        ? page<Member>("members")
        : Promise.resolve(empty<Member>()),
      screen === "devices"
        ? page<Device>("devices")
        : Promise.resolve(empty<Device>()),
      admin && screen === "workspace"
        ? page<Invite>("invites")
        : Promise.resolve(empty<Invite>()),
    ])
      .then(([m, d, i]) => {
        if (active && epoch === authEpoch.current && m && d && i) {
          setMembers(m);
          setDevices(d);
          setInvites(i);
          setPageWorkspace(selected);
        }
      })
      .catch((error) => {
        if (active) setNotice(message(error));
      });
    return () => {
      active = false;
    };
  }, [selected, admin, revision, screen]);
  useEffect(() => {
    setName("");
    setInviteRole("viewer");
    setDeviceName("Windowsの端末");
    setLink("");
    setBusy(false);
  }, [selected, admin]);
  async function act(
    kind: string,
    body: Record<string, unknown>,
    id: string | undefined,
    version = "0",
    workspace = selected,
  ) {
    const epoch = authEpoch.current;
    const owner = selectedRef.current;
    const currentScope = () =>
      epoch === authEpoch.current && owner === selectedRef.current;
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
      if (!currentScope()) return false;
      if (
        (kind === "leave" ||
          (kind === "member-update" &&
            id === me?.id &&
            body.active === false)) &&
        me
      ) {
        const namespace = { api: apiIdentity, user: me.id, workspace };
        await discardSyncWorkspace(namespace);
        await syncStorage(native).remove(namespace);
      }
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
        selectWorkspace(target);
      }
      return true;
    } catch (error) {
      if (!currentScope()) return false;
      if (/更新されています|version_conflict/.test(String(error)))
        pending.current.delete(key);
      setNotice(message(error));
      return false;
    } finally {
      if (currentScope()) setBusy(false);
    }
  }
  async function accept() {
    const epoch = authEpoch.current;
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
      if (epoch !== authEpoch.current) return;
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
      <section aria-label="Workspaceへの参加">
        {account}
        {notice && <p role="status">{notice}</p>}
        {invitation && <p>招待リンクを保持しています。</p>}
      </section>
    );
  const workspaceSelector = (
    <>
      {workspaces.items.length > 0 && (
        <label>
          Workspace
          <select
            value={selected}
            onChange={(e) => selectWorkspace(e.target.value)}
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
        <button
          data-variant="secondary"
          disabled={busy}
          onClick={() => root(workspaces.next_cursor, true)}
        >
          Workspaceの次のページ
        </button>
      )}
    </>
  );
  const createForm = (
    <form
      className="auth-form"
      onSubmit={(e) => {
        e.preventDefault();
        void act("create", { name }, undefined);
      }}
    >
      <Field
        label="新しいWorkspaceの名前"
        value={name}
        onChange={setName}
        required
      />
      <button disabled={busy}>Workspaceを作成</button>
    </form>
  );
  const acceptInvite = invitation && (
    <button disabled={busy} onClick={accept}>
      招待を受諾
    </button>
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
    <section className={current ? undefined : "team"} aria-label="Workspace">
      {notice && <p role="status">{notice}</p>}
      {current ? (
        <NavigationProvider
          key={apiIdentity + "/" + me.id + "/" + selected}
          api={apiIdentity}
          user={me.id}
          workspace={selected}
        >
          <WorkspaceShell
            native={native}
            api={apiIdentity}
            user={me.id}
            workspace={selected}
            name={current.name}
            role={current.role}
            canExecute={current.can_execute}
            members={pageWorkspace === selected ? members.items : []}
            membersNext={
              pageWorkspace === selected ? members.next_cursor : undefined
            }
            onScreen={setScreen}
            account={account}
            connection={connection}
            health={health}
            onRevoked={() => {
              setNotice(
                "Workspaceの取得権限が失効しました。本文キャッシュと下書きを削除しました。",
              );
              void root().catch(() => {});
            }}
            selector={workspaceSelector}
            management={
              <>
                <ScreenContent screens={["workspace"]}>
                  <h3>Workspaceの作成・参加</h3>
                  {acceptInvite}
                  {createForm}
                </ScreenContent>
                {pageWorkspace === selected ? (
                  <div className="workspace-management team">
                    <ScreenContent screens={["workspace"]}>
                      <h3>参加者</h3>
                      {count("参加者", members)}
                      <ul>
                        {members.items.map((m) => (
                          <li key={m.id}>
                            <span>
                              {m.name}（{roles[m.role]}）{" "}
                              <StatusBadge
                                label="状態"
                                value={m.active ? "利用中" : "利用停止"}
                                tone={m.active ? "positive" : "warning"}
                              />
                            </span>
                            {admin && (
                              <div className="team-controls">
                                {selectRole(m.role, (role) => {
                                  void act(
                                    "member-update",
                                    {
                                      role,
                                      can_execute: m.can_execute,
                                      active: m.active,
                                    },
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
                          data-variant="secondary"
                          disabled={busy}
                          aria-busy={busy}
                          onClick={() =>
                            void more("members", members, setMembers)
                          }
                        >
                          参加者の次のページ
                        </button>
                      )}
                      <button
                        data-variant="secondary"
                        disabled={busy}
                        onClick={() => {
                          const own = members.items.find((m) => m.id === me.id);
                          if (own) void act("leave", {}, me.id, own.version);
                        }}
                      >
                        このWorkspaceから離脱
                      </button>
                      {admin && (
                        <>
                          <h3>招待</h3>
                          {selectRole(inviteRole, setInviteRole)}
                          <button
                            disabled={busy}
                            onClick={() =>
                              act(
                                "invite-create",
                                { role: inviteRole },
                                undefined,
                              )
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
                          {count("招待", invites)}
                          <ul>
                            {invites.items.map((i) => {
                              const expired =
                                new Date(i.expires_at) < new Date();
                              return (
                                <li key={i.id}>
                                  {roles[i.role]}{" "}
                                  <StatusBadge
                                    label="状態"
                                    value={
                                      i.cancelled
                                        ? "取消済み"
                                        : i.accepted
                                          ? "受諾済み"
                                          : expired
                                            ? "期限切れ"
                                            : `${new Date(i.expires_at).toLocaleString()}まで有効`
                                    }
                                    tone={
                                      i.accepted
                                        ? "positive"
                                        : i.cancelled || expired
                                          ? "negative"
                                          : "neutral"
                                    }
                                  />
                                  <button
                                    data-variant="secondary"
                                    disabled={busy || i.cancelled || i.accepted}
                                    onClick={() =>
                                      act("invite-cancel", {}, i.id, i.version)
                                    }
                                  >
                                    招待を取り消す
                                  </button>
                                </li>
                              );
                            })}
                          </ul>
                          {invites.next_cursor && (
                            <button
                              data-variant="secondary"
                              disabled={busy}
                              aria-busy={busy}
                              onClick={() =>
                                void more("invites", invites, setInvites)
                              }
                            >
                              招待の次のページ
                            </button>
                          )}
                        </>
                      )}
                    </ScreenContent>
                    <ScreenContent screens={["devices"]}>
                      <h3>端末</h3>
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
                          <button
                            disabled={busy}
                            onClick={() => nativeAction("enroll")}
                          >
                            {mine
                              ? "この端末の資格情報を更新"
                              : "この端末を登録"}
                          </button>
                          {mine && (
                            <button
                              data-variant="secondary"
                              disabled={busy}
                              onClick={() => nativeAction("contact")}
                            >
                              この端末の接続を確認
                            </button>
                          )}
                        </div>
                      )}
                      {!native && (
                        <p>端末の登録はWindowsアプリから行います。</p>
                      )}
                      {count("端末", devices)}
                      <ul>
                        {devices.items.map((d) => (
                          <li key={d.id}>
                            <strong>{d.name}</strong>{" "}
                            <StatusBadge
                              label="登録"
                              value={d.enabled ? "登録済み" : "失効"}
                              tone={d.enabled ? "positive" : "negative"}
                            />
                            <p>
                              <StatusBadge
                                label="入力取得"
                                value={d.can_input ? "許可" : "不許可"}
                                tone={d.can_input ? "positive" : "neutral"}
                              />{" "}
                              <StatusBadge
                                label="実行報告"
                                value={d.can_report ? "許可" : "不許可"}
                                tone={d.can_report ? "positive" : "neutral"}
                              />{" "}
                              <StatusBadge
                                label="最終接続"
                                value={
                                  d.last_seen
                                    ? new Date(d.last_seen).toLocaleString()
                                    : "報告なし"
                                }
                              />
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
                                  data-variant="secondary"
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
                          data-variant="secondary"
                          disabled={busy}
                          aria-busy={busy}
                          onClick={() =>
                            void more("devices", devices, setDevices)
                          }
                        >
                          端末の次のページ
                        </button>
                      )}
                      {admin && (
                        <DeliveryPolicy
                          key={selected}
                          native={native}
                          workspace={selected}
                        />
                      )}
                    </ScreenContent>
                  </div>
                ) : (
                  <p role="status">Workspaceの管理情報を取得中…</p>
                )}
              </>
            }
          />
        </NavigationProvider>
      ) : (
        <>
          <h2>Workspaceの作成・参加</h2>
          {workspaceSelector}
          {acceptInvite}
          {createForm}
        </>
      )}
    </section>
  );
}
