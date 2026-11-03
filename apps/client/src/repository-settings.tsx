import { useEffect, useRef, useState } from "react";
import { syncRequest } from "./auth";
import { SyncHTTPError } from "./sync-error";
import { ScreenContent, useNavigation, useScreenActive } from "./navigation";
import { Tabs } from "./tabs";
import { ScopePicker } from "./scope-picker";
import { RepositoryObservations } from "./repository-observations";
import { repositoryStates, type Repository } from "./repository-picker";
import { GiteaOAuth } from "./gitea-oauth";

type Connection = {
  auth?: string;
  account?: string;
  id: string;
  name: string;
  instance: string;
  allow_create: boolean;
  version: string;
};
type Page<T> = { items: T[]; next?: string };
export function RepositorySettings({
  native,
  workspace,
  role,
  serverAdmin = false,
}: {
  native: boolean;
  workspace: string;
  role: string;
  serverAdmin?: boolean;
}) {
  const active = useScreenActive(),
    navigation = useNavigation();
  const projectScope =
    navigation?.route.target?.kind === "scope"
      ? navigation.route.target.id.replace(/^scope:/, "")
      : "";
  const admin = role === "admin";
  const [tab, setTab] = useState("repositories");
  const [connections, setConnections] = useState<Page<Connection>>();
  const [repositories, setRepositories] = useState<Page<Repository>>();
  const [connection, setConnection] = useState<Connection>();
  const [connectionDraft, setConnectionDraft] = useState<Connection>();
  const [token, setToken] = useState(""),
    [ca, setCA] = useState("");
  const [repository, setRepository] = useState<Repository>();
  const [mode, setMode] = useState("register");
  const [owner, setOwner] = useState(""),
    [repoName, setRepoName] = useState("");
  const [privacy, setPrivacy] = useState("");
  const [owners, setOwners] = useState<{ login: string; type: string }[]>();
  const [ownerPage, setOwnerPage] = useState(1);
  const [busy, setBusy] = useState(false),
    [notice, setNotice] = useState("");
  const [github, setGithub] = useState({
    repository: "",
    repository_id: "",
    installation_id: "",
    kind: "issue",
    external_id: "",
  });
  const pending = useRef(new Map<string, string>());
  const permission = useRef(0);
  const base = `/forge/workspaces/${encodeURIComponent(workspace)}`;
  const call = async <T,>(path: string, body?: unknown) => {
    const epoch = permission.current;
    const result = await syncRequest(base + path, native, {
      method: body === undefined ? "GET" : "POST",
      body,
    });
    if (epoch !== permission.current)
      throw Error("repository_permission_changed");
    return result as T;
  };
  async function work(action: () => Promise<void>) {
    setBusy(true);
    setNotice("");
    try {
      await action();
    } catch (error) {
      if (error instanceof SyncHTTPError && [401, 403].includes(error.status)) {
        permission.current++;
        setConnections(undefined);
        setRepositories(undefined);
        setRepository(undefined);
        setConnection(undefined);
        setConnectionDraft(undefined);
        setToken("");
        setCA("");
        setOwners(undefined);
        pending.current.clear();
      }
      setNotice("設定を確認できません。接続・権限・現在版を確認してください。");
    } finally {
      setBusy(false);
    }
  }
  async function write(path: string, version: string, value: object) {
    const key = JSON.stringify([path, version, value]);
    if (!pending.current.has(key))
      pending.current.set(key, crypto.randomUUID());
    const result = await call<{ data: { version: string } }>(path, {
      ...value,
      operation_id: pending.current.get(key),
      expected_version: version,
    });
    pending.current.delete(key);
    return result.data.version;
  }
  const loadConnections = async (after = "") =>
    setConnections(
      await call<Page<Connection>>(
        `/connections?after=${encodeURIComponent(after)}`,
      ),
    );
  const loadRepositories = async (after = "") =>
    setRepositories(
      await call<Page<Repository>>(
        `/repositories?after=${encodeURIComponent(after)}&project=${encodeURIComponent(projectScope)}`,
      ),
    );
  useEffect(() => {
    if (active && projectScope) setTab("repositories");
    if (active)
      void work(async () => {
        await Promise.all([loadConnections(), loadRepositories()]);
      });
  }, [active, projectScope]);
  function freshRepository(create = false) {
    setMode(create ? "create" : "register");
    setOwner("");
    setRepoName("");
    setPrivacy("");
    setOwners(undefined);
    const target = navigation?.route.target;
    setRepository({
      id: crypto.randomUUID(),
      version: "0",
      connection: connection?.id ?? "",
      connection_version: "",
      instance: "",
      full_name: "",
      repository_id: 0,
      project: target?.kind === "scope" ? target.id.replace(/^scope:/, "") : "",
      private: false,
      auto_init: false,
      state: "",
    });
  }
  async function saveConnection() {
    if (!connectionDraft) return;
    const version = await write(
      `/connections/${encodeURIComponent(connectionDraft.id)}`,
      connectionDraft.version,
      {
        name: connectionDraft.name,
        instance: connectionDraft.instance,
        allow_create: connectionDraft.allow_create,
        token,
        ca,
      },
    );
    setToken("");
    setCA("");
    setConnectionDraft({ ...connectionDraft, version });
    await loadConnections();
    setNotice("共有接続を保存しました。");
  }
  async function listOwners(page = 1) {
    if (!connection) return;
    setOwners(
      await call(
        `/connections/${encodeURIComponent(connection.id)}/owners?page=${page}`,
      ),
    );
    setOwnerPage(page);
  }
  async function saveRepository() {
    if (!repository || !connection) return;
    const full_name =
      mode === "create" ? `${owner}/${repoName}` : repository.full_name;
    await write(
      `/repositories/${encodeURIComponent(repository.id)}/${mode}`,
      repository.version,
      {
        connection: connection.id,
        project: repository.project ?? "",
        full_name,
        ...(mode === "create"
          ? { private: privacy === "private", auto_init: repository.auto_init }
          : {}),
      },
    );
    const saved = await call<Repository>(
      `/repositories/${encodeURIComponent(repository.id)}`,
    );
    setRepository(saved);
    if (saved.repository_id > 0) setMode("register");
    await loadRepositories();
  }
  async function openRepository(value: Repository) {
    setRepository(value);
    setMode("register");
    const selected = connections?.items.find(
      (item) => item.id === value.connection,
    );
    if (selected) setConnection(selected);
    else {
      setConnection(
        await call<Connection>(
          `/connections/${encodeURIComponent(value.connection)}`,
        ),
      );
    }
  }
  const githubTarget = `github-${workspace}-${github.repository_id}-${github.kind}-${github.external_id}`;
  const githubHandoff = {
    observation: {
      targets: [
        {
          id: githubTarget,
          repository: github.repository,
          repository_id: Number(github.repository_id),
          installation_id: Number(github.installation_id),
          kind: github.kind,
          external_id: github.external_id,
        },
      ],
      bindings: [{ workspace_id: workspace, target_id: githubTarget }],
    },
    publication: {
      connections: [
        {
          id: githubTarget,
          workspace_id: workspace,
          repository: github.repository,
          repository_id: Number(github.repository_id),
          installation_id: Number(github.installation_id),
        },
      ],
    },
  };
  return (
    <>
      <ScreenContent>
        <section aria-label="リポジトリ接続">
          <Tabs
            id="repository-settings"
            label="リポジトリ設定"
            items={[
              { id: "repositories", label: "リポジトリ" },
              { id: "connections", label: "共有接続" },
            ]}
            value={tab}
            onChange={setTab}
          />
          {tab === "connections" ? (
            <>
              <button
                disabled={busy}
                onClick={() => void work(() => loadConnections())}
              >
                共有接続を再取得
              </button>
              <ul>
                {connections?.items.map((item) => (
                  <li key={item.id}>
                    {admin ? (
                      <button
                        onClick={() => {
                          setConnection(item);
                          setConnectionDraft(item);
                          setToken("");
                          setCA("");
                        }}
                      >
                        {item.name} · {item.instance}
                      </button>
                    ) : (
                      <span>
                        {item.name} · {item.instance}
                      </span>
                    )}
                  </li>
                ))}
              </ul>
              {connections?.next && (
                <button
                  onClick={() =>
                    void work(() => loadConnections(connections.next))
                  }
                >
                  共有接続の次のページ
                </button>
              )}
              {(admin || serverAdmin) && (
                <GiteaOAuth
                  native={native}
                  workspace={workspace}
                  serverAdmin={serverAdmin}
                  canManage={admin}
                  selected={connectionDraft}
                  onSaved={async () => {
                    setConnectionDraft(undefined);
                    await loadConnections();
                  }}
                />
              )}
              {admin && (
                <>
                  <button
                    onClick={() => {
                      setConnectionDraft({
                        id: crypto.randomUUID(),
                        name: "",
                        instance: "",
                        allow_create: false,
                        version: "0",
                      });
                      setToken("");
                      setCA("");
                    }}
                  >
                    トークンでGitea接続を追加
                  </button>
                  {connectionDraft && (
                    <form
                      onSubmit={(event) => {
                        event.preventDefault();
                        void work(saveConnection);
                      }}
                    >
                      <fieldset disabled={busy}>
                        <legend>Gitea共有接続</legend>
                        <label>
                          接続名
                          <input
                            required
                            value={connectionDraft.name}
                            onChange={(event) =>
                              setConnectionDraft({
                                ...connectionDraft,
                                name: event.target.value,
                              })
                            }
                          />
                        </label>
                        <label>
                          Gitea URL
                          <input
                            type="url"
                            required
                            readOnly={connectionDraft.version !== "0"}
                            value={connectionDraft.instance}
                            onChange={(event) =>
                              setConnectionDraft({
                                ...connectionDraft,
                                instance: event.target.value,
                              })
                            }
                          />
                        </label>
                        {connectionDraft.auth === "oauth" ? (
                          <output>{connectionDraft.account}</output>
                        ) : (
                          <label>
                            {connectionDraft.version === "0"
                              ? "アクセストークン"
                              : "アクセストークンを置換"}
                            <input
                              type="password"
                              autoComplete="new-password"
                              required={connectionDraft.version === "0"}
                              value={token}
                              onChange={(event) => setToken(event.target.value)}
                            />
                          </label>
                        )}
                        {connectionDraft.auth !== "oauth" && (
                          <label>
                            CA証明書（PEM）
                            <textarea
                              rows={3}
                              value={ca}
                              onChange={(event) => setCA(event.target.value)}
                            />
                          </label>
                        )}
                        <label>
                          <input
                            type="checkbox"
                            checked={connectionDraft.allow_create}
                            onChange={(event) =>
                              setConnectionDraft({
                                ...connectionDraft,
                                allow_create: event.target.checked,
                              })
                            }
                          />
                          このWorkspaceでリポジトリ作成を許可
                        </label>
                        <button>共有接続を保存</button>
                      </fieldset>
                    </form>
                  )}
                  <details>
                    <summary>GitHub接続の引継ぎ</summary>
                    {Object.entries({
                      repository: "リポジトリ（所有者/名前）",
                      repository_id: "リポジトリID",
                      installation_id: "Installation ID",
                      external_id: "外部対象ID",
                    }).map(([key, label]) => (
                      <label key={key}>
                        {label}
                        <input
                          value={github[key as keyof typeof github]}
                          onChange={(event) =>
                            setGithub({ ...github, [key]: event.target.value })
                          }
                        />
                      </label>
                    ))}
                    <label>
                      観測の種類
                      <select
                        value={github.kind}
                        onChange={(event) =>
                          setGithub({ ...github, kind: event.target.value })
                        }
                      >
                        {["issue", "pr", "commit", "actions"].map((kind) => (
                          <option key={kind}>{kind}</option>
                        ))}
                      </select>
                    </label>
                    {github.repository &&
                      github.repository_id &&
                      github.installation_id &&
                      github.external_id && (
                        <a
                          download="github-workspace-setup.json"
                          href={`data:application/json;charset=utf-8,${encodeURIComponent(JSON.stringify(githubHandoff, null, 2))}`}
                        >
                          接続設定を出力
                        </a>
                      )}
                  </details>
                </>
              )}
            </>
          ) : (
            <>
              <button
                disabled={busy}
                onClick={() => void work(() => loadRepositories())}
              >
                リポジトリを再取得
              </button>
              <ul>
                {repositories?.items.map((item) => (
                  <li key={item.id}>
                    {admin ? (
                      <button
                        onClick={() => void work(() => openRepository(item))}
                      >
                        {item.full_name} ·{" "}
                        {repositoryStates[item.state] ?? item.state}
                      </button>
                    ) : item.repository_id > 0 ? (
                      <a
                        href={`${item.instance}/${item.full_name}`}
                        target="_blank"
                        rel="noreferrer"
                      >
                        {item.full_name}
                      </a>
                    ) : (
                      <span>
                        {item.full_name} ·{" "}
                        {repositoryStates[item.state] ?? item.state}
                      </span>
                    )}
                  </li>
                ))}
              </ul>
              {repositories?.next && (
                <button
                  onClick={() =>
                    void work(() => loadRepositories(repositories.next))
                  }
                >
                  リポジトリの次のページ
                </button>
              )}
              {repositories && !repositories.items.length && (
                <p>リポジトリはありません。</p>
              )}
              {admin && (
                <>
                  <button onClick={() => freshRepository(false)}>
                    既存のリポジトリを登録
                  </button>
                  <button onClick={() => freshRepository(true)}>
                    Giteaリポジトリを作成
                  </button>
                  <button
                    data-variant="secondary"
                    onClick={() => setTab("connections")}
                  >
                    共有接続を設定
                  </button>
                </>
              )}
              {repository && admin && (
                <form
                  onSubmit={(event) => {
                    event.preventDefault();
                    void work(saveRepository);
                  }}
                >
                  <fieldset disabled={busy}>
                    <legend>
                      {mode === "create"
                        ? "Giteaリポジトリの新規作成"
                        : "リポジトリの登録"}
                    </legend>
                    <label>
                      共有接続
                      <select
                        value={connection?.id ?? ""}
                        disabled={repository.version !== "0"}
                        onChange={(event) => {
                          const value = connections?.items.find(
                            (item) => item.id === event.target.value,
                          );
                          setConnection(value);
                          setOwner("");
                          setOwners(undefined);
                        }}
                      >
                        <option value="">未選択</option>
                        {connection &&
                          !connections?.items.some(
                            (item) => item.id === connection.id,
                          ) && (
                            <option value={connection.id}>
                              {connection.name}
                            </option>
                          )}
                        {connections?.items.map((item) => (
                          <option key={item.id} value={item.id}>
                            {item.name}
                          </option>
                        ))}
                      </select>
                    </label>
                    {connections?.next && (
                      <button
                        type="button"
                        onClick={() =>
                          void work(() => loadConnections(connections.next))
                        }
                      >
                        共有接続の次のページ
                      </button>
                    )}
                    {mode === "create" ? (
                      <>
                        <button
                          type="button"
                          disabled={!connection}
                          onClick={() => void work(() => listOwners())}
                        >
                          所有者を取得
                        </button>
                        <label>
                          所有者
                          <select
                            required
                            value={owner}
                            onChange={(event) => setOwner(event.target.value)}
                          >
                            <option value="">未選択</option>
                            {owner &&
                              !owners?.some((item) => item.login === owner) && (
                                <option value={owner}>{owner}</option>
                              )}
                            {owners?.map((item) => (
                              <option key={item.login} value={item.login}>
                                {item.login}
                              </option>
                            ))}
                          </select>
                        </label>
                        {!!owners && owners.length >= 50 && (
                          <button
                            type="button"
                            onClick={() =>
                              void work(() => listOwners(ownerPage + 1))
                            }
                          >
                            所有者の次のページ
                          </button>
                        )}
                        <label>
                          リポジトリ名
                          <input
                            required
                            value={repoName}
                            onChange={(event) =>
                              setRepoName(event.target.value)
                            }
                          />
                        </label>
                        <label>
                          公開範囲
                          <select
                            required
                            value={privacy}
                            onChange={(event) => setPrivacy(event.target.value)}
                          >
                            <option value="">未選択</option>
                            <option value="private">非公開</option>
                            <option value="public">公開</option>
                          </select>
                        </label>
                        <label>
                          <input
                            type="checkbox"
                            checked={repository.auto_init}
                            onChange={(event) =>
                              setRepository({
                                ...repository,
                                auto_init: event.target.checked,
                              })
                            }
                          />
                          READMEで初期化
                        </label>
                      </>
                    ) : (
                      <label>
                        リポジトリ（所有者/名前）
                        <input
                          required
                          readOnly={repository.version !== "0"}
                          value={repository.full_name}
                          onChange={(event) =>
                            setRepository({
                              ...repository,
                              full_name: event.target.value,
                            })
                          }
                        />
                      </label>
                    )}
                    <ScopePicker
                      native={native}
                      workspace={workspace}
                      kind="project"
                      value={repository.project ?? ""}
                      onChange={(project) =>
                        setRepository({ ...repository, project })
                      }
                    />
                    {repository.state && (
                      <p role="status">
                        {repositoryStates[repository.state] ?? repository.state}
                      </p>
                    )}
                    {mode === "create" ? (
                      <button
                        disabled={
                          !connection?.allow_create ||
                          !owner ||
                          !repoName ||
                          !privacy ||
                          repository.version !== "0"
                        }
                      >
                        リポジトリを作成
                      </button>
                    ) : (
                      <button disabled={!connection}>
                        リポジトリ設定を保存
                      </button>
                    )}
                    {["intent", "unknown"].includes(repository.state) && (
                      <button
                        type="button"
                        onClick={() =>
                          void work(async () => {
                            await write(
                              `/repositories/${encodeURIComponent(repository.id)}/reconcile`,
                              repository.version,
                              {},
                            );
                            setRepository(
                              await call(
                                `/repositories/${encodeURIComponent(repository.id)}`,
                              ),
                            );
                            await loadRepositories();
                          })
                        }
                      >
                        作成結果を照合
                      </button>
                    )}
                    {repository.repository_id > 0 && (
                      <>
                        <a
                          href={`${repository.instance}/${repository.full_name}`}
                          target="_blank"
                          rel="noreferrer"
                        >
                          リポジトリを開く
                        </a>
                      </>
                    )}
                  </fieldset>
                </form>
              )}
            </>
          )}
          {!admin && (
            <p role="status">接続設定の変更には管理者権限が必要です。</p>
          )}
          {notice && <p role="status">{notice}</p>}
        </section>
      </ScreenContent>
      {repository && repository.repository_id > 0 && admin && (
        <RepositoryObservations
          key={repository.id}
          id={repository.id}
          workspace={workspace}
          enabled={tab === "repositories"}
          busy={busy}
          call={call}
          write={write}
          work={work}
        />
      )}
    </>
  );
}
