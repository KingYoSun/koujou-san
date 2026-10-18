import { mcpRequest, syncRequest } from "./auth";
import { allowAnswerScope, answerScopeEpoch } from "./sync-store";
import {
  modelRequest,
  type ModelProfile,
  type VersionRef,
} from "./model-connections";
import type { HostPort, Saved } from "./client-selection-host";

export const operationClientID = crypto.randomUUID();
export type OperationAddress = {
  native: boolean;
  api: string;
  user: string;
  workspace: string;
};

// Manual and automatic operations use the same transport, receipts and epoch.
export function operationPort(
  ui: OperationAddress,
  profile: ModelProfile,
  operation: VersionRef,
  alive: () => boolean,
): HostPort {
  const { native, api, user, workspace } = ui;
  const namespace = { api, user, workspace },
    permission = answerScopeEpoch(namespace);
  const active = () => alive() && allowAnswerScope(namespace, permission);
  const base = `/client-operations/workspaces/${encodeURIComponent(workspace)}`;
  const call = async <T>(path: string, body?: unknown) =>
    syncRequest(base + path, native, {
      method: body === undefined ? "GET" : "POST",
      body,
    }) as Promise<T>;
  let handle: string | undefined;
  return {
    active,
    read: (ref) =>
      call(
        `/records/${encodeURIComponent(ref.id)}?version=${encodeURIComponent(ref.version)}`,
      ),
    query: async (root, kind) => {
      const rows: Saved[] = [];
      let cursor = { after: 0, after_id: "" };
      for (;;) {
        if (!active()) return rows;
        const page = await call<{
          items: Saved[];
          after: number;
          after_id: string;
        }>("/query", { root, kind, limit: 100, ...cursor });
        rows.push(...page.items);
        if (page.items.length < 100) return rows;
        cursor = { after: page.after, after_id: page.after_id };
      }
    },
    write: async (id, action, version, value, mutation) => {
      if (!active()) throw Error("operation_scope_changed");
      const result = await call<{ data?: VersionRef; error?: unknown }>(
        `/operations/${encodeURIComponent(id)}/${action}`,
        { operation_id: mutation, expected_version: version, ...value },
      );
      if (!result.data || result.error) throw Error("operation_not_accepted");
      return { id: result.data.id, version: result.data.version };
    },
    rpc: async (body) => {
      if (!active()) throw Error("operation_scope_changed");
      handle ??= (
        await call<{ handle: string }>(
          `/operations/${encodeURIComponent(operation.id)}/transport`,
        )
      ).handle;
      return mcpRequest(native, workspace, handle, body, operationClientID);
    },
    model: (body, timeout) => {
      if (!active()) throw Error("operation_scope_changed");
      return modelRequest(native, api + "\0" + user, profile, body, timeout);
    },
  };
}
