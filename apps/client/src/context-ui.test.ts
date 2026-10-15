import { expect, it } from "vitest";
import { editorStore } from "./context-ui";
it("uses only fixed reference fields when selecting graph nodes", () => {
  const store = editorStore();
  const node = {
    kind: "sentence",
    id: "shared",
    revision: "v1",
    form: "version",
    current: true,
    deleted: false,
  };
  store.getState().add(node);
  store.getState().choose(node);
  expect(store.getState().include).toEqual([
    { kind: "sentence", id: "shared", revision: "v1" },
  ]);
  expect(store.getState().selected).toEqual({ kind: "sentence", id: "shared" });
});
