import { afterEach, expect, it } from "vitest";
import { SyncEngine, stopSyncActor, syncKey } from "./sync-engine";
import type {
  PendingWrite,
  SavedSync,
  SyncFrame,
  SyncItem,
  Transport,
} from "./sync-engine";
import type { SyncNamespace, SyncStorage } from "./sync-store";
import { SyncHTTPError } from "./sync-error";

const live: SyncEngine[] = [];
afterEach(async () => {
  for (const engine of live.splice(0)) await engine.stop();
});

it("distinguishes a rejected input and a failed local save from an unconfirmed server result", async () => {
  const server = new Server();
  const store = memory();
  const invalid: Transport = (path, method, body) =>
    path.includes("/contexts/") && method === "POST"
      ? Promise.reject(new SyncHTTPError(400, {}))
      : server.api(path, method, body);
  const engine = await start(server, store, namespace(), invalid);
  await engine.edit("sentence:s000", {
    kind: "sentence",
    text: "invalid input",
  });
  await engine.send("sentence:s000");
  expect(engine.status).toBe("current");
  expect(engine.state.drafts["sentence:s000"]?.pending).toBeUndefined();
  expect(engine.state.drafts["sentence:s000"]?.error).toContain(
    "受け付けられていません",
  );
  const unavailable = await start(server, memory(), namespace("no-storage"));
  await unavailable.edit("sentence:s000", {
    kind: "sentence",
    text: "must persist first",
  });
  const failureStorage = (unavailable as unknown as { storage: SyncStorage })
    .storage;
  failureStorage.save = async () => {
    throw new Error("storage unavailable");
  };
  await expect(unavailable.send("sentence:s000")).rejects.toThrow();
  expect(server.posts).toHaveLength(0);
  expect(unavailable.state.drafts["sentence:s000"]?.pending).toBeUndefined();
  expect(unavailable.state.drafts["sentence:s000"]?.error).toContain(
    "送信していません",
  );
});
const namespace = (user = "alice", api = "https://api.test") => ({
  api,
  user,
  workspace: "one",
});
function memory() {
  const data = new Map<string, SavedSync>();
  const key = (n: SyncNamespace) => JSON.stringify(n);
  const storage: SyncStorage = {
    async load(n) {
      const value = data.get(key(n));
      return value ? structuredClone(value) : undefined;
    },
    async save(n, value, keys) {
      const old = data.get(key(n));
      const merged = {
        ...value,
        items: { ...old?.items },
        drafts: { ...old?.drafts },
        receipts: { ...old?.receipts },
      };
      for (const k of keys) {
        for (const field of ["items", "drafts", "receipts"] as const) {
          delete merged[field][k];
          Object.assign(
            merged[field],
            value[field][k] ? { [k]: structuredClone(value[field][k]) } : {},
          );
        }
      }
      data.set(key(n), structuredClone(merged));
    },
    async page() {
      return { entries: [], next: null };
    },
    async read() {
      return undefined;
    },
    async remove(n) {
      data.delete(key(n));
    },
  };
  return { data, storage, saved: (n = namespace()) => data.get(key(n)) };
}
function fixtureItem(id: string, revision = "v1", text = id): SyncItem {
  return {
    record: {
      ref: { kind: "sentence", id, revision },
      value: { kind: "sentence", text },
    },
    editable: true,
  };
}
function response(
  data: SyncFrame,
  position: string,
  next: string | null = null,
) {
  return {
    data,
    meta: {
      workspace_id: "one",
      position,
      next_cursor: next,
      completeness: next
        ? { state: "partial", reason: "page_remaining" }
        : { state: "complete" },
    },
  };
}
const noFrame = (): SyncFrame => ({
  items: [],
  removed: [],
  offered: [],
  permissions: [],
});

it("sends only identity fields for the currently cached objects", async () => {
  const server = new Server();
  const sent: Record<string, unknown>[] = [];
  const transport: Transport = (path, method, body) => {
    if (path.endsWith("/pull"))
      sent.push(structuredClone(body) as Record<string, unknown>);
    return server.api(path, method, body);
  };
  await start(server, memory(), namespace(), transport);
  const known = sent.flatMap(
    (request) => (request.known ?? []) as Record<string, unknown>[],
  );
  expect(known.length).toBeGreaterThan(0);
  for (const entry of known)
    expect(Object.keys(entry).sort()).toEqual(["id", "kind"]);
});

class Server {
  items = new Map<string, SyncItem>();
  journal: SyncItem[] = [];
  positions = ["opaque-z"];
  snapshots = new Map<string, { items: SyncItem[]; position: string }>();
  receipts = new Map<string, unknown>();
  posts: PendingWrite[] = [];
  online = true;
  lost = false;
  historyLost = false;
  revoked = false;
  editable = true;
  hidden = new Set<string>();
  afterChunk?: () => void;
  constructor(count = 1) {
    for (let i = 0; i < count; i++)
      this.items.set(
        `sentence:s${i.toString().padStart(3, "0")}`,
        fixtureItem(`s${i.toString().padStart(3, "0")}`),
      );
  }
  update(id: string, text: string) {
    const item = fixtureItem(id, `revision-${this.journal.length + 2}`, text);
    this.items.set(syncKey(item.record.ref), item);
    this.journal.push(structuredClone(item));
    this.positions.push(
      `opaque-${this.journal.length % 2 ? "a" : "m"}-${this.journal.length}`,
    );
    return item;
  }
  api: Transport = async (path, method, body) => {
    if (!this.online) throw new Error("network unavailable");
    if (this.revoked) throw new Error("forbidden");
    if (path.endsWith("/pull")) {
      const request = body as {
        mode: string;
        cursor?: string;
        position?: string;
        through?: string;
        known?: { kind: string; id: string }[];
        versions?: { kind: string; id: string; revision: string }[];
      };
      const frame = noFrame();
      for (const r of request.known ?? []) {
        const key = syncKey(r);
        if (this.hidden.has(key)) frame.removed.push(r);
        else frame.permissions.push({ identity: r, editable: this.editable });
      }
      expect(request.mode).toBe("watch");
      const versions = new Map(
        (request.versions ?? []).map((r) => [syncKey(r), r.revision]),
      );
      for (const ref of request.known ?? []) {
        const key = syncKey(ref);
        const item = this.items.get(key);
        if (!item || this.hidden.has(key)) {
          if (!frame.removed.some((r) => syncKey(r) === key))
            frame.removed.push(ref);
          continue;
        }
        if (versions.get(key) !== item.record.ref.revision)
          frame.items.push({
            ...structuredClone(item),
            editable: this.editable,
          });
      }
      return response(frame, this.positions.at(-1)!);
    }
    const id = path.split("/").at(-1)!;
    const key = `sentence:${id}`;
    if (method === "GET")
      return { data: structuredClone(this.items.get(key)!.record) };
    const request = structuredClone(body) as PendingWrite;
    this.posts.push(request);
    if (this.receipts.has(request.operation_id))
      return this.receipts.get(request.operation_id);
    if (this.items.get(key)!.record.ref.revision !== request.expected_version)
      throw new Error("version_conflict");
    const item = this.update(id, String(request.value.text));
    const value = { data: { version: item.record.ref.revision } };
    this.receipts.set(request.operation_id, value);
    if (this.lost) {
      this.lost = false;
      throw new Error("response lost");
    }
    return value;
  };
}
let sequence = 0;
async function start(
  server: Server,
  store = memory(),
  n = namespace(),
  api = server.api,
) {
  const engine = new SyncEngine(
    n,
    store.storage,
    api,
    () => {},
    () => `operation-${++sequence}`,
  );
  live.push(engine);
  await engine.start();
  await engine.open({ kind: "sentence", id: "s000" });
  return engine;
}

it("converges two clients after missed/repeated hints and manually reapplies a competing draft", async () => {
  const server = new Server();
  const a = await start(server);
  const b = await start(server, memory(), namespace("bob"));
  await b.edit("sentence:s000", { kind: "sentence", text: "bob local draft" });
  await a.edit("sentence:s000", { kind: "sentence", text: "alice change" });
  await a.send("sentence:s000");
  await b.send("sentence:s000");
  expect(b.state.drafts["sentence:s000"]?.value.text).toBe("bob local draft");
  expect(b.state.drafts["sentence:s000"]?.latest?.record.value.text).toBe(
    "alice change",
  );
  expect(server.items.get("sentence:s000")!.record.value.text).toBe(
    "alice change",
  );
  await b.send("sentence:s000", true);
  await a.refresh();
  await a.refresh();
  expect(a.state.items["sentence:s000"]?.record.ref).toEqual(
    b.state.items["sentence:s000"]?.record.ref,
  );
  expect(a.state.items["sentence:s000"]?.record.value.text).toBe(
    "bob local draft",
  );
  expect(server.posts.map((p) => p.operation_id)).toHaveLength(3);
});

it("restores an offline draft without automatic sending and marks a changed server basis", async () => {
  const server = new Server();
  const store = memory();
  const original = await start(server, store);
  const lastSync = original.state.confirmedAt;
  server.online = false;
  await original.refresh();
  await original.edit("sentence:s000", {
    kind: "sentence",
    text: "offline local",
  });
  expect(original.canStart()).toBe(false);
  expect(original.state.confirmedAt).toBe(lastSync);
  expect(server.posts).toHaveLength(0);
  await original.stop();
  const restarted = await start(server, store);
  expect(restarted.state.drafts["sentence:s000"]?.value.text).toBe(
    "offline local",
  );
  server.update("s000", "other terminal");
  server.online = true;
  await restarted.refresh();
  expect(restarted.notice).not.toContain("接続を確認できません");
  expect(server.posts).toHaveLength(0);
  expect(
    restarted.state.drafts["sentence:s000"]?.latest?.record.value.text,
  ).toBe("other terminal");
  await restarted.send("sentence:s000", true);
  expect(server.items.get("sentence:s000")?.record.value.text).toBe(
    "offline local",
  );
});

it("persists an unconfirmed request before send and retries the same operation after restart", async () => {
  const server = new Server();
  const store = memory();
  const engine = await start(server, store);
  await engine.edit("sentence:s000", {
    kind: "sentence",
    text: "accepted but response lost",
  });
  server.lost = true;
  await engine.send("sentence:s000");
  expect(engine.status).toBe("offline");
  expect(engine.state.drafts["sentence:s000"]?.pending).toEqual(
    server.posts[0],
  );
  expect(store.saved()?.drafts["sentence:s000"]?.pending).toEqual(
    server.posts[0],
  );
  await engine.stop();
  const restarted = await start(server, store);
  expect(server.posts).toHaveLength(1);
  await restarted.send("sentence:s000");
  expect(server.posts[1]).toEqual(server.posts[0]);
  expect(server.journal).toHaveLength(1);
  expect(restarted.state.drafts["sentence:s000"]).toBeUndefined();
  expect(restarted.state.receipts["sentence:s000"]?.operationID).toBe(
    server.posts[0]?.operation_id,
  );
});

it("checks current targets after journal loss without discarding local drafts", async () => {
  const server = new Server();
  const engine = await start(server);
  await engine.edit("sentence:s000", { kind: "sentence", text: "my unsent" });
  server.update("s000", "current server");
  server.historyLost = true;
  await engine.refresh();
  expect(engine.status).toBe("current");
  expect(engine.state.items["sentence:s000"]?.record.value.text).toBe(
    "current server",
  );
  expect(engine.state.drafts["sentence:s000"]?.value.text).toBe("my unsent");
  expect(server.posts).toHaveLength(0);
});

it("checks displayed identities without reading unrelated journal pages or bodies", async () => {
  const server = new Server(10_000);
  const requests: Record<string, unknown>[] = [];
  const api: Transport = (path, method, body) => {
    if (path.endsWith("/pull")) requests.push(body as Record<string, unknown>);
    return server.api(path, method, body);
  };
  const engine = await start(server, memory(), namespace(), api);
  expect(Object.keys(engine.state.items)).toHaveLength(1);
  for (let i = 0; i < 1000; i++) server.update("s9999", `unrelated-${i}`);
  const before = requests.length;
  await engine.refresh();
  expect(requests.length - before).toBe(1);
  expect(requests.at(-1)?.mode).toBe("watch");
  expect(requests.at(-1)?.known).toEqual([{ kind: "sentence", id: "s000" }]);
  expect(Object.keys(engine.state.items)).toHaveLength(1);
  server.update("s000", "changed current target");
  await engine.refresh();
  expect(engine.state.items["sentence:s000"]?.record.value.text).toBe(
    "changed current target",
  );
});

it("isolates actors and servers, removes revoked caches/drafts, and rejects a late response", async () => {
  const server = new Server();
  const store = memory();
  const alice = await start(server, store);
  await alice.edit("sentence:s000", {
    kind: "sentence",
    text: "private local",
  });
  const bob = await start(server, store, namespace("bob"));
  expect(bob.state.drafts).toEqual({});
  const foreign = await start(
    server,
    store,
    namespace("alice", "https://another.test"),
  );
  expect(foreign.state.drafts).toEqual({});
  server.hidden.add("sentence:s000");
  await alice.refresh();
  expect(alice.state.items).toEqual({});
  expect(alice.accessEpoch).toBeGreaterThan(0);
  expect(store.saved()?.drafts).toEqual({});
  server.hidden.clear();
  await alice.open({ kind: "sentence", id: "s000" });
  let release!: (value: unknown) => void;
  const delayed = new SyncEngine(
    namespace("delayed"),
    store.storage,
    async () =>
      new Promise((resolve) => {
        release = resolve;
      }),
  );
  live.push(delayed);
  const starting = delayed.start();
  await Promise.resolve();
  await Promise.resolve();
  await delayed.discard();
  release(
    response({ ...noFrame(), items: [fixtureItem("secret")] }, "late-position"),
  );
  await starting;
  expect(delayed.state.items).toEqual({});
  expect(store.saved(namespace("delayed"))).toBeUndefined();
  server.revoked = true;
  await bob.refresh();
  expect(bob.status).toBe("revoked");
  expect(store.saved(namespace("bob"))).toBeUndefined();
  await stopSyncActor("alice");
  expect(store.saved()).toBeUndefined();
  expect(
    store.saved(namespace("alice", "https://another.test")),
  ).toBeUndefined();
});

it("persists confirmed revocations before a later target batch fails", async () => {
  const server = new Server();
  const store = memory();
  const engine = await start(server, store);
  await engine.edit("sentence:s000", {
    kind: "sentence",
    text: "to be purged",
  });
  for (let i = 1; i < 101; i++)
    engine.state.items[`sentence:s${i}`] = fixtureItem(`s${i}`);
  await store.storage.save(
    namespace(),
    engine.state,
    Object.keys(engine.state.items),
  );
  await engine.stop();
  let requests = 0;
  const api: Transport = async () => {
    if (requests++ > 0)
      throw new Error("disconnected during next target batch");
    return response(
      { ...noFrame(), removed: [{ kind: "sentence", id: "s000" }] },
      "policy-position",
    );
  };
  const disconnected = new SyncEngine(namespace(), store.storage, api);
  live.push(disconnected);
  await disconnected.start();
  expect(disconnected.status).toBe("offline");
  expect(store.saved()?.items["sentence:s000"]).toBeUndefined();
  expect(store.saved()?.drafts["sentence:s000"]).toBeUndefined();
});

it("changes editability after role loss without converting a draft into an execution request", async () => {
  const server = new Server();
  const engine = await start(server);
  await engine.edit("sentence:s000", { kind: "sentence", text: "local" });
  server.editable = false;
  await engine.refresh();
  await engine.send("sentence:s000");
  expect(engine.state.items["sentence:s000"]?.editable).toBe(false);
  expect(server.posts).toHaveLength(0);
  expect(engine.state.drafts["sentence:s000"]?.value.text).toBe("local");
});

it("keeps a newly created unsent Context when it is opened before first publication", async () => {
  const server = new Server();
  const engine = await start(server);
  const key = await engine.create("sentence", {
    kind: "sentence",
    name: "new",
    text: "new local text",
  });
  const identity = { kind: "sentence", id: key.slice("sentence:".length) };
  await engine.open(identity);
  await engine.refresh();
  expect(engine.state.drafts[key]?.value.text).toBe("new local text");
  expect(engine.state.drafts[key]?.base.record.ref.revision).toBe("0");
  expect(server.posts).toHaveLength(0);
});

it("keeps the displayed editing basis while a related membership is opened for an explicit change", async () => {
  const server = new Server(2);
  const engine = await start(server);
  await engine.open({ kind: "sentence", id: "s001" }, true);
  expect(engine.state.items["sentence:s000"]?.record.ref.revision).toBe("v1");
  expect(engine.state.items["sentence:s001"]?.record.ref.revision).toBe("v1");
});

it("does not restore a late local-cache load after sign-out has purged the actor", async () => {
  const server = new Server();
  const store = memory();
  const original = await start(server, store);
  await original.edit("sentence:s000", {
    kind: "sentence",
    text: "private draft",
  });
  await original.stop();
  const saved = structuredClone(store.saved()!);
  let release!: (value: SavedSync) => void;
  store.storage.load = async () =>
    new Promise((resolve) => {
      release = resolve;
    });
  const restarting = new SyncEngine(namespace(), store.storage, server.api);
  live.push(restarting);
  const starting = restarting.start();
  await Promise.resolve();
  await restarting.discard();
  release(saved);
  await starting;
  expect(restarting.state.items).toEqual({});
  expect(restarting.state.drafts).toEqual({});
  expect(restarting.status).toBe("revoked");
});
