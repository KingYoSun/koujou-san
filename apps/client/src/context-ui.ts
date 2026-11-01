import { createStore } from "zustand/vanilla";
import { syncRequest } from "./auth";
import { SyncHTTPError } from "./sync-error";
import type {
  ContextRef,
  ContextIdentity,
  CandidateEvidence,
  ContextRecord,
} from "./context-contract";
import type { SyncEngine } from "./sync-engine";
export type Ref = ContextRef<string>;
export type Identity = ContextIdentity<string>;
export type Candidate = {
  ref: Ref;
  name: string;
  scope_kind?: string;
  parent?: Identity;
};
export type Meta = {
  position: string | null;
  next_cursor?: string | null;
  completeness: { state: string; reason?: string };
};
export type Envelope<T> = { data?: T; meta: Meta; error?: { code: string } };
export type CandidatePage = { items: Candidate[]; evidence: CandidateEvidence };
export type GraphRequest = {
  root: Ref;
  direction: string;
  kinds?: string[];
  history?: boolean;
  depth?: number;
  limit: number;
  cursor?: string;
};
export type GraphEdge = {
  id: string;
  kind: string;
  from: Ref;
  to: Ref;
  source: Ref;
  order: number;
};
export type GraphPage = {
  items: {
    node?: Ref & { form: string; current: boolean };
    edge?: GraphEdge;
  }[];
  unexplored: string[];
  evidence: CandidateEvidence;
};
export const kinds = ["sentence", "paragraph", "section", "document"];
export const scopeKinds = ["product", "project", "milestone", "issue"];
export const labels: Record<string, string> = {
  sentence: "文",
  paragraph: "段落",
  section: "節",
  document: "文書",
  scope: "Scope",
  membership: "所属",
  product: "Product",
  project: "Project",
  milestone: "Milestone",
  issue: "Issue",
};
export const refKey = (r: Ref) => `${r.kind}:${r.id}@${r.revision}`;
export const fixedRef = (r: Ref): Ref => ({
  kind: r.kind,
  id: r.id,
  revision: r.revision,
});
export const identityKey = (r: Identity) => `${r.kind}:${r.id}`;
export const identityLabel = (r: Identity) =>
  `${labels[r.kind] ?? r.kind} · ${r.id}`;
export const refLabel = (r: Ref) => `${identityLabel(r)} · ${r.revision}`;
export const contextPath = (workspace: string, r: Identity) =>
  `/contexts/workspaces/${encodeURIComponent(workspace)}/${encodeURIComponent(r.kind)}/${encodeURIComponent(r.id)}`;
export type UIState = {
  permissionEpoch: number;
  selected?: Identity;
  historical?: Ref;
  reference?: Candidate;
  include: Ref[];
  exclude: Ref[];
  mode: "select" | "none";
  choose: (identity: Identity) => void;
  add: (ref: Ref, excluded?: boolean) => void;
};
export function editorStore() {
  return createStore<UIState>((set) => ({
    permissionEpoch: 0,
    include: [],
    exclude: [],
    mode: "select",
    choose: (selected) =>
      set({
        selected: { kind: selected.kind, id: selected.id },
        historical: undefined,
      }),
    add: (ref, excluded = false) =>
      set((state) => ({
        [excluded ? "exclude" : "include"]: [
          ...state[excluded ? "exclude" : "include"],
          fixedRef(ref),
        ],
        mode: "select",
      })),
  }));
}
export type EditorStore = ReturnType<typeof editorStore>;
export type UI = {
  native: boolean;
  workspace: string;
  user: string;
  api: string;
  engine: SyncEngine;
  store: EditorStore;
  canCreate: boolean;
  prefix: readonly string[];
  request: <T>(path: string, body?: unknown) => Promise<Envelope<T>>;
  fail: (error: unknown) => void;
};
export function requester(native: boolean) {
  return async <T>(path: string, body?: unknown): Promise<Envelope<T>> => {
    try {
      return (await syncRequest(path, native, {
        method: body === undefined ? "GET" : "POST",
        body,
      })) as Envelope<T>;
    } catch (error) {
      if (
        error instanceof SyncHTTPError &&
        error.status !== 401 &&
        error.status !== 403 &&
        error.response &&
        typeof error.response === "object" &&
        "meta" in error.response
      )
        return error.response as Envelope<T>;
      throw error;
    }
  };
}
export function resultData<T>(value: Envelope<T>): T {
  if (value.error || !value.data)
    throw new Error(
      value.meta.completeness.reason ??
        value.error?.code ??
        "取得結果を確認できません。",
    );
  return value.data;
}
export const recordName = (record: ContextRecord<Record<string, unknown>>) =>
  String(
    record.value.title ||
      record.value.name ||
      record.value.text ||
      record.ref.id,
  ).slice(0, 160);
