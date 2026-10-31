import { syncRequest } from "./auth";
import type { VersionRef } from "./model-connections";
import type { Saved } from "./client-selection-host";

export type ChatThread = { name: string; project: VersionRef };
export type ChatMessage = {
  thread: VersionRef;
  role: "human" | "assistant";
  text: string;
  user?: string;
  state: string;
  operation?: VersionRef;
};
export type ChatLink = {
  thread: VersionRef;
  kind: string;
  target: VersionRef;
  name: string;
};
export type ChatAdoption = {
  message: VersionRef;
  start_utf8: number;
  end_utf8: number;
  context: { kind: string; id: string; revision: string };
};
export type ChatPage = { items: Saved[]; after: number; after_id: string };
export const chatBase = (workspace: string) =>
  `/client-operations/workspaces/${encodeURIComponent(workspace)}`;
export const pin = (ref: VersionRef): VersionRef => ({
  id: ref.id,
  version: ref.version,
});
export const utf8Range = (text: string, start: number, end: number) => ({
  start_utf8: new TextEncoder().encode(text.slice(0, start)).length,
  end_utf8: new TextEncoder().encode(text.slice(0, end)).length,
});

export async function linkChatTarget(
  native: boolean,
  workspace: string,
  thread: VersionRef,
  kind: string,
  target: VersionRef,
) {
  const id = `chat-link:${thread.id}:${target.id}:${target.version}`;
  const result = (await syncRequest(
    `${chatBase(workspace)}/chat/links/${encodeURIComponent(id)}`,
    native,
    {
      method: "POST",
      body: {
        operation_id: id,
        expected_version: "0",
        value: { thread: pin(thread), target: pin(target), kind, name: "" },
      },
    },
  )) as { data?: VersionRef };
  if (!result.data) throw Error("chat_link_not_accepted");
}
