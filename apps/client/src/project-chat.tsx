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
import { DetailPane } from "./panes";
import { Disclosure } from "./disclosure";
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
import { operationStateLabels } from "./client-selection";
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

export function ProjectChat({
  native,
  api,
  user,
  workspace,
  role,
  canExecute,
  embedded = false,
  initialThread,
  onThreadOpened,
}: {
  native: boolean;
  api: string;
  user: string;
  workspace: string;
  role: string;
  canExecute: boolean;
  // Popover use: keep the thread local instead of moving the screen.
  embedded?: boolean;
  initialThread?: VersionRef;
  onThreadOpened?: (ref: VersionRef) => void;
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
  >();
  const [projectNext, setProjectNext] = useState("");
  const [threads, setThreads] = useState<ChatPage>();
  const [thread, setThread] = useState<Saved<ChatThread>>();
  const [timeline, setTimeline] = useState<ChatPage>();
  const [operations, setOperations] = useState<Saved<ClientOperation>[]>([]);
  const [turns, setTurns] = useState<Record<string, string>>({});
  const [chosen, setChosen] = useState<VersionRef[]>([]);
  const [profile, setProfile] = useState<ModelProfile>();
  const [name, setName] = useState(""),
    [search, setSearch] = useState(""),
    [text, setText] = useState("");
  const [busy, setBusy] = useState(false),
    [notice, setNotice] = useState("");
  const [context, setContext] = useState(true);
  const [focused, setFocused] = useState<Saved<ChatMessage>>();
  const [excerpt, setExcerpt] = useState<{
    message: Saved<ChatMessage>;
    start: number;
    end: number;
  }>();
  const [adoptName, setAdoptName] = useState("");
  const scroller = useRef<HTMLDivElement>(null);
  const [atBottom, setAtBottomState] = useState(true);
  const bottom = useRef(true);
  const setAtBottom = (value: boolean) => {
    bottom.current = value;
    setAtBottomState(value);
  };
  const anchor = useRef<"bottom" | "top" | "keep">("bottom");
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
        setFocused(undefined);
        if (await workspaceAccessLost(native, workspace, error))
          await syncStorage(native).remove(namespace);
      }
      setNotice(
        "操作を確認できません。接続・権限・対象版を確認して再取得してください。",
      );
    } finally {
      setBusy(false);
    }
  }
  async function listProjects(after = "") {
    const page = await call<{
      data: { items: NonNullable<typeof projects> };
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
    if (currentThread.current?.id !== saved.id)
      await listThreads(undefined, saved.value.project);
    if (mine !== serial.current) return;
    anchor.current = after ? "top" : bottom.current ? "bottom" : "keep";
    setThread(saved);
    setProject(saved.value.project);
    setTimeline(page);
    setOperations(ops);
  }
  function open(ref: VersionRef) {
    setChosen([]);
    setExcerpt(undefined);
    setFocused(undefined);
    setAtBottom(true);
    onThreadOpened?.(pin(ref));
    if (navigation && !embedded)
      navigation.navigate({
        workspace,
        screen: "chat",
        target: { ...pin(ref), kind: "chat-thread" },
      });
    else void act(() => refresh(ref));
  }
  useEffect(() => {
    if (embedded) return;
    const target = navigation?.route.target;
    if (visible && target?.kind === "chat-thread")
      void act(() => refresh(target));
  }, [
    visible,
    navigation?.route.target?.id,
    navigation?.route.target?.version,
  ]);
  useEffect(() => {
    if (embedded && initialThread) void act(() => refresh(initialThread));
  }, []);
  useEffect(() => {
    if (visible && !projects) void act(() => listProjects());
  }, [visible]);
  useEffect(() => {
    const element = scroller.current;
    if (!element) return;
    if (anchor.current === "top") element.scrollTop = 0;
    else if (anchor.current === "bottom") {
      element.scrollTop = element.scrollHeight;
      setAtBottom(true);
    }
    anchor.current = "bottom";
  }, [timeline, operations]);
  const track = () => {
    const element = scroller.current;
    if (element)
      setAtBottom(
        element.scrollHeight - element.scrollTop - element.clientHeight < 8,
      );
  };
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
      setAtBottom(true);
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
    setNotice("選んだ範囲を版付きContextに保存しました。");
  }
  const work = (props: { adoption?: ChatAdoption; link?: ChatLink }) =>
    thread && (
      <ChatWork
        native={native}
        api={api}
        user={user}
        workspace={workspace}
        role={role}
        canExecute={canExecute}
        thread={pin(thread)}
        onLinked={() => refresh(thread)}
        {...props}
      />
    );
  const author = (message: ChatMessage) =>
    message.role === "human"
      ? message.user === user
        ? "自分"
        : message.user || "人間"
      : "AIの応答";
  return (
    <ScreenContent>
      <div className="chat-layout" data-embedded={embedded || undefined}>
        <aside className="chat-sidebar" aria-label="Projectとスレッド">
          <div className="chat-sidebar-section">
            <p className="chat-sidebar-title">Project</p>
            <button
              data-variant="secondary"
              disabled={busy}
              aria-busy={busy}
              onClick={() => void act(() => listProjects())}
            >
              Projectを再取得
            </button>
            {projects && (
              <p>
                {projects.length
                  ? `${projects.length}件を表示${projectNext ? "（続きあり）" : ""}`
                  : "Projectはありません。"}
              </p>
            )}
            <ul className="chat-channels">
              {projects?.map((p) => (
                <li key={p.ref.id}>
                  <button
                    aria-current={
                      project?.id === "scope:" + p.ref.id ? "true" : undefined
                    }
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
              <button
                data-variant="secondary"
                onClick={() => void act(() => listProjects(projectNext))}
              >
                Project候補の次のページ
              </button>
            )}
          </div>
          {project && (
            <div className="chat-sidebar-section">
              <p className="chat-sidebar-title">スレッド</p>
              <form
                onSubmit={(e) => {
                  e.preventDefault();
                  void act(() => listThreads());
                }}
              >
                <label>
                  スレッドを検索
                  <input
                    value={search}
                    onChange={(e) => setSearch(e.target.value)}
                  />
                </label>
                <button
                  data-variant="secondary"
                  disabled={busy}
                  aria-busy={busy}
                >
                  スレッドを検索
                </button>
              </form>
              {threads && (
                <p>
                  {threads.items.length
                    ? `${threads.items.length}件を表示${threads.items.length === 50 ? "（続きあり）" : ""}`
                    : "スレッドはありません。"}
                </p>
              )}
              <ul className="chat-channels">
                {threads?.items.map((r) => (
                  <li key={r.id}>
                    <button
                      aria-current={thread?.id === r.id ? "true" : undefined}
                      onClick={() => open(r)}
                    >
                      # {(r.value as ChatThread).name}
                    </button>
                  </li>
                ))}
              </ul>
              {threads?.items.length === 50 && (
                <button
                  data-variant="secondary"
                  onClick={() => void act(() => listThreads(threads))}
                >
                  スレッドの次のページ
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
            </div>
          )}
        </aside>
        <section
          aria-label="会話"
          className="chat-conversation"
          aria-busy={busy}
        >
          {thread ? (
            <>
              <header className="chat-header">
                <h3># {thread.value.name}</h3>
                <button
                  data-variant="secondary"
                  disabled={busy}
                  aria-busy={busy}
                  onClick={() => void act(() => refresh(thread))}
                >
                  会話を再取得
                </button>
              </header>
              <div className="chat-scroller" ref={scroller} onScroll={track}>
                {timeline?.items.length === 50 && (
                  <button
                    data-variant="secondary"
                    disabled={busy}
                    onClick={() => void act(() => refresh(thread, timeline))}
                  >
                    以前の会話の次のページ
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
                          data-focused={focused?.id === row.id || undefined}
                        >
                          <p className="chat-source">
                            <strong>{author(message.value)}</strong>
                            {message.value.role === "human" &&
                              message.value.user !== user &&
                              " · 人間"}
                            {" · "}
                            {message.value.state === "complete"
                              ? "保存済み"
                              : "応答は未完了"}
                          </p>
                          <p className="chat-text">{message.value.text}</p>
                          <div className="chat-message-actions">
                            {message.value.state === "complete" && (
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
                            )}
                            <button
                              data-variant="secondary"
                              aria-pressed={focused?.id === row.id}
                              onClick={() =>
                                setFocused(
                                  focused?.id === row.id ? undefined : message,
                                )
                              }
                            >
                              詳細を表示
                            </button>
                          </div>
                        </li>
                      );
                    }
                    if (row.kind === "chat-adoption") {
                      const value = row.value as ChatAdoption;
                      return (
                        <li key={row.id} className="chat-event">
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
                          {work({ adoption: value })}
                        </li>
                      );
                    }
                    if (row.kind === "chat-link")
                      return (
                        <li key={row.id} className="chat-event">
                          {work({ link: row.value as ChatLink })}
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
                      {operationStateLabels[op.value.state] ?? op.value.state}
                      {op.value.reason && ` · ${op.value.reason}`}
                    </p>
                    {op.value.state === "sending" &&
                      op.value.client !== operationClientID && (
                        <p>別クライアントで送信中</p>
                      )}
                    <Disclosure title="操作と原応答を確認">
                      {() => (
                        <>
                          <p>
                            {op.id} / {op.version}
                          </p>
                          <button
                            data-variant="secondary"
                            disabled={busy}
                            aria-busy={busy}
                            onClick={() =>
                              void act(async () => {
                                if (!op.value.turn) return;
                                const turn = await read(op.value.turn);
                                setTurns((current) => ({
                                  ...current,
                                  [op.id]: JSON.stringify(turn.value, null, 2),
                                }));
                              })
                            }
                          >
                            保存した原応答を取得
                          </button>
                          {turns[op.id] && <pre>{turns[op.id]}</pre>}
                        </>
                      )}
                    </Disclosure>
                  </section>
                ))}
              </div>
              {!atBottom && (
                <button
                  type="button"
                  className="chat-jump"
                  data-variant="secondary"
                  onClick={() => {
                    anchor.current = "bottom";
                    const element = scroller.current;
                    if (element) element.scrollTop = element.scrollHeight;
                    setAtBottom(true);
                  }}
                >
                  最新のメッセージへ
                </button>
              )}
              {canEdit && (
                <form
                  className="chat-composer"
                  onSubmit={(e) => {
                    e.preventDefault();
                    void act(() => send(true));
                  }}
                >
                  <label>
                    相談・メッセージ
                    <textarea
                      rows={2}
                      value={text}
                      onChange={(e) => setText(e.target.value)}
                    />
                  </label>
                  <Disclosure
                    title={`${profile?.name || "モデル未選択"} · 履歴 ${chosen.length}件 · ${context ? "Context参照" : "Context不参照"}`}
                  >
                    {() => (
                      <>
                        <label>
                          <input
                            type="checkbox"
                            checked={context}
                            onChange={(e) => setContext(e.target.checked)}
                          />
                          Contextを参照
                        </label>
                        <ModelConnectionSettings
                          native={native}
                          actor={api + "\0" + user}
                          workspace={workspace}
                          selected={profile}
                          onSelected={setProfile}
                          selectionOnly
                        />
                      </>
                    )}
                  </Disclosure>
                  <div className="chat-composer-actions">
                    <button
                      type="button"
                      data-variant="secondary"
                      disabled={busy || !text.trim()}
                      onClick={() => void act(() => send(false))}
                    >
                      メッセージだけを保存
                    </button>
                    <button disabled={busy || !text.trim()}>
                      保存してAIへ相談
                    </button>
                  </div>
                </form>
              )}
              {work({})}
            </>
          ) : (
            <p className="chat-empty">
              スレッドを開くか、新しい相談を始めてください。
            </p>
          )}
          <p role="status" className="chat-notice" tabIndex={-1}>
            {busy ? "取得中…" : notice}
          </p>
        </section>
      </div>
      {focused && (
        <DetailPane inline={embedded}>
          <section
            className="chat-message-details"
            aria-label="メッセージの詳細"
          >
            <h4>{author(focused.value)}のメッセージ</h4>
            <p className="chat-source">
              固定版：{focused.id} / {focused.version}
            </p>
            {focused.value.operation && (
              <button
                data-variant="secondary"
                disabled={busy}
                aria-busy={busy}
                onClick={() =>
                  void act(async () => {
                    const record = await read<ClientOperation>({
                      id: focused.value.operation!.id,
                      version: "head",
                    });
                    setOperations((current) => [
                      record,
                      ...current.filter((r) => r.id !== record.id),
                    ]);
                  })
                }
              >
                モデル操作の現在の記録を確認
              </button>
            )}
            {canEdit && focused.value.state === "complete" && (
              <>
                <h5>仕様として採用する範囲</h5>
                <label>
                  採用する本文を選択
                  <textarea
                    readOnly
                    rows={6}
                    value={focused.value.text}
                    onSelect={(e) => {
                      const t = e.currentTarget;
                      setExcerpt({
                        message: focused,
                        start: t.selectionStart,
                        end: t.selectionEnd,
                      });
                    }}
                  />
                </label>
                <button
                  data-variant="secondary"
                  onClick={() =>
                    setExcerpt({
                      message: focused,
                      start: 0,
                      end: focused.value.text.length,
                    })
                  }
                >
                  このメッセージ全体を採用範囲にする
                </button>
                {excerpt &&
                  excerpt.message.id === focused.id &&
                  excerpt.end > excerpt.start && (
                    <div>
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
                      <button disabled={busy} onClick={() => void act(adopt)}>
                        この範囲をContextに保存
                      </button>
                    </div>
                  )}
              </>
            )}
            <button
              data-variant="secondary"
              onClick={() => setFocused(undefined)}
            >
              詳細を閉じる
            </button>
          </section>
        </DetailPane>
      )}
    </ScreenContent>
  );
}
