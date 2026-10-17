import { expect, test } from "vitest";
import { hopperRef } from "./hopper";

test("activation sends a fixed Ref from either record or mutation reply", () => {
  for (const source of [
    { id: "hopper:one", version: "v2", workspace_id: "one" },
    {
      id: "hopper:one",
      version: "v2",
      kind: "definition",
      value: { name: "rule" },
      refs: [],
    },
  ]) {
    expect(
      JSON.parse(
        JSON.stringify({
          definition: hopperRef(source),
          enabled: true,
          controller: "",
        }),
      ),
    ).toEqual({
      definition: { id: "hopper:one", version: "v2" },
      enabled: true,
      controller: "",
    });
  }
});
