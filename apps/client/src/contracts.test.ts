import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";
import saved from "../../../contracts/fixtures-v1.json";
import { decodeResponse, isReference } from "./contracts";
import { clientCapabilities } from "./capabilities";

// The root task passes fresh Go stdout; frontend-only runs use the committed
// copy whose equality to Go output is checked by the Go contract test.
const fixtures = process.env.KOUJOU_CONTRACT_FIXTURES
  ? JSON.parse(readFileSync(process.env.KOUJOU_CONTRACT_FIXTURES, "utf8"))
  : saved;

function fixture(name: string): (typeof saved)[number] {
  const found = fixtures.find(
    (item: (typeof saved)[number]) => item.name === name,
  );
  if (!found) throw new Error(`Missing Go fixture ${name}`);
  return found;
}

describe("Go/TypeScript API boundary", () => {
  it("decodes every Go result without losing version precision", () => {
    for (const item of fixtures as typeof saved) {
      expect(
        decodeResponse(
          item.response,
          isReference,
          item.response.meta.workspace_id,
        ),
      ).toEqual(item.response);
    }
    const result = decodeResponse(
      fixture("read").response,
      isReference,
      "workspace-a",
    );
    if (!result.data) throw new Error("Expected data");
    expect(result.data.version).toBe("9007199254740993");
  });

  it("distinguishes replay, stale version, changed operation and rejected access", () => {
    expect(fixture("replay").response).toEqual(fixture("update").response);
    expect(fixture("replay").request).toEqual(fixture("update").request);
    expect(fixture("version-conflict").response.error?.code).toBe(
      "version_conflict",
    );
    expect(fixture("operation-conflict").response.error?.code).toBe(
      "operation_conflict",
    );
    expect(fixture("workspace-denied").response.error?.code).toBe("forbidden");
    expect(fixture("update-denied").response.error?.code).toBe("forbidden");
  });

  it("preserves partial and pending results instead of treating them as complete", () => {
    expect(fixture("partial").response.meta.completeness).toEqual({
      state: "partial",
      reason: "limit_reached",
    });
    expect(fixture("partial").response.meta.next_cursor).toBe("opaque-page-a");
    expect(fixture("pending").response.meta.completeness).toEqual({
      state: "pending",
      reason: "projection_lag",
    });
  });

  it("rejects malformed, ambiguous and cross-Workspace data", () => {
    const success = fixture("read").response;
    const cases = [
      {},
      { ...success, error: { code: "forbidden" } },
      {
        ...success,
        meta: { ...success.meta, completeness: { state: "partial" } },
      },
      {
        ...success,
        meta: {
          ...success.meta,
          completeness: { state: "complete", reason: "timeout" },
        },
      },
      { ...success, data: { ...success.data, version: 9007199254740992 } },
      { ...success, data: { ...success.data, workspace_id: "workspace-b" } },
      { ...success, meta: { ...success.meta, workspace_id: "workspace-b" } },
    ];
    for (const value of cases)
      expect(() => decodeResponse(value, isReference, "workspace-a")).toThrow();
  });

  it.each([false, true])(
    "does not expose unimplemented execution or UI credentials (native=%s)",
    (native) => {
      expect(clientCapabilities(native)).toEqual({
        surface: native ? "native" : "web",
        localRunner: false,
        credentialAccessFromUI: false,
      });
    },
  );
});
