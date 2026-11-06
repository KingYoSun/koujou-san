import { useEffect, useRef, useState } from "react";
import { connectMCP, syncRequest, workspaceAccessLost } from "./auth";
import { SyncHTTPError } from "./sync-error";
import { allowAnswerScope, answerScopeEpoch, syncStorage } from "./sync-store";
import {
  NavigationLink,
  ScreenContent,
  useNavigation,
  useScreenActive,
} from "./navigation";
import { DetailPane, SidebarPane } from "./panes";
import { Disclosure } from "./disclosure";
import { ModelConnectionSettings } from "./model-connection-settings";
import {
  modelReady,
  modelSnapshot,
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
import { useChatConnections } from "./chat-connections";
import { ChatSidebar } from "./chat-sidebar";
import { ChatHistory, chatHistoryLimit } from "./chat-history";
import { ChatOperationControls } from "./chat-operation-controls";
import { ChatMarkdown } from "./chat-markdown";
import {
  chatPreferenceKey,
  chatPreferences,
  rememberChat,
} from "./chat-preferences";

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
  onThreadOpened?: (ref?: VersionRef) => void;
}) {
  const namespace = { api, user, workspace },
    base = chatBase(workspace);
  const preferenceKey = chatPreferenceKey(api, user, workspace);
  const navigation = useNavigation(),
    visible = useScreenActive();
  const [project, setProject] = useState<VersionRef>();
  const [projectName, setProjectName] = useState("");
  const [threads, setThreads] = useState<ChatPage>();
  const [thread, setThread] = useState<Saved<ChatThread>>();
  const [timeline, setTimeline] = useState<ChatPage>();
  const [operations, setOperations] = useState<Saved<ClientOperation>[]>([]);
  const [turns, setTurns] = useState<Record<string, string>>({});
  const [chosen, setChosen] = useState<VersionRef[]>([]);
  const [recentMessages, setRecentMessages] = useState<Saved<ChatMessage>[]>(
    [],
  );
  const [text, setText] = useState("");
  const search = useRef("");
  const [activity, setActivity] = useState("");
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
  const stopped = useRef(new Set<string>());
  const canEdit = role !== "viewer";
  const { profile, selectModel, mcp, checkMCP } = useChatConnections(
    { native, api, user, workspace },
    visible,
    setNotice,
  );
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
        setProject(undefined);
        setProjectName("");
        setTimeline(undefined);
        setThreads(undefined);
        setOperations([]);
        setChosen([]);
        setRecentMessages([]);
        setExcerpt(undefined);
        setFocused(undefined);
        localStorage.removeItem(preferenceKey);
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
  async function listThreads(
    after?: ChatPage,
    selected = project,
    mine = serial.current,
  ) {
    if (!selected) return;
    const page = await call<ChatPage>("/query", {
      kind: "chat-thread",
      root: selected.id,
      search: search.current,
      limit: 50,
      descending: true,
      after: after?.after ?? 0,
      after_id: after?.after_id ?? "",
    });
    if (mine === serial.current) setThreads(page);
  }
  async function selectProject(ref: VersionRef, current?: number) {
    const mine = current ?? ++serial.current;
    if (current === undefined) {
      setThread(undefined);
      setTimeline(undefined);
      setOperations([]);
      setChosen([]);
      setRecentMessages([]);
      setFocused(undefined);
      setExcerpt(undefined);
    }
    const scoped = await call<{
      data: {
        ref: { id: string; revision: string };
        value: { name: string; kind: string };
      };
    }>(
      `/contexts/workspaces/${encodeURIComponent(workspace)}/scope/${encodeURIComponent(ref.id.replace(/^scope:/, ""))}?revision=${encodeURIComponent(ref.version)}`,
    );
    if (scoped.data.value.kind !== "project") throw Error("project_required");
    if (mine !== serial.current) return;
    const project = {
      id: "scope:" + scoped.data.ref.id,
      version: scoped.data.ref.revision,
    };
    setProject(project);
    setProjectName(scoped.data.value.name);
    rememberChat(preferenceKey, {
      project,
      ...(current === undefined ? { thread: undefined } : {}),
    });
    await listThreads(undefined, project, mine);
  }
  async function refresh(
    ref: VersionRef,
    after?: ChatPage,
    keepHistory = false,
  ) {
    const mine = ++serial.current;
    const saved = await read<ChatThread>(ref);
    if (currentThread.current?.id !== saved.id)
      await selectProject(saved.value.project, mine);
    const page = await call<ChatPage>("/query", {
      root: saved.id,
      limit: 50,
      descending: true,
      after: after?.after ?? 0,
      after_id: after?.after_id ?? "",
    });
    const recent =
      !after && !keepHistory
        ? await call<ChatPage>("/query", {
            root: saved.id,
            kind: "chat-message",
            descending: true,
            limit: chatHistoryLimit,
          })
        : undefined;
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
    if (recent) {
      const messages = recent.items
        .filter(
          (r) =>
            r.kind === "chat-message" &&
            r.root === saved.id &&
            (r.value as ChatMessage).state === "complete",
        )
        .reverse() as Saved<ChatMessage>[];
      setRecentMessages(messages);
      setChosen(messages.map(pin));
    }
    rememberChat(preferenceKey, {
      project: saved.value.project,
      thread: pin(saved),
    });
  }
  function open(ref: VersionRef) {
    setChosen([]);
    setRecentMessages([]);
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
    else if (visible && target?.kind === "scope")
      void act(() => selectProject(target));
    else if (visible && !project) void act(restoreProject);
  }, [
    visible,
    navigation?.route.target?.id,
    navigation?.route.target?.version,
  ]);
  useEffect(() => {
    if (embedded && initialThread) void act(() => refresh(initialThread));
    else if (embedded) void act(restoreProject);
  }, []);
  async function restoreProject() {
    const saved = chatPreferences(preferenceKey);
    if (saved.thread) return refresh(saved.thread);
    if (saved.project)
      return selectProject({ ...saved.project, version: "head" });
    const page = await call<{
      data: { items: { ref: { id: string; revision: string } }[] };
    }>(`/contexts/workspaces/${encodeURIComponent(workspace)}/catalog`, {
      kind: "scope",
      scope_kind: "project",
    });
    const first = page.data.items[0];
    if (first)
      await selectProject({
        id: "scope:" + first.ref.id,
        version: first.ref.revision,
      });
  }
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
    if (!project) throw Error("project_required");
    const name = [...text.trim().split(/\r?\n/)[0]].slice(0, 40).join("");
    const key = JSON.stringify([project, text]);
    if (!ids.current.has("thread:" + key))
      ids.current.set("thread:" + key, `chat-thread:${crypto.randomUUID()}`);
    const id = ids.current.get("thread:" + key)!;
    const ref = await mutate(`/chat/threads/${encodeURIComponent(id)}`, {
      value: { name, project: pin(project) },
    });
    const saved = await read<ChatThread>(ref);
    currentThread.current = saved;
    setThread(saved);
    rememberChat(preferenceKey, { project, thread: pin(saved) });
    onThreadOpened?.(pin(saved));
    return saved;
  }
  function newChat() {
    ++serial.current;
    currentThread.current = undefined;
    setThread(undefined);
    setTimeline(undefined);
    setOperations([]);
    setChosen([]);
    setRecentMessages([]);
    setFocused(undefined);
    setExcerpt(undefined);
    setText("");
    setNotice("");
    rememberChat(preferenceKey, { thread: undefined });
    onThreadOpened?.(undefined);
    if (navigation && !embedded && project)
      navigation.navigate({
        workspace,
        screen: "chat",
        target: { ...project, kind: "scope" },
        view: "new",
      });
  }
  async function send(ai: boolean) {
    if (!project || !text.trim()) return;
    if (ai && (!profile || !modelReady(native, api + "\0" + user, profile))) {
      setNotice("利用するモデル接続を設定・選択してください。");
      return;
    }
    if (ai && context && !(await checkMCP())) {
      setNotice(
        "Projectの参照接続を確認してください。未接続の場合はMCPへ接続してください。",
      );
      return;
    }
    const saved = thread ?? (await create());
    const target = pin(saved),
      projectRef = saved.value.project;
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
          inspect: context,
        },
        mode: "proposal",
        fixed: { mode: context ? "select" : "none" },
        scopes: context ? [projectRef.id.replace(/^scope:/, "")] : [],
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
      await refresh(target, undefined, true);
      await run(operation, policy);
    }
    if (currentThread.current?.id === target.id) {
      setText("");
      setChosen([]);
      setRecentMessages([]);
      setAtBottom(true);
      await refresh(target);
      await listThreads();
      if (navigation && !embedded)
        navigation.navigate(
          {
            workspace,
            screen: "chat",
            target: { ...target, kind: "chat-thread", name: saved.value.name },
          },
          { replace: true, focus: false },
        );
    }
    ids.current.delete("message:" + key);
    ids.current.delete("thread:" + JSON.stringify([project, text]));
  }
  function updateOperation(update: Saved<ClientOperation>) {
    setOperations((current) => [
      update,
      ...current.filter((r) => r.id !== update.id),
    ]);
  }
  async function run(
    operation: Saved<ClientOperation>,
    policy: SelectionPolicy,
  ) {
    if (!profile) throw Error("model_required");
    updateOperation(operation);
    stopped.current.delete(operation.id);
    try {
      await runSelection(
        operation,
        policy,
        profile,
        operationClientID,
        operationPort(
          { native, api, user, workspace },
          profile,
          operation,
          () => lifetime.current && !stopped.current.has(operation.id),
        ),
        updateOperation,
        setActivity,
      );
    } finally {
      setActivity("");
    }
  }
  async function recover(operation: Saved<ClientOperation>, action: string) {
    if (operation.value.user !== user) return;
    if (action === "cancel") stopped.current.add(operation.id);
    let current = operation;
    if (action !== "continue") {
      const ref = await mutate(
        `/operations/${encodeURIComponent(operation.id)}/actions`,
        {
          kind: action,
          client: operationClientID,
          ...(action === "takeover" && profile
            ? { model: modelSnapshot(profile, workspace) }
            : {}),
        },
        operation.version,
      );
      current = await read<ClientOperation>(ref);
      updateOperation(current);
    }
    if (action === "resume" || action === "continue") {
      const policy = await read<SelectionPolicy>(current.value.policy);
      await run(current, policy.value);
    }
    if (thread) await refresh(thread);
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
  async function loadTurn(ref: VersionRef) {
    const op = await read<ClientOperation>({ id: ref.id, version: "head" });
    if (!op.value.turn) return;
    const turn = await read(op.value.turn);
    setTurns((current) => ({
      ...current,
      [op.id]: JSON.stringify(turn.value, null, 2),
    }));
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
        <SidebarPane inline={embedded}>
          <ChatSidebar
            native={native}
            workspace={workspace}
            role={role}
            project={project}
            projectName={projectName}
            thread={thread}
            threads={threads}
            busy={busy}
            embedded={embedded}
            onProject={(ref) => void act(() => selectProject(ref))}
            onNew={newChat}
            onOpen={open}
            onList={(query, after) => {
              search.current = query;
              void act(() => listThreads(after));
            }}
          />
        </SidebarPane>
        <section
          aria-label="会話"
          className="chat-conversation"
          aria-busy={busy}
        >
          <header className="chat-header">
            <div>
              <p className="chat-source">{projectName || "Project未選択"}</p>
              <h3>{thread?.value.name || "新しい相談"}</h3>
            </div>
            {thread && (
              <button
                data-variant="secondary"
                disabled={busy}
                onClick={() => void act(() => refresh(thread))}
              >
                会話を更新
              </button>
            )}
          </header>
          <div className="chat-scroller" ref={scroller} onScroll={track}>
            {!thread && (
              <div className="chat-empty">
                <h3>
                  {canEdit
                    ? "何から始めましょうか"
                    : "スレッドを開くか、Projectを選択してください。"}
                </h3>
                {canEdit && (
                  <button
                    data-variant="secondary"
                    disabled={busy || !project}
                    onClick={() =>
                      setText(
                        "このProjectで開発を開始するために必要な設定が揃っているか、登録リポジトリのREADME・開発手順と現在の実行状態を確認してください。",
                      )
                    }
                  >
                    開発の準備を確認する
                  </button>
                )}
              </div>
            )}
            {timeline?.items.length === 50 && (
              <button
                data-variant="secondary"
                disabled={busy}
                onClick={() => void act(() => refresh(thread!, timeline))}
              >
                以前の会話を表示
              </button>
            )}
            <ol className="chat-timeline">
              {[...(timeline?.items ?? [])].reverse().map((row) => {
                if (row.kind === "chat-message") {
                  const message = row as Saved<ChatMessage>;
                  return (
                    <li
                      key={row.id}
                      className={`chat-message chat-${message.value.role}`}
                      data-focused={focused?.id === row.id || undefined}
                    >
                      <p className="chat-source">
                        <strong>{author(message.value)}</strong>
                        {message.value.state !== "complete" &&
                          " · 応答は未完了"}
                      </p>
                      {message.value.role === "assistant" ? (
                        <ChatMarkdown text={message.value.text} />
                      ) : (
                        <p className="chat-text">{message.value.text}</p>
                      )}
                      <div className="chat-message-actions">
                        <button
                          data-variant="secondary"
                          aria-pressed={focused?.id === row.id}
                          onClick={() =>
                            setFocused(
                              focused?.id === row.id ? undefined : message,
                            )
                          }
                        >
                          詳細・仕様に採用
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
            {operations
              .filter((op) => op.value.state !== "completed")
              .map((op) => (
                <section
                  key={op.id}
                  className="chat-operation"
                  aria-label="モデル操作"
                >
                  <p role="status">
                    {activity ||
                      (op.value.state === "sending"
                        ? "返答を作成中…"
                        : (operationStateLabels[op.value.state] ??
                          op.value.state))}
                    {op.value.reason && ` · ${op.value.reason}`}
                  </p>
                  {op.value.state === "sending" &&
                    op.value.client !== operationClientID && (
                      <p>別クライアントで送信中</p>
                    )}
                  {canEdit && (
                    <ChatOperationControls
                      operation={op.value}
                      user={user}
                      client={operationClientID}
                      busy={busy}
                      modelMatches={
                        !!profile &&
                        modelReady(native, api + "\0" + user, profile) &&
                        profile.id === op.value.model.connection &&
                        profile.revision === op.value.model.revision &&
                        profile.model === op.value.model.requested
                      }
                      onAction={(action) => void act(() => recover(op, action))}
                    />
                  )}
                  <Disclosure title="操作と原応答を確認">
                    {() => (
                      <>
                        <button
                          data-variant="secondary"
                          disabled={busy}
                          onClick={() => void act(() => loadTurn(op))}
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
                if (scroller.current)
                  scroller.current.scrollTop = scroller.current.scrollHeight;
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
                  rows={3}
                  value={text}
                  onChange={(e) => setText(e.target.value)}
                  onKeyDown={(e) => {
                    if (
                      e.key === "Enter" &&
                      (e.ctrlKey || e.metaKey) &&
                      !e.nativeEvent.isComposing &&
                      !busy
                    ) {
                      e.preventDefault();
                      void act(() => send(true));
                    }
                  }}
                />
              </label>
              <div className="chat-composer-toolbar">
                <Disclosure
                  title={
                    profile
                      ? `${profile.name} · ${profile.model}`
                      : "モデル接続を選択"
                  }
                >
                  {() => (
                    <ModelConnectionSettings
                      native={native}
                      actor={api + "\0" + user}
                      workspace={workspace}
                      selected={profile}
                      onSelected={selectModel}
                      selectionOnly
                    />
                  )}
                </Disclosure>
                <Disclosure
                  title={
                    context ? `Project参照 · MCP ${mcp}` : "Project参照なし"
                  }
                >
                  {() => (
                    <>
                      <label>
                        <input
                          type="checkbox"
                          checked={context}
                          onChange={(e) => setContext(e.target.checked)}
                        />
                        Context・設定・登録ファイルを参照
                      </label>
                      {context && (
                        <button
                          type="button"
                          data-variant="secondary"
                          disabled={busy}
                          onClick={() =>
                            void act(async () => {
                              if (mcp === "未接続" || mcp === "別アカウント")
                                await connectMCP(native);
                              await checkMCP();
                            })
                          }
                        >
                          {mcp === "未接続" || mcp === "別アカウント"
                            ? "MCPへ接続"
                            : "MCP接続を確認"}
                        </button>
                      )}
                    </>
                  )}
                </Disclosure>
                <ChatHistory
                  messages={recentMessages}
                  chosen={chosen}
                  onChange={setChosen}
                  disabled={busy}
                />
                <div className="chat-composer-actions">
                  <button
                    type="button"
                    data-variant="secondary"
                    disabled={busy || !project || !text.trim()}
                    onClick={() => void act(() => send(false))}
                  >
                    保存のみ
                  </button>
                  <button
                    disabled={busy || !project || !text.trim() || !profile}
                  >
                    {busy ? "処理中…" : "送信"}
                  </button>
                </div>
              </div>
              {notice && (
                <p role="status" className="chat-notice">
                  {notice}
                </p>
              )}
            </form>
          )}
          {thread && work({})}
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
                  void act(() => loadTurn(focused.value.operation!))
                }
              >
                モデル操作の現在の記録を確認
              </button>
            )}
            {focused.value.operation && turns[focused.value.operation.id] && (
              <pre>{turns[focused.value.operation.id]}</pre>
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
