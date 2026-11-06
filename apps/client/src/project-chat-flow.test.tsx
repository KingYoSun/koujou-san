// @vitest-environment happy-dom
import { act } from "react";
import { createRoot } from "react-dom/client";
import { afterEach, expect, it, vi } from "vitest";
import { ProjectChat } from "./project-chat";
import { syncRequest, mcpIdentity } from "./auth";
import { loadProfiles, type ModelProfile } from "./model-connections";
import {
  runSelection,
  type Saved,
  type ClientOperation,
} from "./client-selection-host";
import { chatPreferenceKey, chatPreferences } from "./chat-preferences";

vi.mock("./auth", async (original) => ({
  ...(await original<typeof import("./auth")>()),
  syncRequest: vi.fn(),
  mcpIdentity: vi.fn(),
}));
vi.mock("./model-connections", async (original) => ({
  ...(await original<typeof import("./model-connections")>()),
  loadProfiles: vi.fn(),
}));
vi.mock("./client-selection-host", async (original) => ({
  ...(await original<typeof import("./client-selection-host")>()),
  runSelection: vi.fn(),
}));
const profile: ModelProfile = {
  id: "model",
  name: "ChatGPT",
  endpoint: "https://model.test",
  revision: "m1",
  model: "fixture",
  allowed_models: ["fixture"],
  auth: "none",
  contract: "",
  prices: {},
  valuation: "estimated",
};
const mounted: (() => Promise<void>)[] = [];
afterEach(async () => {
  for (const dispose of mounted.splice(0)) await dispose();
  localStorage.clear();
  vi.resetAllMocks();
});

async function fixture(lostThread = false) {
  Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
  vi.mocked(mcpIdentity).mockResolvedValue("alice");
  vi.mocked(loadProfiles).mockResolvedValue([profile]);
  const records = new Map<string, Saved>();
  const writes: { path: string; body: Record<string, any> }[] = [];
  const receipts = new Map<string, Saved>();
  let lost = lostThread;
  vi.mocked(syncRequest).mockImplementation(async (path, _native, options) => {
    const body = options.body as Record<string, any>;
    if (path.endsWith("/catalog"))
      return {
        data: {
          items: [{ ref: { id: "p", revision: "p1" }, name: "NAHTLOKA" }],
        },
        meta: {},
      };
    if (path.includes("/scope/"))
      return {
        data: {
          ref: { id: "p", revision: "p1" },
          value: { kind: "project", name: "NAHTLOKA" },
        },
      };
    if (path.endsWith("/query")) {
      const items = [...records.values()]
        .filter((r) =>
          body.kind === "chat-thread"
            ? r.kind === "chat-thread"
            : r.root === body.root && r.kind === "chat-message",
        )
        .filter((r) => !body.after || r.position < body.after)
        .sort((a, b) => b.position - a.position)
        .slice(0, body.limit);
      return {
        items,
        after: items.at(-1)?.position ?? 0,
        after_id: items.at(-1)?.id ?? "",
      };
    }
    if (path.includes("/records/"))
      return records.get(
        decodeURIComponent(path.split("/records/")[1].split("?")[0]),
      );
    writes.push({ path, body });
    const id = decodeURIComponent(path.split("/").at(-1)!);
    const kind = path.includes("/chat/threads/")
      ? "chat-thread"
      : path.includes("/chat/messages/")
        ? "chat-message"
        : path.includes("/policies/")
          ? "policy"
          : "operation";
    const value = body.value ?? { ...body, state: "ready", user: "alice" };
    const record = receipts.get(body.operation_id) ?? {
      id,
      version: "v1",
      kind,
      root: value.thread?.id ?? id,
      value,
      refs: [],
      position: records.size + 1,
    };
    records.set(id, record);
    receipts.set(body.operation_id, record);
    if (kind === "chat-thread" && lost) {
      lost = false;
      throw Error("lost ACK");
    }
    return { data: { id, version: record.version } };
  });
  vi.mocked(runSelection).mockImplementation(
    async (op, _policy, _profile, _client, _port, onChange) => {
      const completed: Saved<ClientOperation> = {
        ...op,
        value: { ...op.value, state: "completed" },
      };
      records.set("chat-message:answer", {
        id: "chat-message:answer",
        version: "a1",
        root: op.value.target.id,
        kind: "chat-message",
        position: records.size + 1,
        refs: [],
        value: {
          role: "assistant",
          text: "### 確認結果\n\n**READMEを確認済み**",
          state: "complete",
          operation: op,
        },
      });
      onChange(completed);
      return completed;
    },
  );
  const mount = async (workspace = "w") => {
    const host = document.createElement("div");
    document.body.append(host);
    const root = createRoot(host);
    await act(() =>
      root.render(
        <ProjectChat
          native={false}
          api="api"
          user="alice"
          workspace={workspace}
          role="editor"
          canExecute={false}
          embedded
        />,
      ),
    );
    const dispose = async () => {
      await act(() => root.unmount());
      host.remove();
    };
    mounted.push(dispose);
    const enter = async (value: string) =>
      act(() => {
        const input = host.querySelector("textarea")!;
        Object.getOwnPropertyDescriptor(
          HTMLTextAreaElement.prototype,
          "value",
        )!.set!.call(input, value);
        input.dispatchEvent(new Event("input", { bubbles: true }));
      });
    const send = async () =>
      act(async () => {
        host
          .querySelector("form.chat-composer")!
          .dispatchEvent(
            new Event("submit", { bubbles: true, cancelable: true }),
          );
      });
    return { host, enter, send };
  };
  return { records, writes, mount };
}
it("starts from a single Project and saved model with one send, then restores the consultation without another model call", async () => {
  const f = await fixture(),
    ui = await f.mount();
  expect(ui.host.textContent).toContain("ChatGPT · fixture");
  expect(ui.host.textContent).not.toContain("新しい相談の題名");
  await ui.enter("開発準備を確認してください");
  await ui.send();
  expect(
    f.writes.filter((w) => w.path.includes("/chat/threads/")),
  ).toHaveLength(1);
  expect(runSelection).toHaveBeenCalledTimes(1);
  expect(vi.mocked(runSelection).mock.calls[0][1].chat?.inspect).toBe(true);
  expect(ui.host.querySelector("h3")?.textContent).toBe(
    "開発準備を確認してください",
  );
  expect(ui.host.querySelector(".chat-markdown strong")?.textContent).toBe(
    "READMEを確認済み",
  );
  expect(
    chatPreferences(chatPreferenceKey("api", "alice", "w")).thread,
  ).toBeDefined();
  expect(
    chatPreferences(chatPreferenceKey("api", "alice", "other")).thread,
  ).toBeUndefined();
  const reopened = await f.mount();
  expect(reopened.host.textContent).toContain("READMEを確認済み");
  expect(runSelection).toHaveBeenCalledTimes(1);
  await reopened.enter("その確認結果を踏まえて次の手順を教えてください");
  await reopened.send();
  const source = vi.mocked(runSelection).mock.calls[1][1].chat!.messages;
  expect(source).toHaveLength(3);
  expect(source[1]).toEqual({ id: "chat-message:answer", version: "a1" });
});

it("automatically sends only the current consultation's bounded recent completed messages", async () => {
  const f = await fixture(),
    first = await f.mount();
  await first.enter("最初の相談");
  await first.send();
  const thread = chatPreferences(
    chatPreferenceKey("api", "alice", "w"),
  ).thread!;
  for (let i = 0; i < 70; i++)
    f.records.set(`chat-message:history-${i}`, {
      id: `chat-message:history-${i}`,
      version: "h1",
      root: thread.id,
      kind: "chat-message",
      refs: [],
      position: i + 10,
      value: {
        thread,
        role: i % 2 ? "assistant" : "human",
        text: `履歴${i}`,
        state: i === 69 ? "incomplete" : "complete",
      },
    });
  f.records.set("chat-message:foreign", {
    id: "chat-message:foreign",
    version: "f1",
    root: "chat-thread:other",
    kind: "chat-message",
    refs: [],
    position: 100,
    value: { role: "human", text: "別相談", state: "complete" },
  });
  const ui = await f.mount();
  expect(ui.host.textContent).toContain("履歴 19件");
  await act(() =>
    [...ui.host.querySelectorAll("button")]
      .find((b) => b.textContent === "以前の会話を表示")!
      .click(),
  );
  expect(ui.host.textContent).toContain("履歴 19件");
  await act(() => {
    const details = [...ui.host.querySelectorAll("details")].find((d) =>
      d.querySelector("summary")?.textContent?.startsWith("履歴 19件"),
    )!;
    details.open = true;
    details.dispatchEvent(new Event("toggle"));
  });
  await act(() =>
    ui.host
      .querySelector<HTMLInputElement>(".chat-history-selection input")!
      .click(),
  );
  await ui.enter("続けてください");
  await ui.send();
  const ids = vi
    .mocked(runSelection)
    .mock.calls.at(-1)![1]
    .chat!.messages.map((r) => r.id);
  expect(ids).toHaveLength(19);
  expect(ids.slice(0, -1)).toEqual(
    Array.from({ length: 18 }, (_, i) => `chat-message:history-${i + 51}`),
  );
  await act(() =>
    [...ui.host.querySelectorAll("button")]
      .find((b) => b.textContent?.includes("新しい相談"))!
      .click(),
  );
  await ui.enter("別の相談です");
  await ui.send();
  expect(
    vi.mocked(runSelection).mock.calls.at(-1)![1].chat!.messages,
  ).toHaveLength(1);
});
it("keeps the same fixed history and policy intent when model startup fails after the message is saved", async () => {
  const f = await fixture(),
    first = await f.mount();
  await first.enter("最初の相談");
  await first.send();
  const thread = chatPreferences(
    chatPreferenceKey("api", "alice", "w"),
  ).thread!;
  for (let i = 0; i < 21; i++)
    f.records.set(`chat-message:retry-${i}`, {
      id: `chat-message:retry-${i}`,
      version: "v1",
      root: thread.id,
      kind: "chat-message",
      refs: [],
      position: i + 10,
      value: { thread, role: "human", text: `履歴${i}`, state: "complete" },
    });
  const ui = await f.mount();
  expect(ui.host.textContent).toContain("履歴 20件");
  vi.mocked(runSelection).mockRejectedValueOnce(
    Error("operation read unavailable"),
  );
  await ui.enter("同じ入力を再確認");
  await ui.send();
  expect(ui.host.querySelector("textarea")!.value).toBe("同じ入力を再確認");
  await ui.send();
  const policies = f.writes
    .filter((w) => w.path.includes("/policies/"))
    .slice(-2);
  expect(policies).toHaveLength(2);
  expect(policies[0]).toEqual(policies[1]);
  expect(
    vi.mocked(runSelection).mock.calls.at(-1)![1].chat!.messages,
  ).toHaveLength(21);
});

it("reuses a thread creation intent after a lost acknowledgement and does not create a conversation when MCP is unauthenticated", async () => {
  const f = await fixture(true),
    ui = await f.mount();
  await ui.enter("確認してください");
  await ui.send();
  expect(runSelection).not.toHaveBeenCalled();
  await ui.send();
  const creates = f.writes.filter((w) => w.path.includes("/chat/threads/"));
  expect(creates).toHaveLength(2);
  expect(creates[0]).toEqual(creates[1]);
  expect(runSelection).toHaveBeenCalledTimes(1);
  await act(() =>
    [...ui.host.querySelectorAll("button")]
      .find((b) => b.textContent?.includes("新しい相談"))!
      .click(),
  );
  vi.mocked(mcpIdentity).mockResolvedValue(undefined);
  const before = f.writes.length;
  await ui.enter("次の相談");
  await ui.send();
  expect(f.writes).toHaveLength(before);
  expect(ui.host.textContent).toContain("MCPへ接続してください");
});
