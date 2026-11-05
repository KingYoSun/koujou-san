// @vitest-environment happy-dom
import { act } from "react";
import { createRoot } from "react-dom/client";
import { renderToStaticMarkup } from "react-dom/server";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { expect, it } from "vitest";
import { ProjectBootstrap } from "./project-bootstrap";
import { SyncEngine, type PendingWrite, type SyncItem } from "./sync-engine";
import { editorStore, type UI } from "./context-ui";

async function fixture(canCreate = true) {
  Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
  const items = new Map<string, SyncItem>();
  const receipts = new Map<string, { data: { version: string } }>();
  const posts: { key: string; request: PendingWrite }[] = [];
  let lostAt = 0;
  let sequence = 0;
  const engine = new SyncEngine(
    { api: "https://fixture.test/", user: "alice", workspace: "empty" },
    {
      load: async () => undefined,
      save: async () => {},
      page: async () => ({ entries: [], next: null }),
      read: async () => undefined,
      remove: async () => {},
    },
    async (path, method, body) => {
      if (path.endsWith("/pull")) {
        const { known } = body as { known: { kind: string; id: string }[] };
        expect(known.every((ref) => items.has(`${ref.kind}:${ref.id}`))).toBe(
          true,
        );
        return {
          data: {
            items: known.map((ref) => items.get(`${ref.kind}:${ref.id}`)),
            removed: [],
            offered: [],
            permissions: [],
          },
          meta: {
            workspace_id: "empty",
            position: "p1",
            next_cursor: null,
            completeness: { state: "complete" },
          },
        };
      }
      expect(method).toBe("POST");
      const [kind, id] = path.split("/").slice(-2);
      const key = `${kind}:${id}`;
      const request = structuredClone(body) as PendingWrite;
      posts.push({ key, request });
      if (receipts.has(request.operation_id))
        return receipts.get(request.operation_id);
      expect(request.expected_version).toBe("0");
      expect(items.has(key)).toBe(false);
      if (kind === "membership") {
        const scope = request.value.scope as { id: string };
        const context = request.value.context as { kind: string; id: string };
        expect(items.has(`scope:${scope.id}`)).toBe(true);
        expect(items.has(`${context.kind}:${context.id}`)).toBe(true);
      }
      items.set(key, {
        record: { ref: { kind, id, revision: "v1" }, value: request.value },
        editable: true,
      });
      const receipt = { data: { version: "v1" } };
      receipts.set(request.operation_id, receipt);
      if (posts.length === lostAt) {
        lostAt = 0;
        throw Error("response lost");
      }
      return receipt;
    },
    () => {},
    () => `id-${++sequence}`,
  );
  await engine.start();
  const store = editorStore();
  const ui: UI = {
    native: false,
    api: "https://fixture.test/",
    user: "alice",
    workspace: "empty",
    engine,
    store,
    prefix: ["empty"],
    canCreate,
    request: async () => {
      throw Error("unexpected API");
    },
    fail: () => {},
  };
  const client = new QueryClient();
  const host = document.createElement("div");
  document.body.append(host);
  const root = createRoot(host);
  const element = (
    <QueryClientProvider client={client}>
      <ProjectBootstrap ui={ui} />
    </QueryClientProvider>
  );
  await act(() => root.render(element));
  const change = async (selector: string, value: string) =>
    act(() => {
      const input = host.querySelector<HTMLInputElement | HTMLTextAreaElement>(
        selector,
      )!;
      const prototype =
        input instanceof HTMLTextAreaElement
          ? HTMLTextAreaElement.prototype
          : HTMLInputElement.prototype;
      Object.getOwnPropertyDescriptor(prototype, "value")!.set!.call(
        input,
        value,
      );
      input.dispatchEvent(new Event("input", { bubbles: true }));
    });
  const submit = async () =>
    act(async () => {
      host
        .querySelector("form")!
        .dispatchEvent(
          new Event("submit", { bubbles: true, cancelable: true }),
        );
    });
  return {
    ui,
    items,
    posts,
    host,
    element,
    change,
    submit,
    render: async (active: boolean) =>
      act(() =>
        root.render(
          <QueryClientProvider client={client}>
            <ProjectBootstrap ui={ui} active={active} />
          </QueryClientProvider>,
        ),
      ),
    loseResponse: (at = 1) => {
      lostAt = at;
    },
    close: async () => {
      await act(() => root.unmount());
      host.remove();
      client.clear();
      await engine.stop();
    },
  };
}

it.each([null, "txt", "md", "html"])(
  "creates the first Project and Context from %s input",
  async (extension) => {
    const f = await fixture();
    const text =
      extension === "html"
        ? '<h1>企画</h1>\n<script>throw Error("must not execute")</script>'
        : "# 企画\n\n最初の仕様。\n";
    try {
      if (extension) {
        const input =
          f.host.querySelector<HTMLInputElement>('input[type="file"]')!;
        expect(input.accept).toBe(".txt,.md,.html");
        Object.defineProperty(input, "files", {
          configurable: true,
          value: [new File([text], `企画.${extension}`)],
        });
        await act(async () =>
          input.dispatchEvent(new Event("change", { bubbles: true })),
        );
        expect(f.host.querySelector("textarea")!.value).toBe(text);
      } else {
        await f.change("textarea", text);
      }
      await f.change('input:not([type="file"])', "最初のProject");
      await f.submit();
      expect(f.host.textContent).toContain(
        "初期ContextとProjectへの所属を保存しました。",
      );
      expect(f.items.size).toBe(3);
      const saved = [...f.items.values()];
      expect(
        saved.find((i) => i.record.ref.kind === "sentence")!.record.value.text,
      ).toBe(text);
      const project = saved.find((i) => i.record.ref.kind === "scope")!;
      expect(project.record.value).toEqual({
        kind: "project",
        name: "最初のProject",
      });
      expect(f.ui.store.getState().selected).toEqual({
        kind: "scope",
        id: project.record.ref.id,
      });
      expect(f.host.querySelector("a")!.getAttribute("href")).toContain(
        "view=documents",
      );
      expect(f.posts).toHaveLength(3);
    } finally {
      await f.close();
    }
  },
);

it("keeps input while another screen is visible without leaving a hidden form", async () => {
  const f = await fixture();
  try {
    await f.change('input:not([type="file"])', "作成途中");
    await f.change("textarea", "戻ってから保存する本文");
    await f.render(false);
    expect(f.host.querySelector("form")).toBeNull();
    await f.render(true);
    expect(f.host.querySelector("textarea")!.value).toBe(
      "戻ってから保存する本文",
    );
    await f.submit();
    expect(f.host.textContent).toContain("所属を保存しました。");
  } finally {
    await f.close();
  }
});

it("confirms an unknown save with the same request before continuing", async () => {
  const f = await fixture();
  try {
    await f.change('input:not([type="file"])', "再確認");
    await f.change("textarea", "保存する本文");
    f.loseResponse();
    await f.submit();
    expect(f.host.querySelector('[role="alert"]')!.textContent).toContain(
      "作成は未完了です。",
    );
    expect(f.host.textContent).not.toContain("所属を保存しました。");
    expect(f.items.size).toBe(1);
    await act(() => f.ui.engine.refresh());
    await f.submit();
    expect(f.posts[1]).toEqual(f.posts[0]);
    expect(f.items.size).toBe(3);
    expect(f.host.textContent).toContain("所属を保存しました。");
  } finally {
    await f.close();
  }
});

it("continues after opening a Project draft evicts the saved Context from the cache", async () => {
  const f = await fixture();
  try {
    await f.change('input:not([type="file"])', "下書きを確認するProject");
    await f.change("textarea", "最初のContext");
    f.loseResponse(2);
    await f.submit();
    expect(f.items.size).toBe(2);
    const context = [...f.items.values()].find(
      (item) => item.record.ref.kind === "sentence",
    )!.record.ref;
    const project = [...f.items.values()].find(
      (item) => item.record.ref.kind === "scope",
    )!.record.ref;
    await act(() => f.ui.engine.open(project));
    expect(
      f.ui.engine.state.receipts[`sentence:${context.id}`],
    ).toBeUndefined();
    await f.submit();
    expect(f.host.textContent).toContain("所属を保存しました。");
    expect(f.posts[2]).toEqual(f.posts[1]);
    expect(f.items.size).toBe(3);
  } finally {
    await f.close();
  }
});

it("exposes a labeled form and keeps creation disabled for viewers", async () => {
  const f = await fixture(false);
  try {
    const html = renderToStaticMarkup(f.element);
    expect(html).toContain('aria-label="初期ContextとProjectの作成"');
    expect(html).toContain("編集権限がありません。");
    expect(f.host.querySelector("fieldset")!.disabled).toBe(true);
    expect(f.host.querySelector("button")!.disabled).toBe(true);
    await f.submit();
    expect(f.posts).toHaveLength(0);
  } finally {
    await f.close();
  }
});
