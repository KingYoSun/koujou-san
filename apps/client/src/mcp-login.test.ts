import { afterEach, expect, it, vi } from "vitest";
import { invoke } from "@tauri-apps/api/core";
import { connectMCP } from "./auth";

vi.mock("@tauri-apps/api/core", () => ({
  invoke: vi.fn(async () => undefined),
}));
afterEach(() => {
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});

it("opens the dedicated MCP login from the Web client", async () => {
  const open = vi.fn();
  vi.stubGlobal("window", { open });
  await connectMCP(false);
  expect(open).toHaveBeenCalledWith(
    "/auth/mcp/start",
    "koujou-mcp-login",
    "popup,width=700,height=760",
  );
  expect(invoke).not.toHaveBeenCalled();
});

it("uses native MCP authentication on desktop", async () => {
  await connectMCP(true);
  expect(invoke).toHaveBeenCalledWith("mcp_login");
});
