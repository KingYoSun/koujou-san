import type { GenerationPolicy } from "./client-selection-host";
import type { Route } from "./navigation";
export type GenerationSource = GenerationPolicy["source"];
export const sourceKey = (source?: GenerationSource) =>
  JSON.stringify(
    source && [
      source.run.id,
      source.run.version,
      source.output.id,
      source.output.version,
      source.path ?? [],
    ],
  );
export const pointerPath = (pointer: string) =>
  pointer === ""
    ? []
    : pointer
        .replace(/^\//, "")
        .split("/")
        .map((p) => p.replace(/~1/g, "/").replace(/~0/g, "~"));
export const pathPointer = (path: readonly string[]) =>
  path.length
    ? "/" +
      path.map((p) => p.replace(/~/g, "~0").replace(/\//g, "~1")).join("/")
    : "";
export function sourceRoute(
  workspace: string,
  source: GenerationSource,
): Route {
  return {
    workspace,
    screen: "context",
    view: "generation",
    target: {
      kind: "output",
      id: source.output.id,
      version: source.output.version,
    },
    origin: { kind: "run", id: source.run.id, version: source.run.version },
    query: pathPointer(source.path ?? []) || undefined,
  };
}
