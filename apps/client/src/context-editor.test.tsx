import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderToStaticMarkup } from "react-dom/server";
import { expect, it } from "vitest";
import { ContextEditor } from "./context-editor";
import { SyncEngine } from "./sync-engine";
import { editorStore } from "./context-ui";
import type { UI } from "./context-ui";
it.each(["scope", "sentence"])(
  "allows editing and saving a new %s without a shared version",
  async (kind) => {
    const engine = new SyncEngine(
      { api: "https://fixture.test/", user: "alice", workspace: "empty" },
      {
        load: async () => undefined,
        save: async () => {},
        page: async () => ({ entries: [], next: null }),
        read: async () => undefined,
        remove: async () => {},
      },
      async () => ({}),
    );
    engine.status = "current";
    const key = await engine.create(
      kind,
      kind === "scope"
        ? { kind: "project", name: "最初のProject" }
        : { kind, name: "最初のContext", text: "本文" },
    );
    const store = editorStore();
    store.getState().choose({ kind, id: key.slice(key.indexOf(":") + 1) });
    store.getInitialState = store.getState;
    const client = new QueryClient();
    const html = renderToStaticMarkup(
      <QueryClientProvider client={client}>
        <ContextEditor
          ui={{
            native: false,
            api: "https://fixture.test/",
            user: "alice",
            workspace: "empty",
            engine,
            store,
            prefix: ["empty"],
            canCreate: true,
            request: async () => {
              throw Error("unexpected request");
            },
            fail: () => {},
          }}
        />
      </QueryClientProvider>,
    );
    expect(html).toContain("下書きを共有版へ保存");
    expect(html).not.toContain("閲覧のみ");
    expect(html).toMatch(/<input(?![^>]*disabled)[^>]*value="最初の/);
    expect(html).toMatch(
      /<button(?![^>]*disabled)[^>]*>下書きを共有版へ保存<\/button>/,
    );
    client.clear();
    await engine.stop();
  },
);
it("shows the editable title of a document that also has an existing name", async () => {
  const engine = new SyncEngine(
    { api: "https://fixture.test/", user: "alice", workspace: "one" },
    {
      load: async () => undefined,
      save: async () => {},
      page: async () => ({ entries: [], next: null }),
      read: async () => undefined,
      remove: async () => {},
    },
    async () => ({}),
  );
  engine.state.items["document:doc"] = {
    record: {
      ref: { kind: "document", id: "doc", revision: "v1" },
      value: {
        kind: "document",
        name: "existing name",
        title: "current document title",
        children: [],
      },
    },
    editable: true,
  };
  engine.status = "current";
  const store = editorStore();
  store.getState().choose({ kind: "document", id: "doc" });
  store.getInitialState = store.getState;
  const client = new QueryClient();
  const ui: UI = {
    native: false,
    api: "https://fixture.test/",
    user: "alice",
    workspace: "one",
    engine,
    store,
    prefix: ["fixture"],
    canCreate: true,
    request: async () => {
      throw new Error("no request in server rendering");
    },
    fail: () => {},
  };
  try {
    const html = renderToStaticMarkup(
      <QueryClientProvider client={client}>
        <ContextEditor ui={ui} />
      </QueryClientProvider>,
    );
    expect(html).toContain('value="current document title"');
  } finally {
    client.clear();
    await engine.stop();
  }
});
