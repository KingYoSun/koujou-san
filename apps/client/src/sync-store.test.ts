import "fake-indexeddb/auto";
import { IDBFactory, IDBObjectStore } from "fake-indexeddb";
import { beforeEach, expect, it, vi } from "vitest";
import {
  allowAnswerActor,
  allowAnswerScope,
  answerScopeEpoch,
  readAnswerDraft,
  saveAnswerDraft,
  purgeStoredActor,
  syncStorage,
  readLocalDraft,
  saveLocalDraft,
} from "./sync-store";
import type { SavedSync, SyncItem } from "./sync-engine";

const n = { api: "https://api.test/", user: "alice", workspace: "one" };

it("keeps workflow drafts separate from Pause drafts and purges both with the Workspace", async () => {
  await saveLocalDraft(
    false,
    n,
    "same-id",
    { text: '{"schema":"workflow-definition-v1"}', version: "fixed" },
    "workflow-definition",
  );
  await saveAnswerDraft(false, n, "same-id", {
    pauseVersion: "pause-v",
    value: "answer",
    json: false,
  });
  expect(
    (
      await readLocalDraft<{ text: string }>(
        false,
        n,
        "same-id",
        "workflow-definition",
      )
    )?.text,
  ).toContain("workflow-definition-v1");
  expect((await readAnswerDraft(false, n, "same-id"))?.value).toBe("answer");
  await syncStorage(false).remove(n);
  allowAnswerScope(n);
  expect(
    await readLocalDraft(false, n, "same-id", "workflow-definition"),
  ).toBeUndefined();
  expect(await readAnswerDraft(false, n, "same-id")).toBeUndefined();
});
beforeEach(() => {
  globalThis.indexedDB = new IDBFactory();
  allowAnswerActor(n.user);
  allowAnswerScope(n);
});

it("restores only the requested Pause draft, preserves its version and serializes writes before purge", async () => {
  const draft = {
    pauseVersion: "question-version",
    value: "offline answer",
    json: false,
  };
  const other = { ...n, workspace: "other-answers" };
  allowAnswerScope(other);
  await seed(legacy(10_000));
  const scan = vi.spyOn(IDBObjectStore.prototype, "openCursor");
  await saveAnswerDraft(false, n, "run-pause:one:first", draft);
  await saveAnswerDraft(false, n, "run-pause:one:second", {
    ...draft,
    value: "second question",
  });
  await saveAnswerDraft(false, other, "run-pause:one:first", draft);
  expect(await readAnswerDraft(false, n, "run-pause:one:first")).toEqual(draft);
  expect(
    await readAnswerDraft(
      false,
      { ...n, api: "https://other.test/" },
      "run-pause:one:first",
    ),
  ).toBeUndefined();
  expect(scan).not.toHaveBeenCalled();
  scan.mockRestore();
  await syncStorage(false).load(n);
  expect(
    (await syncStorage(false).read(n, "sentence:s09999"))?.draft,
  ).toBeDefined();
  expect((await readAnswerDraft(false, n, "run-pause:one:second"))?.value).toBe(
    "second question",
  );
  const pending = saveAnswerDraft(false, n, "run-pause:one:first", {
    ...draft,
    value: "last keystroke",
  });
  const oldQuery = answerScopeEpoch(n);
  await syncStorage(false).remove(n);
  await pending;
  expect(allowAnswerScope(n, oldQuery)).toBe(false);
  await saveAnswerDraft(false, n, "run-pause:one:first", draft);
  allowAnswerScope(n);
  expect(
    await readAnswerDraft(false, n, "run-pause:one:first"),
  ).toBeUndefined();
  expect(await readAnswerDraft(false, other, "run-pause:one:first")).toEqual(
    draft,
  );
  const pendingLogout = saveAnswerDraft(
    false,
    other,
    "run-pause:one:first",
    draft,
  );
  await purgeStoredActor(false, n.user);
  await pendingLogout;
  await saveAnswerDraft(false, other, "run-pause:one:first", draft);
  allowAnswerActor(n.user);
  expect(
    await readAnswerDraft(false, other, "run-pause:one:first"),
  ).toBeUndefined();
});
function legacy(count: number): SavedSync {
  const state: SavedSync = {
    items: {},
    drafts: {},
    receipts: {},
    position: "opaque",
    confirmedAt: "2026-10-16T00:00:00Z",
    nextCursor: null,
    pages: 1,
    available: false,
  };
  for (let i = 0; i < count; i++) {
    const id = `s${i.toString().padStart(5, "0")}`;
    const key = `sentence:${id}`;
    const item: SyncItem = {
      record: {
        ref: { kind: "sentence", id, revision: "old" },
        value: { kind: "sentence", text: id },
      },
      editable: true,
    };
    state.items[key] = item;
    state.drafts[key] = {
      base: item,
      value: { kind: "sentence", text: "unsent" },
      pending: {
        operation_id: `pending-${i}`,
        expected_version: "old",
        value: { kind: "sentence", text: "unsent" },
      },
      latest: {
        ...item,
        record: {
          ...item.record,
          ref: { ...item.record.ref, revision: "other" },
        },
      },
      error: "unconfirmed",
    };
    state.receipts[key] = { operationID: `prior-${i}`, revision: "old" };
  }
  return state;
}
async function seed(state: unknown, user = n.user, workspace = n.workspace) {
  const db = await new Promise<IDBDatabase>((resolve) => {
    const request = indexedDB.open("koujou-sync-v1");
    request.onupgradeneeded = () => request.result.createObjectStore("states");
    request.onsuccess = () => resolve(request.result);
  });
  await new Promise<void>((resolve, reject) => {
    const tx = db.transaction("states", "readwrite");
    tx.objectStore("states").put(
      state,
      JSON.stringify([n.api, user, workspace]),
    );
    tx.oncomplete = () => resolve();
    tx.onerror = () => reject(tx.error);
  });
  db.close();
}
it("migrates every draft atomically but starts and writes bounded pages", async () => {
  const old = legacy(10_000);
  await seed(old);
  const storage = syncStorage(false);
  const state = (await storage.load(n))!;
  expect(Object.keys(state.items)).toHaveLength(100);
  expect(Object.keys(state.drafts)).toHaveLength(100);
  expect(state.position).toBe(old.position);
  const lastKey = "sentence:s09999";
  expect((await storage.read(n, lastKey))?.draft).toEqual(old.drafts[lastKey]);
  expect((await storage.read(n, lastKey))?.receipt).toEqual(
    old.receipts[lastKey],
  );
  const page = await storage.page(n, "sentence:s00099", true);
  expect(page.entries).toHaveLength(100);
  expect(page.entries[0]!.key).toBe("sentence:s00100");
  expect(page.next).toBe("sentence:s00199");
  const put = vi.spyOn(IDBObjectStore.prototype, "put");
  state.drafts["sentence:s00000"]!.value.text = "local change";
  await storage.save(n, state, ["sentence:s00000"]);
  expect(put).toHaveBeenCalledTimes(2); // One Context and a small synchronization header.
  put.mockRestore();
  expect(
    (await syncStorage(false).read(n, "sentence:s00000"))?.draft?.value.text,
  ).toBe("local change");
  expect((await storage.read(n, lastKey))?.draft).toEqual(old.drafts[lastKey]);
  const restarted = (await storage.load(n))!;
  expect(restarted.drafts["sentence:s00000"]?.pending?.operation_id).toBe(
    "pending-0",
  );
});
it("keeps API/User/Workspace boundaries and deletes caches plus drafts on revocation", async () => {
  await seed(legacy(2));
  const storage = syncStorage(false);
  const state = (await storage.load(n))!;
  const bob = { ...n, user: "bob" };
  const two = { ...n, workspace: "two" };
  await storage.save(bob, state, Object.keys(state.items));
  await storage.save(two, state, Object.keys(state.items));
  expect(
    await storage.load({ ...n, api: "https://another.test/" }),
  ).toBeUndefined();
  await storage.remove(n);
  expect(await storage.read(n, "sentence:s00000")).toBeUndefined();
  expect((await storage.load(bob))?.drafts["sentence:s00000"]).toBeDefined();
  await purgeStoredActor(false, "alice");
  expect(await storage.load(two)).toBeUndefined();
  expect((await storage.load(bob))?.drafts["sentence:s00000"]).toBeDefined();
});

it("retries an aborted migration from the unchanged legacy drafts", async () => {
  const old = legacy(205);
  await seed(old);
  const put = vi
    .spyOn(IDBObjectStore.prototype, "put")
    .mockImplementationOnce(() => {
      throw new DOMException("owned fixture failure", "QuotaExceededError");
    });
  await expect(syncStorage(false).load(n)).rejects.toThrow(
    "保存を確認できません",
  );
  put.mockRestore();
  const entry = await syncStorage(false).load(n);
  expect(entry?.drafts["sentence:s00000"]).toEqual(
    old.drafts["sentence:s00000"],
  );
  expect((await syncStorage(false).read(n, "sentence:s00204"))?.draft).toEqual(
    old.drafts["sentence:s00204"],
  );
});

it("finishes a retried legacy migration before accepting a new record write", async () => {
  const old = legacy(205);
  delete old.drafts["sentence:s00000"]!.pending;
  await seed(old);
  const storage = syncStorage(false);
  const put = vi
    .spyOn(IDBObjectStore.prototype, "put")
    .mockImplementationOnce(() => {
      throw new DOMException("quota", "QuotaExceededError");
    });
  await expect(storage.load(n)).rejects.toThrow();
  put.mockRestore();
  const changed = legacy(1);
  delete changed.drafts["sentence:s00000"]!.pending;
  changed.drafts["sentence:s00000"]!.value.text = "newer unsent content";
  await storage.save(n, changed, ["sentence:s00000"]);
  expect((await storage.load(n))?.drafts["sentence:s00000"]?.value.text).toBe(
    "newer unsent content",
  );
  expect((await storage.read(n, "sentence:s00204"))?.draft).toEqual(
    old.drafts["sentence:s00204"],
  );
});
