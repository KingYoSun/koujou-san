import {
  applyEdits,
  findNodeAtLocation,
  modify,
  parseTree,
} from "jsonc-parser";
import type { JSONPath, ParseError, Node } from "jsonc-parser";

const tree = (text: string) => {
  const errors: ParseError[] = [];
  const root = parseTree(text, errors, {
    disallowComments: true,
    allowTrailingComma: false,
  });
  if (!root || errors.length) throw Error("JSONの構文を確認してください。");
  return root;
};
export function fieldSource(text: string, path: JSONPath): string {
  return fieldSources(text, [path])[0];
}
export function fieldSources(text: string, paths: JSONPath[]): string[] {
  if (!paths.length) return [];
  const root = tree(text);
  return paths.map((path) => {
    const node = findNodeAtLocation(root, path);
    return node ? text.slice(node.offset, node.offset + node.length) : "";
  });
}
export function editField(
  text: string,
  path: JSONPath,
  value: string | undefined,
): string {
  tree(text);
  const next = applyEdits(
    text,
    modify(text, path, value === undefined ? undefined : null, {
      formattingOptions: { insertSpaces: true, tabSize: 2 },
    }),
  );
  if (value === undefined) return next;
  tree(value);
  const node = findNodeAtLocation(tree(next), path)!;
  const changed =
    next.slice(0, node.offset) + value + next.slice(node.offset + node.length);
  tree(changed);
  return changed;
}
export function nodeCount(text: string): number {
  return findNodeAtLocation(tree(text), ["nodes"])?.children?.length ?? 0;
}

export const applyFields = (
  text: string,
  fields: Record<string, string | null>,
) =>
  Object.entries(fields).reduce(
    (source, [path, value]) =>
      editField(source, JSON.parse(path) as JSONPath, value ?? undefined),
    text,
  );

export function mergeFields(base: string, changes: string): string {
  const source = tree(changes);
  if (source.type !== "object" || tree(base).type !== "object")
    throw Error("定義のJSON objectを指定してください。");
  let merged = base;
  const visit = (node: Node, path: JSONPath) => {
    if (
      node.type === "object" &&
      findNodeAtLocation(tree(merged), path)?.type === "object"
    ) {
      for (const property of node.children ?? [])
        visit(property.children![1], [
          ...path,
          property.children![0].value as string,
        ]);
    } else
      merged = editField(
        merged,
        path,
        changes.slice(node.offset, node.offset + node.length),
      );
  };
  visit(source, []);
  return merged;
}
export function selectDefinitionContext(
  text: string,
  selection: {
    mode: "none" | "select";
    include: unknown[];
    exclude: unknown[];
  },
): string {
  if (selection.mode === "none")
    return editField(text, ["selection"], '{"mode":"none"}');
  for (const key of ["mode", "include", "exclude"] as const)
    text = editField(text, ["selection", key], JSON.stringify(selection[key]));
  return text;
}
