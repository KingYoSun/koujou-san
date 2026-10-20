import { expect, it } from "vitest";
import {
  sourceKey,
  sourceRoute,
  pointerPath,
  pathPointer,
} from "./context-generation-source";
import { readRoute, routeHref } from "./navigation";
const source = {
  run: { id: "run:one", version: "90071992547409931234" },
  output: { id: "run-output:one:2", version: "fixed-output" },
  path: ["項目/一覧", "a~b", "0", ""],
};
it("preserves exact output identity and JSON pointer segments across navigation", () => {
  const route = readRoute(
    routeHref(sourceRoute("workspace", source)),
    "workspace",
  );
  expect(pointerPath(route.query!)).toEqual(source.path);
  expect(pathPointer(pointerPath(route.query!))).toBe(route.query);
  expect(route.origin?.version).toBe(source.run.version);
  expect(route.target?.version).toBe(source.output.version);
});
it("separates proposals from other Runs, versions, outputs and selected paths", () => {
  expect(sourceKey({ ...source, path: [...source.path] })).toBe(
    sourceKey(source),
  );
  for (const next of [
    { ...source, run: { ...source.run, id: "run:other" } },
    { ...source, run: { ...source.run, version: "other" } },
    { ...source, output: { ...source.output, id: "run-output:other:2" } },
    { ...source, output: { ...source.output, version: "other" } },
    { ...source, path: ["項目", "一覧/a~b", "0", ""] },
  ])
    expect(sourceKey(next)).not.toBe(sourceKey(source));
});
