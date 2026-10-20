import { expect, it } from "vitest";
import {
  editField,
  fieldSource,
  mergeFields,
  selectDefinitionContext,
} from "./workflow-json";

it("returns fixed Context roots to the definition without rewriting command data or existing direct inputs", () => {
  const text =
    '{"name":"draft","spec":{"input":{"command":{"kind":"cli","parameters":{"future":123456789012345678901234567890}}}},"selection":{"mode":"select","direct":[{"future":123456789012345678901234567890}],"external":[]}}';
  const selection = {
    mode: "select" as const,
    include: [{ kind: "document", id: "selected", revision: "fixed-doc" }],
    exclude: [{ kind: "paragraph", id: "excluded", revision: "fixed-part" }],
  };
  const changed = selectDefinitionContext(text, selection);
  expect(JSON.parse(changed).selection).toMatchObject(selection);
  expect(fieldSource(changed, ["spec"]).replace(/\s/g, "")).toBe(
    fieldSource(text, ["spec"]).replace(/\s/g, ""),
  );
  expect(fieldSource(changed, ["selection", "direct"]).replace(/\s/g, "")).toBe(
    fieldSource(text, ["selection", "direct"]).replace(/\s/g, ""),
  );
  expect(fieldSource(changed, ["selection", "external"])).toBe("[]");
});
it("returns explicit none as an empty Context specification and keeps unrelated definition edits", () => {
  const text =
    '{"name":"unsent name","selection":{"mode":"select","include":[{"kind":"sentence","id":"old","revision":"fixed"}],"direct":[{"text":"old"}],"external":[]},"spec":{"input":{"command":{"parameters":123456789012345678901234567890}}}}';
  const changed = selectDefinitionContext(text, {
    mode: "none",
    include: [],
    exclude: [],
  });
  expect(JSON.parse(changed).selection).toEqual({ mode: "none" });
  expect(fieldSource(changed, ["name"])).toBe('"unsent name"');
  expect(fieldSource(changed, ["spec"])).toBe(fieldSource(text, ["spec"]));
});

it("edits one GUI field without changing large input numbers or other nodes", () => {
  const text =
    '{"name":"old","spec":{"input":{"context":123456789012345678901234567890}},"nodes":[{"id":"first"},{"id":"second"}]}';
  const changed = editField(text, ["name"], '"new"');
  expect(fieldSource(changed, ["spec", "input", "context"])).toBe(
    "123456789012345678901234567890",
  );
  const node = editField(changed, ["nodes", 1, "id"], '"updated"');
  expect(fieldSource(node, ["nodes", 0, "id"])).toBe('"first"');
  expect(fieldSource(node, ["nodes", 1, "id"])).toBe('"updated"');
  expect(
    editField(
      node,
      ["spec", "input", "prompt"],
      '{"number":123456789012345678901234567890}',
    ),
  ).toContain('"number":123456789012345678901234567890');
  expect(() => editField(node, ["name"], '{"bad":}')).toThrow("構文");
});

it("returns a partial generated proposal to a complete draft without losing omitted fixed values", () => {
  const base =
    '{"schema":"run-definition-v1","name":"manual","spec":{"input":{"prompt":"keep","command":{"kind":"cli","target":"codex","parameters":[123456789012345678901234567890]}}}}';
  const merged = mergeFields(
    base,
    '{"spec":{"input":{"command":{"target":"claude"}}}}',
  );
  expect(fieldSource(merged, ["name"])).toBe('"manual"');
  expect(fieldSource(merged, ["spec", "input", "prompt"])).toBe('"keep"');
  expect(fieldSource(merged, ["spec", "input", "command", "parameters"])).toBe(
    "[123456789012345678901234567890]",
  );
  expect(fieldSource(merged, ["spec", "input", "command", "target"])).toBe(
    '"claude"',
  );
});
