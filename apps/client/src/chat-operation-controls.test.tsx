import { renderToStaticMarkup } from "react-dom/server";
import { expect, it } from "vitest";
import { ChatOperationControls } from "./chat-operation-controls";
import type { ClientOperation } from "./client-selection-host";

const operation = (state: string, client = "here"): ClientOperation => ({
  state,
  client,
  user: "alice",
  purpose: "chat",
  goal: "相談",
  policy: { id: "policy", version: "fixed" },
  target: { id: "thread", version: "fixed" },
  model: {
    connection: "model",
    revision: "fixed",
    requested: "model",
    allowed: ["model"],
  },
});
const render = (
  state: string,
  client = "here",
  user = "alice",
  modelMatches = true,
) =>
  renderToStaticMarkup(
    <ChatOperationControls
      operation={operation(state, client)}
      client="here"
      user={user}
      modelMatches={modelMatches}
      busy={false}
      onAction={() => {}}
    />,
  );
it("offers explicit recovery for failed, incomplete and ready chat operations", () => {
  for (const state of ["failed", "incomplete"]) {
    expect(render(state)).toContain("相談を再開");
    expect(render(state)).toContain("この相談操作を終了");
  }
  expect(render("ready")).toContain("相談を続ける");
  expect(render("sending", "other")).toContain("このクライアントへ引き継ぐ");
  expect(render("sending", "other")).not.toContain("相談を続ける");
  expect(render("failed", "other")).toContain("disabled");
  expect(render("ready", "here", "alice", false)).toContain("disabled");
});
it("does not offer mutation of another user's or completed operation", () => {
  expect(render("failed", "here", "bob")).toBe("");
  expect(render("applied")).toBe("");
  expect(render("cancelled")).toBe("");
});
