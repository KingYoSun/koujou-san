import { readFileSync } from "node:fs";
import { expect, it } from "vitest";
import {
  decodeContextResolution,
  verifyContextInput,
} from "./context-contract";

const saved = JSON.parse(
  readFileSync(
    new URL("../../../contracts/context-fixtures-v1.json", import.meta.url),
    "utf8",
  ),
);
const fixtures = process.env.KOUJOU_CONTEXT_FIXTURES
  ? JSON.parse(readFileSync(process.env.KOUJOU_CONTEXT_FIXTURES, "utf8"))
  : saved;

it("reconstructs exact structured input and provenance from the Go fixture", async () => {
  expect(fixtures).toEqual(saved);
  for (const value of Object.values(fixtures)) {
    const result = decodeContextResolution(value, "workspace-fixture");
    if (result.manifest)
      expect(await verifyContextInput(result.manifest)).toBe(true);
  }
  const normal = decodeContextResolution(
    fixtures.normal,
    "workspace-fixture",
  ).manifest!;
  const doc = JSON.parse(normal.input_utf8!).contexts[0];
  expect(doc.title).toBe("製品方針");
  expect(doc.children[0].attachments[0]).toEqual({
    uri: "fixture://code",
    revision: "code-v1",
    range: "lines:1-2",
    digest: expect.any(String),
  });
  expect(doc.children[0].children[0].children[0].text).toBe("共有の原文。");
  expect(doc.children[0].children[1].children[0].text).toBe("共有の原文。");
  expect(normal.placements).toHaveLength(7);
  expect(
    normal.placements.find((p) => p.ref.id === "9007199254740993")?.ref
      .revision,
  ).toBe("9007199254740994");
  expect(normal.duplicates).toHaveLength(2);
  const output = decodeContextResolution(
    fixtures.output,
    "workspace-fixture",
  ).manifest!;
  expect(output.selection.direct?.[0].origin?.revision).toBe("o1");
  expect(JSON.parse(output.input_utf8!).direct[0]).toEqual({
    result: "原出力",
  });
  expect(output.read_status).toBe("unobserved");
  const excerpt = decodeContextResolution(
    fixtures.excerpt,
    "workspace-fixture",
  ).manifest!;
  expect(excerpt.input_utf8).not.toContain("共有の原文。");
  expect(excerpt.placements).toHaveLength(4);
});

it("rejects cross-Workspace, mutable refs, false completeness and modified input", async () => {
  expect(() => decodeContextResolution(fixtures.normal, "foreign")).toThrow();
  const revision = structuredClone(fixtures.normal);
  revision.manifest.placements[0].ref.revision = 3;
  expect(() =>
    decodeContextResolution(revision, "workspace-fixture"),
  ).toThrow();
  const mutable = structuredClone(fixtures.normal);
  mutable.manifest.selection.include[0].revision = "head";
  expect(() => decodeContextResolution(mutable, "workspace-fixture")).toThrow();
  const partial = structuredClone(fixtures["partial-candidates"]);
  partial.manifest = fixtures.normal.manifest;
  expect(() => decodeContextResolution(partial, "workspace-fixture")).toThrow();
  const changed = structuredClone(fixtures.normal.manifest);
  changed.input_utf8 = changed.input_utf8.replace("共有の原文。", "別の原文。");
  expect(await verifyContextInput(changed)).toBe(false);
});
