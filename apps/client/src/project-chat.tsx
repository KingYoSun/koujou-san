import { useEffect, useRef, useState } from "react";
import { syncRequest, workspaceAccessLost } from "./auth";
import { SyncHTTPError } from "./sync-error";
import { allowAnswerScope, answerScopeEpoch, syncStorage } from "./sync-store";
import {
  NavigationLink,
  ScreenContent,
  useNavigation,
  useScreenActive,
} from "./navigation";
import { ModelConnectionSettings } from "./model-connection-settings";
import {
  modelReady,
  modelSnapshot,
  type ModelProfile,
  type VersionRef,
} from "./model-connections";
import { operationClientID, operationPort } from "./client-operation-port";
import {
  runSelection,
  type ClientOperation,
  type Saved,
  type SelectionPolicy,
} from "./client-selection-host";
import {
  chatBase,
  pin,
  utf8Range,
  type ChatAdoption,
  type ChatLink,
  type ChatMessage,
  type ChatPage,
  type ChatThread,
} from "./chat-api";
import { ChatWork } from "./chat-work";

const stateNames: Record<string, string> = {
  ready: "送信前",
  sending: "モデルへ要求済み・応答待ち",
  completed: "応答完了",
  incomplete: "応答は未完了",
  failed: "応答を取得できませんでした",
  cancelled: "操作を終了",
  waiting: "担当クライアント待ち",
};

export function ProjectChat({
  native,
  api,
  user,
  workspace,
  role,
  canExecute,
}: {
  native: boolean;
  api: string;
  user: string;
  workspace: string;
  role: string;
  canExecute: boolean;
}) {
  const namespace = { api, user, workspace },
    base = chatBase(workspace);
  const navigation = useNavigation(),
    visible = useScreenActive();
  const [project, setProject] = useState<VersionRef>();
  const [projects, setProjects] = useState<
    {
      ref: { id: string; revision: string };
      name: string;
      scope_kind: string;
    }[]
  >([]);
  const [projectNext, setProjectNext] = useState("");
  const [threads, setThreads] = useState<ChatPage>();
  const [thread, setThread] = useState<Saved<ChatThread>>();
  const [timeline, setTimeline] = useState<ChatPage>();
  const [operations, setOperations] = useState<Saved<ClientOperation>[]>([]);
  const [chosen, setChosen] = useState<VersionRef[]>([]);
  const [profile, setProfile] = useState<ModelProfile>();
  const [name, setName] = useState(""),
    [search, setSearch] = useState(""),
    [text, setText] = useState("");
  const [busy, setBusy] = useState(false),
    [notice, setNotice] = useState("");
  const [context, setContext] = useState(true);
  const [excerpt, setExcerpt] = useState<{
    message: Saved<ChatMessage>;
    start: number;
    end: number;
  }>();
  const [adoptName, setAdoptName] = useState("");
  const lifetime = useRef(true),
    serial = useRef(0),
    currentThread = useRef(thread);
  currentThread.current = thread;
  const ids = useRef(new Map<string, string>());
  const canEdit = role !== "viewer";
  useEffect(() => {
    lifetime.current = true;
    return () => {
      lifetime.current = false;
    };
  }, []);
  async function call<T>(path: string, body?: unknown): Promise<T> {
    const epoch = answerScopeEpoch(namespace);
    const out = await syncRequest(
      path.startsWith("/contexts/") ? path : base + path,
      native,
      { method: body === undefined ? "GET" : "POST", body },
    );
    if (!lifetime.current || !allowAnswerScope(namespace, epoch))
      throw Error("chat_scope_changed");
    return out as T;
  }
  const read = <T,>(ref: VersionRef) =>
    call<Saved<T>>(
      `/records/${encodeURIComponent(ref.id)}?version=${encodeURIComponent(ref.version)}`,
    );
  async function mutate(path: string, value: object, version = "0") {
    const key = JSON.stringify([path, version, value]);
    if (!ids.current.has(key)) ids.current.set(key, crypto.randomUUID());
    const result = await call<{ data?: VersionRef }>(path, {
      operation_id: ids.current.get(key),
      expected_version: version,
      ...value,
    });
    if (!result.data) throw Error("chat_mutation_not_accepted");
    return pin(result.data);
  }
  async function act(action: () => Promise<void>) {
    setBusy(true);
    setNotice("");
    try {
      await action();
    } catch (error) {
      if (error instanceof SyncHTTPError && [401, 403].includes(error.status)) {
        ++serial.current;
        setThread(undefined);
        setTimeline(undefined);
        setThreads(undefined);
        setOperations([]);
        setChosen([]);
        setExcerpt(undefined);
        if (await workspaceAccessLost(native, workspace, error))
          await syncStorage(native).remove(namespace);
      }
      setNotice(
        "操作を確認できません。接続・現在の権限・対象版を確認して再取得してください。結果不明のモデル要求は自動再送しません。",
      );
    } finally {
      setBusy(false);
    }
  }
  async function listProjects(after = "") {
    const page = await call<{
      data: { items: typeof projects };
      meta: { next_cursor?: string };
    }>(`/contexts/workspaces/${encodeURIComponent(workspace)}/catalog`, {
      kind: "scope",
      after,
    });
    setProjects(page.data.items.filter((p) => p.scope_kind === "project"));
    setProjectNext(page.meta.next_cursor ?? "");
  }
  async function listThreads(after?: ChatPage, selected = project) {
    if (!selected) return;
    setThreads(
      await call<ChatPage>("/query", {
        kind: "chat-thread",
        root: selected.id,
        search,
        limit: 50,
        descending: true,
        after: after?.after ?? 0,
        after_id: after?.after_id ?? "",
      }),
    );
  }
  async function refresh(ref: VersionRef, after?: ChatPage) {
    const mine = ++serial.current;
    const saved = await read<ChatThread>(ref);
    const page = await call<ChatPage>("/query", {
      root: saved.id,
      limit: 50,
      descending: true,
      after: after?.after ?? 0,
      after_id: after?.after_id ?? "",
    });
    const ops = await Promise.all(
      page.items
        .filter((r) => r.kind === "chat-operation")
        .map((r) =>
          read<ClientOperation>({
            id: (r.value as { operation: VersionRef }).operation.id,
            version: "head",
          }),
        ),
    );
    if (mine !== serial.current) return;
    setThread(saved);
    setProject(saved.value.project);
    setTimeline(page);
    setOperations(ops);
  }
  function open(ref: VersionRef) {
    setChosen([]);
    setExcerpt(undefined);
    navigation?.navigate({
      workspace,
      screen: "chat",
      target: { ...pin(ref), kind: "chat-thread" },
    });
    if (!navigation) void act(() => refresh(ref));
  }
  useEffect(() => {
    const target = navigation?.route.target;
    if (visible && target?.kind === "chat-thread")
      void act(() => refresh(target));
  }, [
    visible,
    navigation?.route.target?.id,
    navigation?.route.target?.version,
  ]);
  async function create() {
    if (!project || !name.trim()) return;
    const key = JSON.stringify([project, name]);
    if (!ids.current.has("thread:" + key))
      ids.current.set("thread:" + key, `chat-thread:${crypto.randomUUID()}`);
    const id = ids.current.get("thread:" + key)!;
    const ref = await mutate(`/chat/threads/${encodeURIComponent(id)}`, {
      value: { name, project: pin(project) },
    });
    setName("");
    ids.current.delete("thread:" + key);
    await listThreads();
    open(ref);
  }
  async function send(ai: boolean) {
    if (!thread || !text.trim()) return;
    if (ai && (!profile || !modelReady(native, api + "\0" + user, profile))) {
      setNotice("利用するモデル接続を設定・選択してください。");
      return;
    }
    const target = pin(thread),
      project = thread.value.project;
    const key = JSON.stringify([target, text]);
    if (!ids.current.has("message:" + key))
      ids.current.set("message:" + key, `chat-message:${crypto.randomUUID()}`);
    const message = await mutate(
      `/chat/messages/${encodeURIComponent(ids.current.get("message:" + key)!)}`,
      { value: { thread: target, role: "human", text, state: "complete" } },
    );
    if (ai) {
      const policy: SelectionPolicy = {
        chat: {
          thread: target,
          messages: [...chosen.filter((r) => r.id !== message.id), message],
        },
        mode: "proposal",
        fixed: { mode: context ? "select" : "none" },
        scopes: context ? [project.id.replace(/^scope:/, "")] : [],
        limits: { model_requests: 32, mcp_requests: 256, seconds: 600 },
      };
      const policyRef = await mutate(`/policies/chat-policy:${message.id}`, {
        value: policy,
      });
      const ref = await mutate(
        `/operations/client-operation:chat:${message.id}`,
        {
          policy: policyRef,
          target,
          purpose: "chat",
          goal: "Projectの相談",
          model: modelSnapshot(profile!, workspace),
          client: operationClientID,
        },
      );
      const operation = await read<ClientOperation>(ref);
      const port = operationPort(
        { native, api, user, workspace },
        profile!,
        ref,
        () => lifetime.current,
      );
      setOperations((current) => [
        operation,
        ...current.filter((r) => r.id !== operation.id),
      ]);
      await runSelection(
        operation,
        policy,
        profile!,
        operationClientID,
        port,
        (update) =>
          setOperations((current) => [
            update,
            ...current.filter((r) => r.id !== update.id),
          ]),
      );
    }
    if (currentThread.current?.id === target.id) {
      setText("");
      setChosen([]);
      await refresh(target);
    }
    ids.current.delete("message:" + key);
  }
  async function adopt() {
    if (!excerpt || !thread) return;
    const range = utf8Range(
      excerpt.message.value.text,
      excerpt.start,
      excerpt.end,
    );
    await mutate(
      `/chat/adoptions/chat-adoption:${excerpt.message.id}:${range.start_utf8}:${range.end_utf8}`,
      { message: pin(excerpt.message), ...range, name: adoptName },
    );
    setExcerpt(undefined);
    setAdoptName("");
    await refresh(thread);
    setNotice(
      "選んだ範囲を版付きContextに保存しました。Runは開始していません。",
    );
  }
  return (
    <ScreenContent>
      <div className="chat-layout">
        <section aria-label="Projectとスレッド" className="chat-sidebar">
          <h3>Projectの相談</h3>
          <button
            disabled={busy}
            onClick={() => void act(() => listProjects())}
          >
            Projectを探す
          </button>
          <ul>
            {projects.map((p) => (
              <li key={p.ref.id}>
                <button
                  onClick={() => {
                    const ref = {
                      id: "scope:" + p.ref.id,
                      version: p.ref.revision,
                    };
                    setProject(ref);
                    void act(() => listThreads(undefined, ref));
                  }}
                >
                  {p.name || p.ref.id}
                </button>
              </li>
            ))}
          </ul>
          {projectNext && (
            <button onClick={() => void act(() => listProjects(projectNext))}>
              Project候補の次ページ
            </button>
          )}
          {project && (
            <>
              <label>
                スレッドを検索
                <input
                  value={search}
                  onChange={(e) => setSearch(e.target.value)}
                />
              </label>
              <button
                disabled={busy}
                onClick={() => void act(() => listThreads())}
              >
                スレッドを検索・更新
              </button>
              <ul>
                {threads?.items.map((r) => (
                  <li key={r.id}>
                    <button
                      aria-current={thread?.id === r.id ? "page" : undefined}
                      onClick={() => open(r)}
                    >
                      {(r.value as ChatThread).name}
                    </button>
                  </li>
                ))}
              </ul>
              {threads?.items.length === 50 && (
                <button onClick={() => void act(() => listThreads(threads))}>
                  スレッドの次ページ
                </button>
              )}
              {canEdit && (
                <form
                  onSubmit={(e) => {
                    e.preventDefault();
                    void act(create);
                  }}
                >
                  <label>
                    新しい相談の題名
                    <input
                      value={name}
                      onChange={(e) => setName(e.target.value)}
                    />
                  </label>
                  <button disabled={busy || !name.trim()}>
                    スレッドを作成
                  </button>
                </form>
              )}
            </>
          )}
        </section>
        <section
          aria-label="会話"
          className="chat-conversation"
          aria-busy={busy}
        >
          {thread ? (
            <>
              <h3>{thread.value.name}</h3>
              <button
                disabled={busy}
                onClick={() => void act(() => refresh(thread))}
              >
                会話と進行中の操作を再取得
              </button>
              <p>
                履歴は50件ずつ取得します。AIへ送る会話を選択してください。今回の入力も送ります。
              </p>
              {timeline?.items.length === 50 && (
                <button
                  disabled={busy}
                  onClick={() => void act(() => refresh(thread, timeline))}
                >
                  以前の会話を取得
                </button>
              )}
              <ol className="chat-timeline">
                {[...(timeline?.items ?? [])].reverse().map((row) => {
                  if (row.kind === "chat-message") {
                    const message = row as Saved<ChatMessage>,
                      selected = chosen.some(
                        (r) => r.id === row.id && r.version === row.version,
                      );
                    return (
                      <li
                        key={row.id}
                        className={`chat-message chat-${message.value.role}`}
                      >
                        <p className="chat-source">
                          {message.value.role === "human"
                            ? `人間 · ${message.value.user === user ? "自分" : message.value.user}`
                            : "AIの応答"}{" "}
                          ·{" "}
                          {message.value.state === "complete"
                            ? "保存済み"
                            : "応答は未完了"}
                        </p>
                        <p className="chat-text">{message.value.text}</p>
                        {message.value.state === "complete" && (
                          <>
                            <label>
                              <input
                                type="checkbox"
                                checked={selected}
                                onChange={() =>
                                  setChosen((current) =>
                                    selected
                                      ? current.filter((r) => r.id !== row.id)
                                      : [...current, pin(row)],
                                  )
                                }
                              />
                              次の相談に含める
                            </label>
                            {canEdit && (
                              <details>
                                <summary>仕様として採用する範囲</summary>
                                <label>
                                  採用する本文を選択
                                  <textarea
                                    readOnly
                                    value={message.value.text}
                                    onSelect={(e) => {
                                      const t = e.currentTarget;
                                      setExcerpt({
                                        message,
                                        start: t.selectionStart,
                                        end: t.selectionEnd,
                                      });
                                    }}
                                  />
                                </label>
                                <button
                                  onClick={() =>
                                    setExcerpt({
                                      message,
                                      start: 0,
                                      end: message.value.text.length,
                                    })
                                  }
                                >
                                  このメッセージ全体を採用範囲にする
                                </button>
                              </details>
                            )}
                          </>
                        )}
                        <details>
                          <summary>固定版・原応答への参照</summary>
                          <p>
                            {row.id} / {row.version}
                          </p>
                          {message.value.operation && (
                            <button
                              onClick={() =>
                                void act(async () =>
                                  setOperations([
                                    await read<ClientOperation>({
                                      id: message.value.operation!.id,
                                      version: "head",
                                    }),
                                  ]),
                                )
                              }
                            >
                              モデル操作の現在の記録を確認
                            </button>
                          )}
                        </details>
                      </li>
                    );
                  }
                  if (row.kind === "chat-adoption") {
                    const value = row.value as ChatAdoption;
                    return (
                      <li key={row.id}>
                        <p>仕様をContextへ採用済み</p>
                        <NavigationLink
                          related
                          to={{
                            workspace,
                            screen: "context",
                            target: {
                              kind: value.context.kind,
                              id: value.context.id,
                              version: value.context.revision,
                            },
                          }}
                        >
                          保存した仕様を確認
                        </NavigationLink>
                        <ChatWork
                          native={native}
                          api={api}
                          user={user}
                          workspace={workspace}
                          role={role}
                          canExecute={canExecute}
                          thread={pin(thread)}
                          adoption={value}
                        />
                      </li>
                    );
                  }
                  if (row.kind === "chat-link")
                    return (
                      <li key={row.id}>
                        <ChatWork
                          native={native}
                          api={api}
                          user={user}
                          workspace={workspace}
                          role={role}
                          canExecute={canExecute}
                          thread={pin(thread)}
                          link={row.value as ChatLink}
                        />
                      </li>
                    );
                  return null;
                })}
              </ol>
              {operations.map((op) => (
                <section
                  key={op.id}
                  className="chat-operation"
                  aria-label="モデル操作"
                >
                  <p role="status">
                    {stateNames[op.value.state] ?? op.value.state}
                    {op.value.reason && ` · ${op.value.reason}`}
                  </p>
                  {op.value.state === "sending" &&
                    op.value.client !== operationClientID && (
                      <p>
                        別クライアントで送信済みです。結果は未確認です。再表示からは再送しません。
                      </p>
                    )}
                  <details>
                    <summary>操作と原応答を確認</summary>
                    <p>
                      {op.id} / {op.version}
                    </p>
                    <button
                      onClick={() =>
                        void act(async () => {
                          if (!op.value.turn) return;
                          const turn = await read(op.value.turn);
                          setNotice(JSON.stringify(turn.value, null, 2));
                        })
                      }
                    >
                      保存した原応答を取得
                    </button>
                  </details>
                </section>
              ))}
              {excerpt && excerpt.end > excerpt.start && (
                <section aria-label="仕様の採用">
                  <h4>採用する範囲</h4>
                  <p className="chat-text">
                    {excerpt.message.value.text.slice(
                      excerpt.start,
                      excerpt.end,
                    )}
                  </p>
                  <label>
                    Contextの名前
                    <input
                      value={adoptName}
                      onChange={(e) => setAdoptName(e.target.value)}
                    />
                  </label>
                  <button
                    disabled={busy || !canEdit}
                    onClick={() => void act(adopt)}
                  >
                    この範囲をContextに保存
                  </button>
                </section>
              )}
              {canEdit && (
                <form
                  onSubmit={(e) => {
                    e.preventDefault();
                    void act(() => send(true));
                  }}
                >
                  <label>
                    相談・メッセージ
                    <textarea
                      value={text}
                      onChange={(e) => setText(e.target.value)}
                    />
                  </label>
                  <p>次の相談に含める履歴: {chosen.length}件</p>
                  <label>
                    <input
                      type="checkbox"
                      checked={context}
                      onChange={(e) => setContext(e.target.checked)}
                    />
                    このProjectのContextを検索して参照できるようにする
                  </label>
                  <ModelConnectionSettings
                    native={native}
                    actor={api + "\0" + user}
                    workspace={workspace}
                    selected={profile}
                    onSelected={setProfile}
                    selectionOnly
                  />
                  <button disabled={busy || !text.trim()}>
                    保存してAIへ相談
                  </button>
                  <button
                    type="button"
                    disabled={busy || !text.trim()}
                    onClick={() => void act(() => send(false))}
                  >
                    メッセージだけを保存
                  </button>
                </form>
              )}
              <ChatWork
                native={native}
                api={api}
                user={user}
                workspace={workspace}
                role={role}
                canExecute={canExecute}
                thread={pin(thread)}
                onLinked={() => refresh(thread)}
              />
            </>
          ) : (
            <p>
              Projectを選び、スレッドを開くか新しい相談を始めてください。IssueやRunは不要です。
            </p>
          )}
          <p role="status" className="chat-notice" tabIndex={-1}>
            {notice}
          </p>
        </section>
      </div>
    </ScreenContent>
  );
}
