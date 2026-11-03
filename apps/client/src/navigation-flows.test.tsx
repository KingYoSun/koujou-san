// @vitest-environment happy-dom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { renderToStaticMarkup } from "react-dom/server";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { syncRequest } from "./auth";
import { RepositorySettings } from "./repository-settings";
import { TriageIssue } from "./triage-panels";
import { GraphRelationEditor } from "./graph-relation-editor";
import { ProjectChat } from "./project-chat";
import { ResourceSharing } from "./resource-sharing";
import { DesktopConnection } from "./desktop-connection";
import { invoke } from "@tauri-apps/api/core";
import { NavigationProvider } from "./navigation";
import {
  runSelection,
  type ClientOperation,
  type Saved,
} from "./client-selection-host";
import type { ModelProfile } from "./model-connections";
import type { UI } from "./context-ui";

vi.mock("./auth", async (original) => ({
  ...(await original<typeof import("./auth")>()),
  syncRequest: vi.fn(),
}));
vi.mock("@tauri-apps/api/core", () => ({ invoke: vi.fn() }));
vi.mock("./client-selection-host", async (original) => ({
  ...(await original<typeof import("./client-selection-host")>()),
  runSelection: vi.fn(),
}));
vi.mock("./model-connection-settings", () => ({
  ModelConnectionSettings: ({
    onSelected,
  }: {
    onSelected: (value: ModelProfile) => void;
  }) => (
    <button
      type="button"
      onClick={() =>
        onSelected({
          id: "model",
          name: "モデル",
          revision: "fixed-model",
          model: "fixture",
          allowed_models: ["fixture"],
          auth: "none",
          endpoint: "https://model.fixture.test",
          contract: "fixture",
          prices: {},
          valuation: "reported",
        })
      }
    >
      接続を選択
    </button>
  ),
}));
let root: Root, host: HTMLDivElement;
beforeEach(() => {
  vi.clearAllMocks();
  localStorage.clear();
  Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});
afterEach(async () => {
  await act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});
const button = (label: string) => {
  const element = [...host.querySelectorAll("button")].find(
    (item) => item.textContent === label,
  );
  if (!element) throw Error(`missing button: ${label}`);
  return element;
};
const input = (label: string) => {
  const node = [...host.querySelectorAll("label")].find((item) =>
    item.textContent?.startsWith(label),
  );
  const element =
    node?.querySelector<
      HTMLInputElement | HTMLTextAreaElement | HTMLSelectElement
    >("input,textarea,select") ??
    (node?.htmlFor
      ? host.querySelector<HTMLInputElement>(`#${node.htmlFor}`)
      : undefined);
  if (!element) throw Error(`missing input: ${label}`);
  return element;
};
async function click(label: string) {
  await act(() => button(label).click());
}
async function change(label: string, value: string) {
  await act(() => {
    const element = input(label);
    const prototype =
      element instanceof HTMLSelectElement
        ? HTMLSelectElement.prototype
        : element instanceof HTMLTextAreaElement
          ? HTMLTextAreaElement.prototype
          : HTMLInputElement.prototype;
    Object.getOwnPropertyDescriptor(prototype, "value")!.set!.call(
      element,
      value,
    );
    element.dispatchEvent(
      new Event(element instanceof HTMLSelectElement ? "change" : "input", {
        bubbles: true,
      }),
    );
  });
}
async function submit(label: string) {
  await act(() =>
    button(label)
      .closest("form")!
      .dispatchEvent(new Event("submit", { bubbles: true, cancelable: true })),
  );
}

it("keeps record deletion absent until a refused repository is selected", () => {
  const html = renderToStaticMarkup(
    <RepositorySettings native={false} workspace="one" role="admin" />,
  );
  expect(html).toContain('aria-label="リポジトリ設定"');
  expect(html).not.toContain("作成拒否の記録を削除");
});

it("configures a shared Gitea connection and creates/registers a repository from the actual forms", async () => {
  let connections: unknown[] = [],
    repository: Record<string, unknown> | undefined;
  vi.mocked(syncRequest).mockImplementation(async (path, _native, options) => {
    const body = options.body as Record<string, unknown>;
    if (path.endsWith("/catalog"))
      return {
        data: {
          items: [
            {
              ref: { kind: "scope", id: "project", revision: "fixed-project" },
              name: "Project A",
              scope_kind: "project",
            },
          ],
        },
        meta: {},
      };
    if (options.method === "POST" && path.includes("/connections/")) {
      connections = [
        {
          id: path.split("/").at(-1),
          name: body.name,
          instance: body.instance,
          allow_create: true,
          version: "c1",
        },
      ];
      return { data: { version: "c1" } };
    }
    if (path.includes("/owners"))
      return [
        { login: "owner", type: "user" },
        { login: "team", type: "organization" },
      ];
    if (path.includes("/connections?")) return { items: connections };
    if (options.method === "POST" && path.endsWith("/create")) {
      repository = {
        ...body,
        id: path.split("/").at(-2),
        instance: "https://forge.fixture.test",
        state: "created",
        repository_id: 42,
        version: "r1",
      };
      return { data: { version: "r1" } };
    }
    if (path.includes("/observations?")) return { items: [] };
    if (path.includes("/repositories?"))
      return { items: repository ? [repository] : [] };
    if (path.includes("/repositories/")) return repository;
    throw Error(path);
  });
  await act(() =>
    root.render(
      <RepositorySettings native={false} workspace="one" role="admin" />,
    ),
  );
  expect(
    vi
      .mocked(syncRequest)
      .mock.calls.filter(([, , options]) => options.method === "POST"),
  ).toHaveLength(0);
  await click("共有接続");
  await click("トークンでGitea接続を追加");
  await change("接続名", "Shared Gitea");
  await change("Gitea URL", "https://forge.fixture.test");
  await change("アクセストークン", "synthetic-token");
  await act(() => input("このWorkspaceでリポジトリ作成を許可").click());
  await submit("共有接続を保存");
  expect(input("アクセストークンを置換").value).toBe("");
  await click("リポジトリ");
  await click("Giteaリポジトリを作成");
  await change("共有接続", String((connections[0] as { id: string }).id));
  await click("所有者を取得");
  await change("所有者", "team");
  await change("リポジトリ名", "new-project");
  await change("公開範囲", "private");
  await change("Project", "project");
  // Project has a separate name filter; select the actual Project choice.
  await act(() => {
    const select = host.querySelector<HTMLSelectElement>(
      'select option[value="project"]',
    )?.parentElement as HTMLSelectElement;
    select.value = "project";
    select.dispatchEvent(new Event("change", { bubbles: true }));
  });
  await submit("リポジトリを作成");
  const sent = vi
    .mocked(syncRequest)
    .mock.calls.filter(
      ([path, , options]) =>
        path.endsWith("/create") && options.method === "POST",
    );
  expect(sent).toHaveLength(1);
  expect(sent[0][2].body).toMatchObject({
    full_name: "team/new-project",
    private: true,
    project: "project",
    expected_version: "0",
  });
  expect(host.textContent).toContain("作成済み");
  expect(input("リポジトリ（所有者/名前）").value).toBe("team/new-project");
  expect(localStorage.length).toBe(0);
});

it("lets a server administrator configure OAuth apps without Workspace management rights", async () => {
  vi.mocked(syncRequest).mockResolvedValue({ items: [] });
  await act(() =>
    root.render(
      <RepositorySettings
        native={false}
        workspace="one"
        role="viewer"
        serverAdmin
      />,
    ),
  );
  await click("共有接続");
  expect(button("OAuthアプリを追加")).toBeTruthy();
  expect(host.textContent).not.toContain("Giteaにログイン");
  expect(host.textContent).not.toContain("トークンでGitea接続を追加");
  expect(
    vi
      .mocked(syncRequest)
      .mock.calls.some(([path]) => path.startsWith("/forge/oauth/apps")),
  ).toBe(true);
});

it("opens new Issue information without a query and saves with the new-record version", async () => {
  const call = vi.fn(),
    write = vi.fn(async () => ({
      data: { id: "issue:issue", version: "saved" },
    }));
  await act(() =>
    root.render(
      <TriageIssue
        scope={{ id: "scope:issue", version: "fixed-scope" }}
        canEdit
        admin={false}
        call={call}
        write={write}
        run={async (action) => action()}
      />,
    ),
  );
  await click("Issue情報を新規作成");
  expect(call).not.toHaveBeenCalled();
  expect(write).not.toHaveBeenCalled();
  await change("Issue名", "新しいIssue");
  await click("Issue情報を保存して再評価を要求");
  expect(write).toHaveBeenCalledWith(
    "issue:issue",
    "0",
    expect.objectContaining({
      title: "新しいIssue",
      scope: { id: "scope:issue", version: "fixed-scope" },
    }),
  );
  expect(host.textContent).toContain("保存版：saved");
});

it("starts a consultation from the fixed Project reached through Scope editing without requiring an Issue or Run", async () => {
  history.replaceState(
    null,
    "",
    "?workspace=one&screen=chat&kind=scope&id=project&version=project-fixed",
  );
  let threadID = "";
  vi.mocked(syncRequest).mockImplementation(async (path) => {
    if (path.endsWith("/catalog")) return { data: { items: [] }, meta: {} };
    if (path.includes("/contexts/") && path.includes("/scope/"))
      return {
        data: {
          ref: { id: "project", revision: "project-fixed" },
          value: { kind: "project", name: "Project A" },
        },
      };
    if (path.endsWith("/query")) return { items: [], after: 0, after_id: "" };
    if (path.includes("/chat/threads/")) {
      threadID = decodeURIComponent(path.split("/").at(-1)!);
      return { data: { id: threadID, version: "thread-fixed" } };
    }
    if (path.includes("/records/"))
      return {
        id: threadID,
        version: "thread-fixed",
        kind: "chat-thread",
        value: {
          name: "相談",
          project: { id: "scope:project", version: "project-fixed" },
        },
      };
    throw Error(path);
  });
  await act(() =>
    root.render(
      <NavigationProvider
        api="https://api.fixture.test"
        user="alice"
        workspace="one"
      >
        <ProjectChat
          native={false}
          api="https://api.fixture.test"
          user="alice"
          workspace="one"
          role="editor"
          canExecute={false}
        />
      </NavigationProvider>,
    ),
  );
  expect(host.textContent).toContain("Project Aのスレッド");
  await change("新しい相談の題名", "相談");
  await submit("スレッドを作成");
  const created = vi
    .mocked(syncRequest)
    .mock.calls.filter(([path]) => path.includes("/chat/threads/"));
  expect(created).toHaveLength(1);
  expect(created[0][2].body).toMatchObject({
    expected_version: "0",
    value: {
      name: "相談",
      project: { id: "scope:project", version: "project-fixed" },
    },
  });
  expect(host.textContent).toContain("保存してAIへ相談");
  expect(runSelection).not.toHaveBeenCalled();
});

it("loads a resource's sharing policy and saves scope/grants with each current policy version", async () => {
  let version = "0",
    restricted = false;
  vi.mocked(syncRequest).mockImplementation(async (path, _native, options) => {
    if (path.includes("/members"))
      return { items: [{ id: "bob", name: "Bob", active: true }] };
    if (options.method === "POST") {
      const body = options.body as {
        restricted?: boolean;
        expected_version: string;
      };
      expect(body.expected_version).toBe(version);
      if (body.restricted !== undefined) restricted = body.restricted;
      version = version === "0" ? "p1" : "p2";
      return { data: { version } };
    }
    return { id: "scope:project", version, restricted, grants: [] };
  });
  await act(() =>
    root.render(
      <ResourceSharing native={false} workspace="one" role="admin" />,
    ),
  );
  await change("資源ID", "scope:project");
  await submit("共有設定を取得");
  await act(() => input("閲覧者を制限").click());
  await click("共有範囲を保存");
  await click("参加者を取得");
  await change("参加者", "bob");
  await act(() => input("編集").click());
  await click("参加者の権限を保存");
  const changes = vi
    .mocked(syncRequest)
    .mock.calls.filter(([, , options]) => options.method === "POST");
  expect(changes).toHaveLength(2);
  expect(changes[1][2].body).toMatchObject({
    expected_version: "p1",
    grant: { user: "bob", read: true, update: true, execute: false },
  });
  expect(
    changes.every(([path]) => path.endsWith("/resources/scope%3Aproject")),
  ).toBe(true);
});

it("shows and changes the saved desktop API URL using an explicit restart action", async () => {
  vi.mocked(invoke).mockImplementation(async (command) =>
    command === "connection_status"
      ? { api: "https://old.fixture.test/", managed: false }
      : { api: "https://new.fixture.test/", managed: false },
  );
  await act(() => root.render(<DesktopConnection settings />));
  expect(input("共有サーバーURL").value).toBe("https://old.fixture.test/");
  await change("共有サーバーURL", "https://new.fixture.test/");
  await submit("接続先を保存して再起動");
  expect(invoke).toHaveBeenLastCalledWith("connection_configure", {
    api: "https://new.fixture.test/",
  });
});

it("saves explicit graph relations with fixed references and reuses the chosen source revision when editing", async () => {
  vi.mocked(syncRequest).mockResolvedValue({
    data: { version: "saved-relation" },
  });
  const ui: Pick<UI, "workspace" | "native" | "canCreate" | "fail"> = {
    workspace: "one",
    native: false,
    canCreate: true,
    fail: (error: unknown) => {
      throw error;
    },
  };
  const from = { kind: "sentence", id: "from", revision: "from-fixed" },
    to = { kind: "document", id: "to", revision: "to-fixed" };
  await act(() =>
    root.render(
      <GraphRelationEditor
        ui={ui}
        selected={from}
        reference={to}
        onSaved={() => {}}
      />,
    ),
  );
  await click("関係を新規作成");
  await change("関係", "semantic");
  await click("関係を保存");
  expect(syncRequest).toHaveBeenLastCalledWith(
    expect.stringContaining("/relations/"),
    false,
    {
      method: "PUT",
      body: expect.objectContaining({
        expected_version: "0",
        value: { kind: "semantic", from, to, order: 0, present: true },
      }),
    },
  );
  await act(() =>
    root.render(
      <GraphRelationEditor
        ui={ui}
        onSaved={() => {}}
        editing={{
          id: "edge",
          kind: "reference",
          source: {
            kind: "relation",
            id: "relation",
            revision: "chosen-fixed",
          },
          from,
          to,
          order: 2,
        }}
      />,
    ),
  );
  await click("関係を保存");
  expect(syncRequest).toHaveBeenLastCalledWith(
    expect.stringContaining("/relations/relation"),
    false,
    {
      method: "PUT",
      body: expect.objectContaining({
        expected_version: "chosen-fixed",
        value: { kind: "reference", from, to, order: 2, present: true },
      }),
    },
  );
});

it("takes over and resumes the same chat operation without creating another request or consuming a model on reload", async () => {
  const thread = { id: "chat-thread:thread", version: "thread-fixed" };
  let operation: Saved<ClientOperation> = {
    id: "client-operation:op",
    version: "op-fixed",
    kind: "operation",
    root: thread.id,
    position: 1,
    refs: [],
    value: {
      policy: { id: "policy", version: "policy-fixed" },
      target: thread,
      goal: "相談",
      purpose: "chat",
      model: {
        connection: "model",
        revision: "fixed-model",
        requested: "fixture",
        allowed: ["fixture"],
      },
      state: "failed",
      client: "other",
      user: "alice",
    },
  };
  const policy = {
    mode: "proposal" as const,
    fixed: { mode: "none" as const },
    scopes: [],
    limits: { model_requests: 32, mcp_requests: 256, seconds: 600 },
    chat: { thread, messages: [] },
  };
  vi.mocked(syncRequest).mockImplementation(async (path, _native, options) => {
    const body = options.body as Record<string, unknown>;
    if (path.includes("/catalog")) return { data: { items: [] }, meta: {} };
    if (path.includes("/contexts/") && path.includes("/scope/"))
      return {
        data: {
          ref: { id: "project", revision: "fixed-project" },
          value: { kind: "project", name: "Project" },
        },
      };
    if (path.endsWith("/query"))
      return {
        items:
          body.kind === "chat-thread"
            ? []
            : [
                {
                  id: "link",
                  kind: "chat-operation",
                  value: {
                    operation: { id: operation.id, version: operation.version },
                  },
                },
              ],
        after: 0,
        after_id: "",
      };
    if (path.includes("/records/chat-thread"))
      return {
        ...thread,
        kind: "chat-thread",
        value: {
          name: "相談",
          project: { id: "scope:project", version: "fixed-project" },
        },
      };
    if (path.includes("/records/client-operation")) return operation;
    if (path.includes("/records/policy"))
      return { id: "policy", version: "policy-fixed", value: policy };
    if (path.endsWith("/actions")) {
      const kind = body.kind;
      operation = {
        ...operation,
        version: operation.version + "-next",
        value: {
          ...operation.value,
          client: String(body.client),
          state:
            kind === "resume"
              ? "ready"
              : kind === "cancel"
                ? "cancelled"
                : operation.value.state,
        },
      };
      return { data: { id: operation.id, version: operation.version } };
    }
    throw Error(path);
  });
  vi.mocked(runSelection).mockImplementation(
    async (current, _policy, _profile, _client, _port, onChange) => {
      operation = { ...current, value: { ...current.value, state: "applied" } };
      onChange(operation);
      return operation;
    },
  );
  await act(() =>
    root.render(
      <ProjectChat
        native={false}
        api="https://api.fixture.test"
        user="alice"
        workspace="one"
        role="editor"
        canExecute={false}
        embedded
        initialThread={thread}
      />,
    ),
  );
  expect(runSelection).not.toHaveBeenCalled();
  expect(button("相談を再開").disabled).toBe(true);
  await act(() => {
    const details = [...host.querySelectorAll("details")].find((item) =>
      item.querySelector("summary")?.textContent?.includes("モデル未選択"),
    )!;
    details.open = true;
    details.dispatchEvent(new Event("toggle"));
  });
  await click("接続を選択");
  await click("このクライアントへ引き継ぐ");
  await click("相談を再開");
  expect(runSelection).toHaveBeenCalledTimes(1);
  const actions = vi
    .mocked(syncRequest)
    .mock.calls.filter(([path]) => path.endsWith("/actions"));
  expect(
    actions.map(([, , options]) => (options.body as { kind: string }).kind),
  ).toEqual(["takeover", "resume"]);
  expect(
    actions.every(([path]) => path.includes("client-operation%3Aop")),
  ).toBe(true);
  expect(
    vi
      .mocked(syncRequest)
      .mock.calls.some(
        ([path, , options]) =>
          options.method === "POST" && path.includes("/chat/messages/"),
      ),
  ).toBe(false);
});
