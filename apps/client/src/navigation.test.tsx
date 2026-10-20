import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderToStaticMarkup } from "react-dom/server";
import { afterEach, expect, it, vi } from "vitest";
import {
  NavigationProvider,
  readRoute,
  routeHref,
  type Screen,
} from "./navigation";
import { WorkspaceShell } from "./workspace-shell";

afterEach(() => vi.unstubAllGlobals());

it("keeps fixed references, filters and page cursors in a shareable route", () => {
  const route = {
    screen: "work" as const,
    workspace: "workspace/日本語",
    target: { kind: "run", id: "run:検品/1", version: "90071992547409931234" },
    view: "evidence",
    query: "判断待ち",
    cursor: "page/2+3",
  };
  expect(readRoute(routeHref(route), route.workspace)).toEqual(route);
});

it("does not carry another Workspace's selection or arbitrary content into navigation", () => {
  expect(
    readRoute(
      "?workspace=a&screen=context&kind=document&id=private&version=v1",
      "b",
    ),
  ).toEqual({ workspace: "b", screen: "work" });
  const route = readRoute(
    "?workspace=a&screen=work&body=private&answer=draft&token=secret",
    "a",
  );
  expect(routeHref(route)).toBe("?workspace=a&screen=work");
  expect(readRoute("?workspace=a&screen=unknown", "a").screen).toBe("work");
});

function renderScreen(screen: Screen, native = false, role = "editor") {
  vi.stubGlobal("location", { search: `?workspace=w&screen=${screen}` });
  const client = new QueryClient();
  const html = renderToStaticMarkup(
    <QueryClientProvider client={client}>
      <NavigationProvider api="api" user="user" workspace="w">
        <WorkspaceShell
          native={native}
          api="api"
          user="user"
          workspace="w"
          name="作業場所"
          role={role}
          canExecute={false}
          members={[]}
          selector={<span>Workspace選択</span>}
          management={<p>参加者管理フォーム</p>}
          account={<p>アカウント設定フォーム</p>}
          connection={<p>API接続設定</p>}
          health="接続確認済み"
          onScreen={() => {}}
          onRevoked={() => {}}
        />
      </NavigationProvider>
    </QueryClientProvider>,
  );
  client.clear();
  return html;
}

it.each(["work", "workflows", "account"] as const)(
  "renders only the selected %s screen and no hidden forms",
  (screen) => {
    const html = renderScreen(screen);
    expect(html).toContain('aria-label="主な画面"');
    expect(html).toContain('id="screen-title"');
    expect(html).not.toContain("Contextの検索語");
    expect(html).not.toContain("参加者管理フォーム");
    expect(html.includes("アカウント設定フォーム")).toBe(screen === "account");
    expect(html.includes("新しいRunを作成")).toBe(screen === "work");
    expect(html.includes("新しい定義の下書き")).toBe(screen === "workflows");
  },
);

it.each([
  [false, "editor", false],
  [true, "editor", true],
  [true, "viewer", false],
] as const)(
  "keeps local execution settings within native capability and role: native=%s, role=%s",
  (native, role, allowed) => {
    const html = renderScreen("execution", native, role);
    expect(html.includes("実行するprogramのpath")).toBe(allowed);
    expect(html.includes("接続先の論理名")).toBe(allowed);
    if (!allowed)
      expect(html).toContain(
        native ? "編集権限が必要" : "Webにローカル実行能力はありません",
      );
  },
);
it("keeps pricing edits and shared automation controls out of unrelated work screens", () => {
  expect(renderScreen("usage")).not.toContain("料金の手動設定");
  expect(renderScreen("pricing")).toContain("料金の手動設定");
  expect(renderScreen("priority", false, "admin")).not.toContain(
    "自動化主体・処理枠・予算",
  );
  expect(renderScreen("triage", false, "admin")).not.toContain(
    "自動化主体・処理枠・予算",
  );
  expect(renderScreen("operations", false, "admin")).toContain(
    "自動化主体・処理枠・予算",
  );
});
