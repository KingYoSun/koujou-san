import { invoke } from "@tauri-apps/api/core";
import type { Draft, SavedSync, SyncItem } from "./sync-engine";

export type SyncNamespace = { api: string; user: string; workspace: string };
export type LocalEntry = {
  key: string;
  item?: SyncItem;
  draft?: Draft;
  receipt?: SavedSync["receipts"][string];
};
export type LocalPage = { entries: LocalEntry[]; next: string | null };
export type SyncHeader = Omit<SavedSync, "items" | "drafts" | "receipts">;
export interface SyncStorage {
  load(namespace: SyncNamespace): Promise<SavedSync | undefined>;
  save(
    namespace: SyncNamespace,
    value: SavedSync,
    keys: string[],
  ): Promise<void>;
  page(
    namespace: SyncNamespace,
    after?: string,
    draftsOnly?: boolean,
  ): Promise<LocalPage>;
  read(namespace: SyncNamespace, key: string): Promise<LocalEntry | undefined>;
  remove(namespace: SyncNamespace): Promise<void>;
}
const namespace = (n: SyncNamespace) => [n.api, n.user, n.workspace];
const legacyKey = (n: SyncNamespace) => JSON.stringify(namespace(n));
export type AnswerDraft = {
  pauseVersion: string;
  value: string;
  json: boolean;
};
const answerWrites = new Map<string, Promise<void>>();
const closedAnswerActors = new Set<string>();
const closedAnswerScopes = new Set<string>();
const answerScopeEpochs = new Map<string, number>();
export const answerScopeEpoch = (n: SyncNamespace) =>
  answerScopeEpochs.get(legacyKey(n)) ?? 0;
export function allowAnswerActor(user: string) {
  closedAnswerActors.delete(user);
}
export function allowAnswerScope(
  n: SyncNamespace,
  observedEpoch = answerScopeEpoch(n),
) {
  if (observedEpoch !== answerScopeEpoch(n)) return false;
  closedAnswerScopes.delete(legacyKey(n));
  return true;
}
const answerKey = (n: SyncNamespace, key: string) =>
  JSON.stringify([...namespace(n), "pause-answer", key]);
export async function readAnswerDraft(
  native: boolean,
  n: SyncNamespace,
  key: string,
): Promise<AnswerDraft | undefined> {
  await answerWrites.get(n.user)?.catch(() => {});
  if (closedAnswerActors.has(n.user) || closedAnswerScopes.has(legacyKey(n)))
    return;
  if (native)
    return (
      (await invoke<AnswerDraft | null>("run_answer_draft", {
        ...n,
        key,
        action: "read",
        value: null,
      })) ?? undefined
    );
  return transaction<AnswerDraft | undefined>(["states"], false, (tx, done) => {
    const request = tx.objectStore("states").get(answerKey(n, key));
    request.onsuccess = () => done(request.result as AnswerDraft | undefined);
  });
}
export function saveAnswerDraft(
  native: boolean,
  n: SyncNamespace,
  key: string,
  value?: AnswerDraft,
): Promise<void> {
  const work = (answerWrites.get(n.user) ?? Promise.resolve())
    .catch(() => {})
    .then(async () => {
      if (
        closedAnswerActors.has(n.user) ||
        closedAnswerScopes.has(legacyKey(n))
      )
        return;
      if (native) {
        await invoke("run_answer_draft", {
          ...n,
          key,
          action: value ? "save" : "remove",
          value: value ?? null,
        });
        return;
      }
      await transaction<void>(["states"], true, (tx, done) => {
        const store = tx.objectStore("states");
        if (value) store.put(value, answerKey(n, key));
        else store.delete(answerKey(n, key));
        done();
      });
    });
  answerWrites.set(n.user, work);
  return work;
}
export function localEntry(value: SavedSync, key: string): LocalEntry {
  return {
    key,
    item: value.items[key],
    draft: value.drafts[key],
    receipt: value.receipts[key],
  };
}
export function syncHeader(value: SavedSync): SyncHeader {
  const {
    items: _items,
    drafts: _drafts,
    receipts: _receipts,
    ...header
  } = value;
  return header;
}
async function database(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const request = indexedDB.open("koujou-sync-v1", 2);
    request.onupgradeneeded = () => {
      const db = request.result;
      if (!db.objectStoreNames.contains("states"))
        db.createObjectStore("states");
      db.createObjectStore("meta");
      const records = db.createObjectStore("records", {
        keyPath: ["api", "user", "workspace", "key"],
      });
      records.createIndex("actor", "user");
      records.createIndex("drafts", [
        "api",
        "user",
        "workspace",
        "dirty",
        "key",
      ]);
    };
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(new Error("下書きの保存領域を開けません。"));
  });
}
function row(n: SyncNamespace, entry: LocalEntry) {
  return { ...n, ...entry, dirty: entry.draft ? 1 : 0 };
}
async function transaction<T>(
  stores: string[],
  write: boolean,
  run: (tx: IDBTransaction, done: (value: T) => void) => void,
): Promise<T> {
  const db = await database();
  try {
    return await new Promise<T>((resolve, reject) => {
      const tx = db.transaction(stores, write ? "readwrite" : "readonly");
      let value: T;
      tx.oncomplete = () => resolve(value);
      tx.onerror = tx.onabort = () =>
        reject(new Error("下書きの保存を確認できません。"));
      run(tx, (result) => {
        value = result;
      });
    });
  } finally {
    db.close();
  }
}
async function migrate(n: SyncNamespace) {
  // Removing the legacy value and committing every entry are atomic. An aborted
  // transaction leaves the original Workspace blob available for the next try.
  await transaction<void>(["states", "records", "meta"], true, (tx, done) => {
    const old = tx.objectStore("states").get(legacyKey(n));
    old.onsuccess = () => {
      try {
        const value = old.result as SavedSync | undefined;
        if (value) {
          for (const key of new Set([
            ...Object.keys(value.items),
            ...Object.keys(value.drafts),
            ...Object.keys(value.receipts),
          ]))
            tx.objectStore("records").put(row(n, localEntry(value, key)));
          tx.objectStore("meta").put(syncHeader(value), namespace(n));
          tx.objectStore("states").delete(legacyKey(n));
        }
        done();
      } catch {
        tx.abort();
      }
    };
  });
}
function range(n: SyncNamespace, after = "", draftsOnly = false) {
  const prefix = draftsOnly ? [...namespace(n), 1] : namespace(n);
  return IDBKeyRange.bound([...prefix, after], [...prefix, []], true, true);
}
export function syncStorage(native: boolean): SyncStorage {
  const migrations = new Map<string, Promise<void>>();
  const ensure = (n: SyncNamespace) => {
    const key = legacyKey(n);
    let pending = migrations.get(key);
    if (!pending) {
      pending = migrate(n).catch((error) => {
        migrations.delete(key);
        throw error;
      });
      migrations.set(key, pending);
    }
    return pending;
  };
  const args = (n: SyncNamespace) => ({ user: n.user, workspace: n.workspace });
  const storage: SyncStorage = {
    async load(n) {
      let header: SyncHeader | undefined;
      if (native)
        header =
          (await invoke<SyncHeader | null>("sync_load", args(n))) ?? undefined;
      else {
        await ensure(n);
        header = await transaction<SyncHeader | undefined>(
          ["meta"],
          false,
          (tx, done) => {
            const request = tx.objectStore("meta").get(namespace(n));
            request.onsuccess = () =>
              done(request.result as SyncHeader | undefined);
          },
        );
      }
      if (!header) return;
      const value: SavedSync = {
        ...header,
        items: {},
        drafts: {},
        receipts: {},
      };
      // Startup is bounded even if migration preserved millions of records.
      const [cached, drafts] = await Promise.all([
        storage.page(n),
        storage.page(n, undefined, true),
      ]);
      for (const entry of [...cached.entries, ...drafts.entries]) {
        if (entry.item) value.items[entry.key] = entry.item;
        if (entry.draft) value.drafts[entry.key] = entry.draft;
        if (entry.receipt) value.receipts[entry.key] = entry.receipt;
      }
      return value;
    },
    async save(n, value, keys) {
      if (!native) await ensure(n);
      const header = syncHeader(value);
      const entries = keys.map((key) => localEntry(value, key));
      if (native) await invoke("sync_save", { ...args(n), header, entries });
      else
        await transaction<void>(["records", "meta"], true, (tx, done) => {
          tx.objectStore("meta").put(header, namespace(n));
          for (const entry of entries) {
            if (entry.item || entry.draft || entry.receipt)
              tx.objectStore("records").put(row(n, entry));
            else tx.objectStore("records").delete([...namespace(n), entry.key]);
          }
          done();
        });
    },
    async page(n, after = "", draftsOnly = false) {
      if (!native) await ensure(n);
      if (native)
        return invoke<LocalPage>("sync_page", {
          ...args(n),
          after,
          draftsOnly,
        });
      return transaction<LocalPage>(["records"], false, (tx, done) => {
        const records = tx.objectStore("records");
        const request = draftsOnly
          ? records.index("drafts").getAll(range(n, after, true), 101)
          : records.getAll(range(n, after), 101);
        request.onsuccess = () => {
          const entries = request.result as LocalEntry[];
          const more = entries.length > 100;
          done({
            entries: entries.slice(0, 100),
            next: more ? entries[99]!.key : null,
          });
        };
      });
    },
    async read(n, key) {
      if (!native) await ensure(n);
      if (native)
        return (
          (await invoke<LocalEntry | null>("sync_read", { ...args(n), key })) ??
          undefined
        );
      return transaction<LocalEntry | undefined>(
        ["records"],
        false,
        (tx, done) => {
          const request = tx.objectStore("records").get([...namespace(n), key]);
          request.onsuccess = () =>
            done(request.result as LocalEntry | undefined);
        },
      );
    },
    async remove(n) {
      closedAnswerScopes.add(legacyKey(n));
      answerScopeEpochs.set(legacyKey(n), answerScopeEpoch(n) + 1);
      await answerWrites.get(n.user)?.catch(() => {});
      if (native) {
        await invoke("sync_remove", args(n));
        return;
      }
      await transaction<void>(
        ["states", "records", "meta"],
        true,
        (tx, done) => {
          tx.objectStore("states").delete(legacyKey(n));
          const prefix = legacyKey(n).slice(0, -1) + ",";
          tx.objectStore("states").delete(
            IDBKeyRange.bound(prefix, prefix + "\uffff"),
          );
          tx.objectStore("meta").delete(namespace(n));
          tx.objectStore("records").delete(range(n));
          done();
        },
      );
    },
  };
  return storage;
}
export async function purgeStoredActor(native: boolean, user: string) {
  closedAnswerActors.add(user);
  await answerWrites.get(user)?.catch(() => {});
  if (native) {
    await invoke("sync_purge_actor", { user });
    return;
  }
  await transaction<void>(["states", "records", "meta"], true, (tx, done) => {
    const records = tx
      .objectStore("records")
      .index("actor")
      .openCursor(IDBKeyRange.only(user));
    records.onsuccess = () => {
      const cursor = records.result;
      if (cursor) {
        cursor.delete();
        cursor.continue();
      }
    };
    for (const name of ["states", "meta"]) {
      const request = tx.objectStore(name).openKeyCursor();
      request.onsuccess = () => {
        const cursor = request.result;
        if (!cursor) return;
        const key =
          typeof cursor.key === "string"
            ? (JSON.parse(cursor.key) as unknown[])
            : (cursor.key as unknown[]);
        if (key[1] === user) tx.objectStore(name).delete(cursor.primaryKey);
        cursor.continue();
      };
    }
    done();
  });
}
