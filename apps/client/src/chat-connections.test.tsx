// @vitest-environment happy-dom
import { act } from "react";
import { createRoot } from "react-dom/client";
import { expect, it, vi } from "vitest";
import { invoke } from "@tauri-apps/api/core";
import { mcpIdentity } from "./auth";
import { useChatConnections } from "./chat-connections";

vi.mock("@tauri-apps/api/core", () => ({ invoke: vi.fn() }));
vi.mock("./model-connections", async (original) => ({
  ...(await original<typeof import("./model-connections")>()),
  loadProfiles: vi.fn(async () => []),
}));

it("distinguishes missing authentication from HTTP and native transport failures", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => new Response("{}", { status: 503 })),
  );
  try {
    await expect(mcpIdentity(false)).rejects.toThrow();
    vi.mocked(fetch).mockResolvedValue(new Response("{}", { status: 401 }));
    await expect(mcpIdentity(false)).resolves.toBeUndefined();
    vi.mocked(invoke).mockRejectedValue("authentication_required");
    await expect(mcpIdentity(true)).resolves.toBeUndefined();
    vi.mocked(invoke).mockRejectedValue("api_unavailable");
    await expect(mcpIdentity(true)).rejects.toBe("api_unavailable");
  } finally {
    vi.unstubAllGlobals();
  }
});

it("shows checking and a confirmation failure separately from unauthenticated and connected", async () => {
  Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
  let respond!: (value: Response) => void;
  vi.stubGlobal(
    "fetch",
    vi.fn(
      () =>
        new Promise<Response>((resolve) => {
          respond = resolve;
        }),
    ),
  );
  function Status() {
    const { mcp, checkMCP } = useChatConnections(
      { native: false, api: "api", user: "alice", workspace: "w" },
      true,
      () => {},
    );
    return (
      <>
        <output>{mcp}</output>
        <button onClick={() => void checkMCP()}>確認</button>
      </>
    );
  }
  const host = document.createElement("div"),
    root = createRoot(host);
  try {
    await act(() => root.render(<Status />));
    expect(host.textContent).toContain("確認中");
    await act(async () =>
      respond(new Response(JSON.stringify({ user: { id: "alice" } }))),
    );
    expect(host.textContent).toContain("認証済み");
    await act(() => host.querySelector("button")!.click());
    expect(host.textContent).toContain("確認中");
    await act(async () => respond(new Response("{}", { status: 503 })));
    expect(host.textContent).toContain("確認失敗");
    await act(() => host.querySelector("button")!.click());
    await act(async () => respond(new Response("{}", { status: 401 })));
    expect(host.textContent).toContain("未接続");
  } finally {
    await act(() => root.unmount());
    vi.unstubAllGlobals();
    localStorage.clear();
  }
});
