import { invoke } from "@tauri-apps/api/core";
import type { SavedSync } from "./sync-engine";

export type SyncNamespace = { api: string; user: string; workspace: string };
export interface SyncStorage {
  load(namespace: SyncNamespace): Promise<SavedSync | undefined>;
  save(namespace: SyncNamespace, value: SavedSync): Promise<void>;
  remove(namespace: SyncNamespace): Promise<void>;
}
const key = (n: SyncNamespace) => JSON.stringify([n.api, n.user, n.workspace]);
async function database(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const request = indexedDB.open("koujou-sync-v1", 1);
    request.onupgradeneeded = () => request.result.createObjectStore("states");
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(new Error("下書きの保存領域を開けません。"));
  });
}
async function transaction<T>(
  write: boolean,
  run: (store: IDBObjectStore) => IDBRequest<T>,
): Promise<T> {
  const db = await database();
  try {
    return await new Promise<T>((resolve, reject) => {
      const tx = db.transaction("states", write ? "readwrite" : "readonly");
      const request = run(tx.objectStore("states"));
      tx.oncomplete = () => resolve(request.result);
      tx.onerror = () => reject(new Error("下書きの保存を確認できません。"));
      tx.onabort = () => reject(new Error("下書きの保存を確認できません。"));
    });
  } finally {
    db.close();
  }
}
export function syncStorage(native: boolean): SyncStorage {
  return {
    async load(n) {
      const value = native
        ? await invoke<SavedSync | null>("sync_load", {
            user: n.user,
            workspace: n.workspace,
          })
        : ((await transaction(false, (store) => store.get(key(n)))) as
            SavedSync | undefined);
      return value ?? undefined;
    },
    async save(n, value) {
      if (native)
        await invoke("sync_save", {
          user: n.user,
          workspace: n.workspace,
          payload: value,
        });
      else await transaction(true, (store) => store.put(value, key(n)));
    },
    async remove(n) {
      if (native)
        await invoke("sync_remove", { user: n.user, workspace: n.workspace });
      else await transaction(true, (store) => store.delete(key(n)));
    },
  };
}
export async function purgeStoredActor(native: boolean, user: string) {
  if (native) {
    await invoke("sync_purge_actor", { user });
    return;
  }
  const keys = await transaction(false, (store) => store.getAllKeys());
  for (const candidate of keys) {
    if (typeof candidate !== "string") continue;
    const namespace = JSON.parse(candidate) as [string, string, string];
    if (namespace[1] === user)
      await transaction(true, (store) => store.delete(candidate));
  }
}
