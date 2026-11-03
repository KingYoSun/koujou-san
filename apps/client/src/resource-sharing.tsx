import { useEffect, useState } from "react";
import { syncRequest } from "./auth";
import { SyncHTTPError } from "./sync-error";
import { useNavigation, useScreenActive, ScreenContent } from "./navigation";

type Grant = {
  user: string;
  name: string;
  read: boolean;
  update: boolean;
  execute: boolean;
};
type Policy = {
  id: string;
  restricted: boolean;
  version: string;
  grants: Grant[];
  next?: string;
};
export function ResourceSharing({
  native,
  workspace,
  role,
}: {
  native: boolean;
  workspace: string;
  role: string;
}) {
  const navigation = useNavigation(),
    visible = useScreenActive();
  const [id, setID] = useState("");
  const [policy, setPolicy] = useState<Policy>();
  const [grant, setGrant] = useState<Grant>({
    user: "",
    name: "",
    read: true,
    update: false,
    execute: false,
  });
  const [members, setMembers] =
    useState<{ id: string; name: string; active: boolean }[]>();
  const [memberNext, setMemberNext] = useState("");
  const [busy, setBusy] = useState(false),
    [notice, setNotice] = useState("");
  const base = `/access/workspaces/${encodeURIComponent(workspace)}`;
  async function work(action: () => Promise<void>) {
    setBusy(true);
    setNotice("");
    try {
      await action();
    } catch (error) {
      if (error instanceof SyncHTTPError && [401, 403].includes(error.status)) {
        setPolicy(undefined);
        setMembers(undefined);
        setGrant({
          user: "",
          name: "",
          read: true,
          update: false,
          execute: false,
        });
      }
      setNotice(
        "共有設定を確認できません。対象・権限・現在版を確認してください。",
      );
    } finally {
      setBusy(false);
    }
  }
  async function load(target = id, after = "") {
    const value = (await syncRequest(
      `${base}/resources/${encodeURIComponent(target)}${after ? `?after=${encodeURIComponent(after)}` : ""}`,
      native,
      { method: "GET" },
    )) as Policy;
    setPolicy(value);
    setID(target);
  }
  async function listMembers(after = "") {
    const page = (await syncRequest(
      `${base}/members?after=${encodeURIComponent(after)}`,
      native,
      { method: "GET" },
    )) as { items: NonNullable<typeof members>; next_cursor?: string };
    setMembers(page.items);
    setMemberNext(page.next_cursor ?? "");
  }
  async function save(value: object) {
    if (!policy) return;
    await syncRequest(
      `${base}/resources/${encodeURIComponent(policy.id)}`,
      native,
      {
        method: "POST",
        body: {
          ...value,
          operation_id: crypto.randomUUID(),
          expected_version: policy.version,
        },
      },
    );
    await load(policy.id);
    setNotice("共有設定を保存しました。");
  }
  useEffect(() => {
    const target =
      navigation?.route.view === "resource-policy"
        ? navigation.route.target
        : undefined;
    if (visible && role === "admin" && target) void work(() => load(target.id));
  }, [visible, navigation?.route.target?.id, navigation?.route.view]);
  if (role !== "admin") return null;
  return (
    <ScreenContent>
      <section aria-label="資源の共有設定">
        <h3>資源の共有</h3>
        <form
          onSubmit={(event) => {
            event.preventDefault();
            void work(() => load());
          }}
        >
          <label>
            資源ID
            <input value={id} onChange={(event) => setID(event.target.value)} />
          </label>
          <button disabled={busy || !id}>共有設定を取得</button>
        </form>
        {policy && (
          <fieldset disabled={busy}>
            <legend>{policy.id}</legend>
            <label>
              <input
                type="checkbox"
                checked={policy.restricted}
                onChange={(event) =>
                  setPolicy({ ...policy, restricted: event.target.checked })
                }
              />
              閲覧者を制限
            </label>
            <button
              onClick={() =>
                void work(() => save({ restricted: policy.restricted }))
              }
            >
              共有範囲を保存
            </button>
            <ul>
              {policy.grants.map((item) => (
                <li key={item.user}>
                  <button onClick={() => setGrant(item)}>
                    {item.name || item.user} · {item.read ? "閲覧" : "権限なし"}
                    {item.update ? "・編集" : ""}
                    {item.execute ? "・実行" : ""}
                  </button>
                </li>
              ))}
            </ul>
            {policy.next && (
              <button
                onClick={() => void work(() => load(policy.id, policy.next))}
              >
                権限の次のページ
              </button>
            )}
            <button onClick={() => void work(() => listMembers())}>
              参加者を取得
            </button>
            <label>
              参加者
              <select
                value={grant.user}
                onChange={(event) =>
                  setGrant({ ...grant, user: event.target.value })
                }
              >
                <option value="">選択してください</option>
                {grant.user &&
                  !members?.some((member) => member.id === grant.user) && (
                    <option value={grant.user}>
                      {grant.name || grant.user}
                    </option>
                  )}
                {members
                  ?.filter((member) => member.active)
                  .map((member) => (
                    <option key={member.id} value={member.id}>
                      {member.name || member.id}
                    </option>
                  ))}
              </select>
            </label>
            {memberNext && (
              <button onClick={() => void work(() => listMembers(memberNext))}>
                参加者の次のページ
              </button>
            )}
            {(["read", "update", "execute"] as const).map((key, index) => (
              <label key={key}>
                <input
                  type="checkbox"
                  checked={grant[key]}
                  onChange={(event) =>
                    setGrant({
                      ...grant,
                      [key]: event.target.checked,
                      ...(key === "read" && !event.target.checked
                        ? { update: false, execute: false }
                        : key !== "read" && event.target.checked
                          ? { read: true }
                          : {}),
                    })
                  }
                />
                {["閲覧", "編集", "実行"][index]}
              </label>
            ))}
            <button
              disabled={!grant.user}
              onClick={() => void work(() => save({ grant }))}
            >
              参加者の権限を保存
            </button>
          </fieldset>
        )}
        {notice && <p role="status">{notice}</p>}
      </section>
    </ScreenContent>
  );
}
