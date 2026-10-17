import {
  applyEdits,
  findNodeAtLocation,
  modify,
  parseTree,
} from "jsonc-parser";
import type { JSONPath, ParseError } from "jsonc-parser";

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
  const node = findNodeAtLocation(tree(text), path);
  return node ? text.slice(node.offset, node.offset + node.length) : "";
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
