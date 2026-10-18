import { expect, it } from "vitest";
import { editField, fieldSource, mergeFields } from "./workflow-json";

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
