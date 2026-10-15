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
  private persist() {
    const snapshot = structuredClone(this.state);
    this.writes = this.writes
      .catch(() => {})
      .then(async () => {
        if (!this.revoked) await this.storage.save(this.namespace, snapshot);
      });
    return this.writes;
  }
  async start() {
    try {
      const saved = await this.storage.load(this.namespace);
      if (!this.alive) return;
      if (saved) this.state = saved;
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
  private known() {
    const records = Object.values(this.state.items).map((i) => i.record.ref);
    for (const d of Object.values(this.state.drafts))
      records.push(d.base.record.ref);
    return [
      ...new Map(
        records.map((r) => [syncKey(r), { kind: r.kind, id: r.id }]),
      ).values(),
    ];
  }
  private async applyPolicy(
    result: SyncFrame,
    staging: Record<string, SyncItem>,
  ) {
    for (const r of result.removed) {
      const key = syncKey(r);
      delete staging[key];
      delete this.state.items[key];
      delete this.state.drafts[key];
      delete this.state.receipts[key];
    }
    for (const p of result.permissions) {
      const key = syncKey(p.identity);
      if (staging[key])
        staging[key] = { ...staging[key]!, editable: p.editable };
      if (this.state.items[key])
        this.state.items[key] = {
          ...this.state.items[key]!,
          editable: p.editable,
        };
      if (this.state.drafts[key])
        this.state.drafts[key]!.base.editable = p.editable;
    }
    this.emit();
    if (result.removed.length) await this.persist();
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
  async refresh(snapshot = false) {
    if (this.busy || !this.alive || this.revoked) return;
    this.busy = true;
    const reconnecting = this.status === "offline";
    const epoch = this.epoch;
    this.emit();
    const current = () => this.alive && !this.revoked && epoch === this.epoch;
    try {
      let position = this.state.position;
      let staging = structuredClone(this.state.items);
      let nextCursor = this.state.nextCursor;
      let reset = snapshot || !position;
      let attempts = 0;
      while (attempts++ < 2) {
        if (reset) {
          this.state.available = false;
          staging = {};
          let cursor: string | undefined;
          for (let page = 0; page < this.state.pages; page++) {
            const response = await this.pull({ mode: "snapshot", cursor });
            if (!current()) return;
            if (response.error)
              throw new Error(
                response.meta.completeness.state === "complete"
                  ? "snapshot unavailable"
                  : response.meta.completeness.reason,
              );
            for (const i of response.data.items)
              staging[syncKey(i.record.ref)] = i;
            position = response.meta.position;
            nextCursor = response.meta.next_cursor;
            cursor = nextCursor ?? undefined;
            if (!cursor) break;
          }
        }
        const known = new Map(this.known().map((i) => [syncKey(i), i]));
        for (const i of Object.values(staging))
          known.set(syncKey(i.record.ref), {
            kind: i.record.ref.kind,
            id: i.record.ref.id,
          });
        const identities = [...known.values()];
        const chunks: ContextIdentity<string>[][] = [];
        for (let i = 0; i < identities.length; i += 100)
          chunks.push(identities.slice(i, i + 100));
        if (!chunks.length) chunks.push([]);
        let invalidHistory = false;
        let through = position;
        let fixedThrough: string | null = null;
        for (const chunk of chunks) {
          let cursor: string | undefined;
          do {
            const response = await this.pull({
              mode: "changes",
              position,
              through: fixedThrough ?? undefined,
              cursor,
              known: chunk,
            });
            if (!current()) return;
            if (response.error) {
              const reason =
                response.meta.completeness.state === "complete"
                  ? ""
                  : response.meta.completeness.reason;
              if (
                reason === "history_unavailable" ||
                reason === "invalid_cursor"
              ) {
                invalidHistory = true;
                break;
              }
              throw new Error("同期を確認できません。");
            }
            await this.applyPolicy(response.data, staging);
            if (!current()) return;
            for (const i of response.data.items)
              staging[syncKey(i.record.ref)] = i;
            this.state.available ||= response.data.offered.some(
              (r) => !known.has(syncKey(r)),
            );
            fixedThrough ??= response.meta.position;
            through = response.meta.position;
            cursor = response.meta.next_cursor ?? undefined;
          } while (cursor);
          if (invalidHistory) break;
        }
        if (invalidHistory) {
          reset = true;
          continue;
        }
        this.state.items = staging;
        this.state.position = through;
        this.state.nextCursor = nextCursor;
        for (const [key, draft] of Object.entries(this.state.drafts)) {
          const latest = staging[key];
          if (
            !draft.pending &&
            latest &&
            latest.record.ref.revision !== draft.base.record.ref.revision
          )
            draft.latest = structuredClone(latest);
        }
        this.state.confirmedAt = new Date().toISOString();
        this.status = "current";
        if (reconnecting) {
          const drafts = Object.values(this.state.drafts);
          this.notice = drafts.some((d) => d.pending)
            ? "接続を確認しました。保存結果が未確認の要求は、同じ内容で再送してください。"
            : drafts.some((d) => d.latest)
              ? "接続を確認しました。最新内容と下書きを確認してください。"
              : drafts.length
                ? "接続を確認しました。未送信の下書きがあります。"
                : "接続を確認しました。";
        }
        await this.persist();
        this.emit();
        return;
      }
      throw new Error("同期の基準を取得し直してください。");
    } catch (error) {
      if (!current()) return;
      if (authFailure(error)) await this.discard();
      else {
        this.status = "offline";
        this.notice =
          "接続を確認できません。最後に確認したキャッシュを表示しています。";
        this.emit();
      }
    } finally {
      if (current()) {
        this.busy = false;
        this.emit();
      }
    }
  }
  async more() {
    if (this.busy || this.status !== "current") return;
    this.state.pages++;
    await this.refresh(true);
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
    await this.persist();
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
      await this.persist();
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
      await this.persist();
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
      await this.persist();
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
