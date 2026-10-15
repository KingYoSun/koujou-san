import { decodeResponse } from "./contracts";
import type { ApiResponse, Mutation } from "./contracts";
import type { ContextIdentity, ContextRecord } from "./context-contract";
import type { SyncNamespace, SyncStorage } from "./sync-store";
import { SyncHTTPError } from "./sync-error";

export type SyncItem = {
  record: ContextRecord<Record<string, unknown>>;
  editable: boolean;
};
type Permission = { identity: ContextIdentity<string>; editable: boolean };
export type SyncFrame = {
  items: SyncItem[];
  removed: ContextIdentity<string>[];
  permissions: Permission[];
  offered: ContextIdentity<string>[];
};
export type PendingWrite = Mutation & { value: Record<string, unknown> };
export type Draft = {
  base: SyncItem;
  value: Record<string, unknown>;
  pending?: PendingWrite;
  latest?: SyncItem;
  error?: string;
};
export type SavedSync = {
  items: Record<string, SyncItem>;
  drafts: Record<string, Draft>;
  position: string | null;
  confirmedAt: string | null;
  nextCursor: string | null;
  pages: number;
  receipts: Record<string, { operationID: string; revision: string }>;
  available: boolean;
};
export type Transport = (
  path: string,
  method: string,
  body?: unknown,
) => Promise<unknown>;
export const syncKey = (ref: ContextIdentity<string>) =>
  `${ref.kind}:${ref.id}`;
const empty = (): SavedSync => ({
  items: {},
  drafts: {},
  position: null,
  confirmedAt: null,
  nextCursor: null,
  pages: 1,
  receipts: {},
  available: false,
});
const actors = new Set<SyncEngine>();
const isObject = (value: unknown): value is Record<string, unknown> =>
  value !== null && typeof value === "object" && !Array.isArray(value);
const kinds = [
  "sentence",
  "paragraph",
  "section",
  "document",
  "scope",
  "membership",
  "baseline",
  "manifest",
  "addition",
  "report",
];
function identity(value: unknown): value is ContextIdentity<string> {
  return (
    isObject(value) &&
    typeof value.id === "string" &&
    value.id.length > 0 &&
    kinds.includes(String(value.kind))
  );
}
function item(value: unknown): value is SyncItem {
  if (
    !isObject(value) ||
    !isObject(value.record) ||
    typeof value.editable !== "boolean"
  )
    return false;
  return (
    identity(value.record.ref) &&
    "revision" in value.record.ref &&
    typeof value.record.ref.revision === "string" &&
    isObject(value.record.value)
  );
}
function frame(value: unknown): value is SyncFrame {
  return (
    isObject(value) &&
    Array.isArray(value.items) &&
    value.items.every(item) &&
    Array.isArray(value.removed) &&
    value.removed.every(identity) &&
    Array.isArray(value.permissions) &&
    value.permissions.every(
      (p) =>
        isObject(p) && identity(p.identity) && typeof p.editable === "boolean",
    ) &&
    Array.isArray(value.offered) &&
    value.offered.every(identity)
  );
}
function authFailure(error: unknown) {
  return /ログインが必要|authentication_required|この操作の権限|forbidden/.test(
    String(error),
  );
}
function conflict(error: unknown) {
  return /更新されています|version_conflict/.test(String(error));
}

export class SyncEngine {
  state: SavedSync = empty();
  status: "checking" | "current" | "offline" | "revoked" = "checking";
  get available() {
    return this.state.available;
  }
  accessEpoch = 0;
  notice = "";
  busy = false;
  sending = new Set<string>();
  private alive = true;
  private revoked = false;
  private epoch = 0;
  private writes: Promise<void> = Promise.resolve();
  constructor(
    readonly namespace: SyncNamespace,
    private storage: SyncStorage,
    private api: Transport,
    private changed: () => void = () => {},
    private uuid: () => string = () => crypto.randomUUID(),
  ) {
    actors.add(this);
  }
  private emit() {
    if (this.alive) this.changed();
  }
  private persist(keys: string[] = []) {
    const snapshot: SavedSync = {
      ...this.state,
      items: {},
      drafts: {},
      receipts: {},
    };
    for (const key of new Set(keys)) {
      if (this.state.items[key])
        snapshot.items[key] = structuredClone(this.state.items[key]!);
      if (this.state.drafts[key])
        snapshot.drafts[key] = structuredClone(this.state.drafts[key]!);
      if (this.state.receipts[key])
        snapshot.receipts[key] = structuredClone(this.state.receipts[key]!);
    }
    this.writes = this.writes
      .catch(() => {})
      .then(async () => {
        if (!this.revoked)
          await this.storage.save(this.namespace, snapshot, keys);
      });
    return this.writes;
  }
  async start() {
    try {
      const saved = await this.storage.load(this.namespace);
      if (!this.alive || this.revoked) return;
      if (saved) {
        this.state = saved;
      }
      this.emit();
      await this.refresh();
    } catch {
      this.status = "offline";
      this.notice = "保存領域と接続を確認してください。";
      this.emit();
    }
  }
  async stop() {
    this.alive = false;
    this.epoch++;
    await this.writes.catch(() => {});
    actors.delete(this);
  }
  async discard() {
    this.revoked = true;
    this.epoch++;
    this.status = "revoked";
    this.state = empty();
    this.notice =
      "権限を確認できないため、本文キャッシュと下書きを削除しました。";
    this.emit();
    await this.writes.catch(() => {});
    await this.storage.remove(this.namespace);
  }
  private watched = new Map<string, ContextIdentity<string>>();
  private known() {
    const records = [
      ...Object.values(this.state.items),
      ...Object.values(this.state.drafts).map((d) => d.base),
    ];
    return [
      ...new Map(
        records
          .filter((i) => i.record.ref.revision !== "0")
          .map((i) => [syncKey(i.record.ref), i.record.ref]),
      ).values(),
    ];
  }
  async open(identity: ContextIdentity<string>, keepCurrent = false) {
    const key = syncKey(identity);
    if (!keepCurrent) this.watched.clear();
    this.watched.set(key, identity);
    for (const cached of Object.keys(this.state.items))
      if (!keepCurrent && cached !== key && !this.state.drafts[cached]) {
        delete this.state.items[cached];
        delete this.state.receipts[cached];
      }
    const epoch = this.epoch;
    if (!this.state.items[key] && !this.state.drafts[key]) {
      const stored = await this.storage.read(this.namespace, key);
      if (!this.alive || this.revoked || epoch !== this.epoch) return;
      if (stored?.item) this.state.items[key] = stored.item;
      if (stored?.draft) this.state.drafts[key] = stored.draft;
      if (stored?.receipt) this.state.receipts[key] = stored.receipt;
    }
    await this.refresh(
      this.state.drafts[key]?.base.record.ref.revision === "0"
        ? undefined
        : identity,
    );
    this.emit();
  }
  async create(kind: string, value: Record<string, unknown>) {
    const id = this.uuid();
    const key = syncKey({ kind, id });
    this.watched.set(key, { kind, id });
    this.state.drafts[key] = {
      base: {
        record: { ref: { kind, id, revision: "0" }, value },
        editable: true,
      },
      value: structuredClone(value),
    };
    await this.persist([key]);
    this.emit();
    return key;
  }
  async draftPage(after?: string) {
    const epoch = this.epoch;
    await this.writes;
    const page = await this.storage.page(this.namespace, after, true);
    if (!this.alive || this.revoked || epoch !== this.epoch)
      return { entries: [], next: null };
    const visible = new Set([
      ...page.entries.map((entry) => entry.key),
      ...this.watched.keys(),
      ...this.sending,
    ]);
    for (const key of Object.keys(this.state.drafts))
      if (!visible.has(key)) {
        delete this.state.drafts[key];
        delete this.state.items[key];
        delete this.state.receipts[key];
      }
    for (const entry of page.entries) {
      if (entry.item) this.state.items[entry.key] = entry.item;
      if (entry.draft) this.state.drafts[entry.key] ??= entry.draft;
      if (entry.receipt) this.state.receipts[entry.key] = entry.receipt;
    }
    await this.refresh();
    this.emit();
    return page;
  }
  private async pull(
    request: Record<string, unknown>,
  ): Promise<ApiResponse<SyncFrame>> {
    const value = await this.api(
      `/sync/workspaces/${encodeURIComponent(this.namespace.workspace)}/pull`,
      "POST",
      request,
    );
    return decodeResponse(value, frame, this.namespace.workspace);
  }
  async refresh(extra?: ContextIdentity<string>) {
    if (this.busy || !this.alive || this.revoked) return;
    this.busy = true;
    const reconnecting = this.status === "offline";
    const epoch = this.epoch;
    const current = () => this.alive && !this.revoked && epoch === this.epoch;
    this.emit();
    try {
      const known = new Map(this.known().map((ref) => [syncKey(ref), ref]));
      for (const [key, identity] of this.watched)
        if (
          !known.has(key) &&
          this.state.drafts[key]?.base.record.ref.revision !== "0"
        )
          known.set(key, { ...identity, revision: "" });
      if (extra && !known.has(syncKey(extra)))
        known.set(syncKey(extra), { ...extra, revision: "" });
      const refs = [...known.values()];
      const changed = new Set<string>();
      for (let offset = 0; offset < Math.max(1, refs.length); offset += 100) {
        const chunk = refs.slice(offset, offset + 100);
        const response = await this.pull({
          mode: "watch",
          known: chunk.map(({ kind, id }) => ({ kind, id })),
          versions: chunk.filter((ref) => ref.revision),
        });
        if (!current()) return;
        if (response.error || response.meta.completeness.state !== "complete")
          throw new Error("同期を確認できません。");
        if (response.data.removed.length) this.accessEpoch++;
        for (const ref of response.data.removed) {
          const key = syncKey(ref);
          delete this.state.items[key];
          delete this.state.drafts[key];
          delete this.state.receipts[key];
          changed.add(key);
        }
        // Persist confirmed removals before another network request can fail.
        if (response.data.removed.length)
          await this.persist(response.data.removed.map(syncKey));
        if (!current()) return;
        for (const permission of response.data.permissions) {
          const key = syncKey(permission.identity);
          const item = this.state.items[key];
          const draft = this.state.drafts[key];
          if (item && item.editable !== permission.editable) {
            item.editable = permission.editable;
            changed.add(key);
          }
          if (draft && draft.base.editable !== permission.editable) {
            draft.base.editable = permission.editable;
            changed.add(key);
          }
        }
        for (const item of response.data.items) {
          const key = syncKey(item.record.ref);
          this.state.items[key] = item;
          const draft = this.state.drafts[key];
          if (
            draft &&
            !draft.pending &&
            item.record.ref.revision !== draft.base.record.ref.revision
          )
            draft.latest = structuredClone(item);
          changed.add(key);
        }
        this.state.position = response.meta.position;
      }
      this.state.confirmedAt = new Date().toISOString();
      this.status = "current";
      if (reconnecting)
        this.notice =
          "接続を確認しました。未送信の下書き・最新内容を確認してから送信してください。";
      await this.persist([...changed]);
    } catch (error) {
      if (!current()) return;
      if (authFailure(error)) await this.discard();
      else {
        this.status = "offline";
        this.notice =
          "接続を確認できません。最後に確認したキャッシュを表示しています。";
      }
    } finally {
      if (current()) {
        this.busy = false;
        this.emit();
      }
    }
  }
  async edit(key: string, value: Record<string, unknown>) {
    const base = this.state.items[key] ?? this.state.drafts[key]?.base;
    if (
      !base?.editable ||
      this.state.drafts[key]?.pending ||
      this.sending.has(key) ||
      this.revoked
    )
      return;
    const previous = this.state.drafts[key];
    this.state.drafts[key] = {
      base: previous?.base ?? structuredClone(base),
      value: structuredClone(value),
      latest: previous?.latest,
    };
    this.emit();
    await this.persist([key]);
  }
  async send(key: string, reapply = false) {
    const draft = this.state.drafts[key];
    if (
      !draft ||
      this.status !== "current" ||
      this.sending.has(key) ||
      this.revoked ||
      !draft.base.editable
    )
      return;
    if (draft.latest && !reapply) return;
    if (reapply && draft.latest) {
      draft.base = structuredClone(draft.latest);
      draft.pending = undefined;
      draft.latest = undefined;
    }
    const hadPending = Boolean(draft.pending);
    draft.error = undefined;
    if (!draft.pending)
      draft.pending = {
        operation_id: this.uuid(),
        expected_version: draft.base.record.ref.revision,
        value: structuredClone(draft.value),
      };
    const pending = draft.pending;
    const ref = draft.base.record.ref;
    const path = `/contexts/workspaces/${encodeURIComponent(this.namespace.workspace)}/${encodeURIComponent(ref.kind)}/${encodeURIComponent(ref.id)}`;
    const epoch = this.epoch;
    this.sending.add(key);
    this.emit();
    let attempted = false;
    try {
      await this.persist([key]);
      if (!this.alive || this.revoked || epoch !== this.epoch) return;
      attempted = true;
      const result = (await this.api(path, "POST", pending)) as {
        data?: { version: string };
        error?: unknown;
      };
      if (!this.alive || this.revoked || epoch !== this.epoch) return;
      if (!result.data || result.error)
        throw new Error("保存結果を確認できません。");
      this.state.receipts[key] = {
        operationID: pending.operation_id,
        revision: result.data.version,
      };
      delete this.state.drafts[key];
      this.notice =
        "保存要求が受け付けられました。確定した内容を再取得します。";
      await this.persist([key]);
      await this.refresh();
    } catch (error) {
      if (!this.alive || this.revoked || epoch !== this.epoch) return;
      if (!attempted) {
        if (!hadPending) draft.pending = undefined;
        draft.error = "送信前の保存を確認できないため、送信していません。";
      } else if (error instanceof SyncHTTPError && error.status === 400) {
        draft.pending = undefined;
        draft.error =
          "入力内容を確認してください。保存は受け付けられていません。";
      } else if (conflict(error)) {
        draft.pending = undefined;
        try {
          const latest = (await this.api(path, "GET")) as {
            data: ContextRecord<Record<string, unknown>>;
          };
          if (!this.alive || this.revoked || epoch !== this.epoch) return;
          draft.latest = { record: latest.data, editable: draft.base.editable };
          this.notice =
            "他の端末で更新されています。最新内容と下書きを確認して、再適用してください。";
        } catch {
          this.status = "offline";
          this.notice =
            "競合後の最新内容を取得できません。下書きを保持して接続を確認します。";
        }
      } else if (authFailure(error)) {
        if (!hadPending) draft.pending = undefined;
        this.notice = "保存権限を確認できません。現在の権限を再取得します。";
        await this.refresh();
      } else {
        this.status = "offline";
        this.notice = "保存結果は未確認です。同じ内容と操作IDで再送できます。";
      }
      await this.persist([key]);
    } finally {
      this.sending.delete(key);
      this.emit();
    }
  }
  canStart(): boolean {
    return this.status === "current" && !this.revoked;
  }
}

export async function stopSyncActor(user: string) {
  const engines = [...actors].filter((e) => e.namespace.user === user);
  for (const engine of engines) await engine.discard();
}
export async function discardSyncWorkspace(namespace: SyncNamespace) {
  for (const engine of [...actors])
    if (
      engine.namespace.api === namespace.api &&
      engine.namespace.user === namespace.user &&
      engine.namespace.workspace === namespace.workspace
    )
      await engine.discard();
}
