import { describe, expect, it, vi } from "vitest";
import {
  initialMessages,
  runSelection,
  selectionInput,
  toolCalls,
  type ClientOperation,
  type HostPort,
  type Saved,
  type SelectionPolicy,
} from "./client-selection-host";
import type { ModelHTTP, ModelProfile } from "./model-connections";
const policy: SelectionPolicy = {
  mode: "auto",
  fixed: { mode: "none", exclude: [] },
  scopes: [],
  limits: { model_requests: 32, mcp_requests: 256, seconds: 600 },
};
const profile: ModelProfile = {
  id: "owned",
  name: "owned",
  endpoint: "https://gateway.example/v1",
  model: "fixed",
  allowed_models: ["fixed"],
  auth: "none",
  revision: "v1",
  contract: "",
  prices: {},
  valuation: "estimated",
};
function fixture(
  beginAckLost = false,
  finishAckLost = false,
  anotherIntent = false,
  purpose = "selection",
) {
  let revision = 1;
  let op: Saved<ClientOperation> = {
    id: "client-operation:owned",
    version: "v1",
    root: "client-operation:owned",
    position: 1,
    kind: "operation",
    refs: [],
    value: {
      policy: { id: "policy", version: "v1" },
      target: { id: "selection", version: "0" },
      goal: "owned goal",
      purpose,
      model: {
        connection: "owned",
        revision: "v1",
        requested: "fixed",
        allowed: ["fixed"],
      },
      client: "client",
      user: "alice",
      state: "ready",
    },
  };
  const records = new Map<string, Saved>();
  records.set(op.id, op);
  const copy = <T>(v: unknown) => JSON.parse(JSON.stringify(v)) as T;
  const write = vi.fn(
    async (
      _id: string,
      action: "begin" | "finish" | "actions",
      _version: string,
      value: Record<string, unknown>,
      mutation: string,
    ) => {
      op = copy(op);
      op.version = `v${++revision}`;
      if (action === "begin") {
        op.value.state = "sending";
        op.value.turn = { id: "client-turn:owned", version: "intent" };
        records.set("client-turn:owned", {
          id: "client-turn:owned",
          version: "intent",
          kind: "turn",
          root: op.id,
          position: 2,
          refs: [],
          value: {
            state: "intent",
            request_id: anotherIntent ? "another-request" : mutation,
            timeout_ms: 10000,
          },
        });
      } else if (action === "finish") {
        const reply = value.response as {
          choices?: { message?: { tool_calls?: unknown[] } }[];
        };
        op.value.state = value.failure
          ? "failed"
          : reply?.choices?.[0]?.message?.tool_calls?.length
            ? "ready"
            : "applied";
        op.value.turn = { id: "client-turn:owned", version: "response" };
        records.set("client-turn:owned", {
          id: "client-turn:owned",
          version: "response",
          kind: "turn",
          root: op.id,
          position: 3,
          refs: [],
          value: { state: "response", response: value.response },
        });
      } else {
        op.value.state = "incomplete";
        op.value.reason = String(value.reason);
      }
      records.set(op.id, op);
      if (action === "begin" && beginAckLost) throw new Error("lost begin ACK");
      if (action === "finish" && finishAckLost)
        throw new Error("lost finish ACK");
      return { id: op.id, version: op.version };
    },
  );
  const model = vi.fn(async (): Promise<ModelHTTP> => ({
    status: 200,
    body: {
      id: "owned-result",
      model: "fixed",
      choices: [
        {
          message: {
            role: "assistant",
            content: JSON.stringify({ items: [], questions: [], unknown: [] }),
          },
          finish_reason: "stop",
        },
      ],
      usage: { prompt_tokens: 10, completion_tokens: 2 },
    },
  }));
  const rpc = vi.fn();
  const port: HostPort = {
    read: async <T>(ref: { id: string; version: string }) =>
      copy<Saved<T>>(records.get(ref.id)),
    query: async () => [],
    write,
    rpc,
    model,
    active: () => true,
  };
  return {
    start: copy<Saved<ClientOperation>>(op),
    port,
    model,
    rpc,
    write,
    setPending: () => {
      op.value.state = "sending";
      records.set(op.id, op);
    },
  };
}
describe("client selection Host", () => {
  it("uses the shared Host for definition generation while none exposes only metadata tools", async () => {
    const f = fixture(false, false, false, "definition");
    const names = ["workflow_get", "workflow_query", "workflow_capabilities"];
    f.rpc.mockImplementation(async (wire: { method: string }) => ({
      status: 200,
      body: {
        result:
          wire.method === "tools/list"
            ? {
                tools: [...names, "context_get", "context_edit"].map(
                  (name) => ({ name, inputSchema: { type: "object" } }),
                ),
              }
            : {},
      },
    }));
    await runSelection(
      f.start,
      {
        ...policy,
        definition: {
          fixed_paths: [],
          sources: [{ id: "runner:owned", version: "v1" }],
        },
      },
      profile,
      "client",
      f.port,
      () => {},
    );
    expect(f.model).toHaveBeenCalledTimes(1);
    const body = f.port.model === f.model ? f.model.mock.calls[0] : undefined;
    expect(body).toBeDefined();
    const input = (
      f.model.mock.calls as unknown as [Record<string, unknown>][]
    )[0][0];
    expect(
      (input.tools as { function: { name: string } }[]).map(
        (t) => t.function.name,
      ),
    ).toEqual(names);
    expect(input.response_format).toMatchObject({
      json_schema: {
        name: "run_definition",
        schema: {
          required: ["definition_utf8", "reason", "questions", "unknown"],
        },
      },
    });
    expect(f.rpc.mock.calls.map((c) => c[0].method)).toEqual([
      "server/discover",
      "tools/list",
    ]);
    const privatePolicy: SelectionPolicy = {
      ...policy,
      definition: {
        sources: [],
        fixed_paths: ["/spec/input/command/parameters"],
        fixed_values: {
          "/spec/input/command/parameters": '["private-fixed-argument"]',
        },
      },
    };
    expect(
      JSON.stringify(
        initialMessages(
          { ...f.start.value, purpose: "definition" },
          privatePolicy,
        ),
      ),
    ).not.toContain("private-fixed-argument");
  });
  it("does not dispatch a Context tool invented by a definition model under none", async () => {
    const f = fixture(false, false, false, "definition");
    f.rpc.mockResolvedValue({
      status: 200,
      body: {
        result: {
          tools: [
            "workflow_get",
            "workflow_query",
            "workflow_capabilities",
          ].map((name) => ({ name, inputSchema: { type: "object" } })),
        },
      },
    });
    f.model.mockResolvedValueOnce({
      status: 200,
      body: {
        model: "fixed",
        choices: [
          {
            message: {
              role: "assistant",
              tool_calls: [
                {
                  id: "forbidden",
                  type: "function",
                  function: { name: "context_get", arguments: "{}" },
                },
              ],
            },
            finish_reason: "tool_calls",
          },
        ],
      },
    });
    await expect(
      runSelection(
        f.start,
        { ...policy, definition: { fixed_paths: [], sources: [] } },
        profile,
        "client",
        f.port,
        () => {},
      ),
    ).rejects.toThrow("tool_not_allowed");
    const result = await f.port.read<ClientOperation>({
      id: f.start.id,
      version: "head",
    });
    expect(result.value.state).toBe("incomplete");
    expect(f.rpc.mock.calls.some((c) => c[0].method === "tools/call")).toBe(
      false,
    );
  });
  it("does not carry previous selections into an explicit none save", () => {
    const ref = { kind: "sentence", id: "previous", revision: "v1" };
    expect(selectionInput("none", [ref], [ref])).toEqual({
      mode: "none",
      include: [],
      exclude: [],
    });
  });
  it("uses stateless MCP metadata and continues a normal authorized search page", async () => {
    const f = fixture();
    f.rpc.mockImplementation(
      async (wire: { method: string; params: Record<string, unknown> }) => {
        expect(wire.params._meta).toEqual({
          "io.modelcontextprotocol/protocolVersion": "2026-07-28",
          "io.modelcontextprotocol/clientInfo": {
            name: "koujou-san-client",
            version: "1",
          },
          "io.modelcontextprotocol/clientCapabilities": {},
        });
        if (wire.method === "server/discover")
          return { status: 200, body: { result: {} } };
        if (wire.method === "tools/list")
          return {
            status: 200,
            body: {
              result: {
                tools: [
                  "context_search",
                  "context_get",
                  "context_graph",
                  "context_resolve",
                ].map((name) => ({ name, inputSchema: { type: "object" } })),
              },
            },
          };
        return {
          status: 200,
          body: {
            result: {
              content: [
                {
                  type: "text",
                  text: JSON.stringify({
                    data: { items: [] },
                    meta: {
                      next_cursor: "owned-page",
                      completeness: {
                        state: "partial",
                        reason: "limit_reached",
                      },
                    },
                  }),
                },
              ],
            },
          },
        };
      },
    );
    f.model.mockResolvedValueOnce({
      status: 200,
      body: {
        model: "fixed",
        choices: [
          {
            message: {
              role: "assistant",
              content: null,
              tool_calls: [
                {
                  id: "search",
                  type: "function",
                  function: {
                    name: "context_search",
                    arguments: '{"limit":2}',
                  },
                },
              ],
            },
            finish_reason: "tool_calls",
          },
        ],
        usage: { prompt_tokens: 1, completion_tokens: 1 },
      },
    });
    const result = await runSelection(
      f.start,
      { ...policy, fixed: { mode: "select" } },
      profile,
      "client",
      f.port,
      () => {},
    );
    expect(result.value.state).toBe("applied");
    expect(f.model).toHaveBeenCalledTimes(2);
    expect(f.rpc.mock.calls.map((c) => c[0].method)).toEqual([
      "server/discover",
      "tools/list",
      "tools/call",
    ]);
  });
  it("sends only the explicit goal and positive fixed refs, without hidden cache or exclusions", () => {
    const f = fixture();
    const op = { ...f.start.value, implementation_dialogue: "FORBIDDEN_IMPL" };
    const messages = initialMessages(op, {
      ...policy,
      fixed: {
        mode: "select",
        include: [
          {
            kind: "sentence",
            id: "allowed",
            revision: "v1",
            text: "FORBIDDEN_EXTRA",
          } as never,
        ],
        exclude: [
          { kind: "sentence", id: "FORBIDDEN_EXCLUSION", revision: "v1" },
        ],
      },
    });
    expect(JSON.stringify(messages)).toContain("allowed");
    for (const value of [
      "FORBIDDEN_IMPL",
      "FORBIDDEN_EXTRA",
      "FORBIDDEN_EXCLUSION",
    ])
      expect(JSON.stringify(messages)).not.toContain(value);
  });
  it("uses neither MCP nor Runs for none, and resolves lost ACKs without another model request", async () => {
    const f = fixture(true, true);
    const changed = vi.fn();
    const result = await runSelection(
      f.start,
      policy,
      profile,
      "client",
      f.port,
      changed,
    );
    expect(result.value.state).toBe("applied");
    expect(f.model).toHaveBeenCalledTimes(1);
    expect(f.rpc).not.toHaveBeenCalled();
    expect(f.write.mock.calls.filter((c) => c[1] === "begin")).toHaveLength(1);
    expect(f.write.mock.calls.filter((c) => c[1] === "finish")).toHaveLength(1);
  });
  it("does not assume a different in-flight intent is its own after a failed reservation", async () => {
    const f = fixture(true, false, true);
    await expect(
      runSelection(f.start, policy, profile, "client", f.port, () => {}),
    ).rejects.toThrow();
    expect(f.model).not.toHaveBeenCalled();
  });
  it("holds a pending request after reconnect without sending again", async () => {
    const f = fixture();
    f.setPending();
    const result = await runSelection(
      f.start,
      policy,
      profile,
      "client",
      f.port,
      () => {},
    );
    expect(result.value.state).toBe("sending");
    expect(f.model).not.toHaveBeenCalled();
    expect(f.write).not.toHaveBeenCalled();
  });
  it("never invokes a provider-supplied mutation or Run tool", () => {
    expect(() =>
      toolCalls({
        role: "assistant",
        tool_calls: [
          {
            id: "one",
            type: "function",
            function: { name: "run_start", arguments: "{}" },
          },
        ],
      }),
    ).toThrow("tool_not_allowed");
    expect(() =>
      toolCalls({
        role: "assistant",
        tool_calls: [
          {
            id: "one",
            type: "function",
            function: { name: "context_get", arguments: "[]" },
          },
        ],
      }),
    ).toThrow("invalid_tool_arguments");
  });
});
