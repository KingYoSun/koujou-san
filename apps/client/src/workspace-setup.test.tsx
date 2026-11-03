// @vitest-environment happy-dom
import { act } from "react";
import { createRoot } from "react-dom/client";
import { expect, it, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import "fake-indexeddb/auto";
import { syncRequest } from "./auth";
import { NavigationProvider } from "./navigation";
import { WorkspaceShell } from "./workspace-shell";
import { request as httpRequest } from "node:http";

async function ownedHTTP(
  url: string,
  options: {
    method?: string;
    headers?: Record<string, string>;
    body?: string;
  } = {},
): Promise<{ status: number; ok: boolean; json: () => Promise<unknown> }> {
  if (
    !url.startsWith(process.env.KOUJOU_SETUP_API!) &&
    !url.startsWith(process.env.KOUJOU_SETUP_FORGE!)
  )
    throw Error("foreign fixture origin");
  return new Promise((resolve, reject) => {
    const req = httpRequest(url, options, (res) => {
      if (res.statusCode === 302 && res.headers.location) {
        res.resume();
        void ownedHTTP(res.headers.location).then(resolve, reject);
        return;
      }
      const chunks: Buffer[] = [];
      res.on("data", (chunk) => chunks.push(chunk));
      res.on("end", () =>
        resolve({
          status: res.statusCode!,
          ok: res.statusCode! < 400,
          json: async () => JSON.parse(Buffer.concat(chunks).toString()),
        }),
      );
    });
    req.on("error", reject);
    req.end(options.body);
  });
}

vi.mock("./auth", () => ({
  syncRequest: vi.fn(),
  apiRequest: vi.fn(),
  mcpRequest: vi.fn(),
  connectMCP: vi.fn(),
  workspaceAccessLost: vi.fn(),
}));
const api = process.env.KOUJOU_SETUP_API;
it.skipIf(!api)(
  "starts an empty Workspace through real forms and owned OAuth/HTTP, without provisioning helpers",
  async () => {
    Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
    localStorage.clear();
    const errors: string[] = [];
    vi.mocked(syncRequest).mockImplementation(
      async (path, _native, options) => {
        const response = await ownedHTTP(api + path, {
          method: options.method,
          headers: {
            Authorization: "Bearer setup-admin",
            "Content-Type": "application/json",
          },
          body:
            options.body === undefined
              ? undefined
              : JSON.stringify(options.body),
        });
        const value = await response.json();
        if (!response.ok) {
          errors.push(`${path}: ${response.status}`);
          throw Error(JSON.stringify(value));
        }
        return value;
      },
    );
    let authorization: Promise<unknown> | undefined;
    const popup = {
      opener: null,
      close: vi.fn(),
      location: {
        replace: (url: string) => {
          authorization = ownedHTTP(url).then(async (res) => {
            expect(res.status).toBe(200);
          });
        },
      },
    };
    vi.spyOn(window, "open").mockReturnValue(popup as unknown as Window);
    const host = document.createElement("div");
    document.body.append(host);
    const root = createRoot(host);
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    history.replaceState(null, "", "?workspace=setup-ui&screen=serverModels");
    const button = (text: string) => {
      const item = [...host.querySelectorAll("button")].find(
        (node) => node.textContent === text,
      );
      if (!item) throw Error(`button ${text}\n${host.textContent}`);
      return item;
    };
    const field = (text: string) => {
      const label = [...host.querySelectorAll("label")].find((node) =>
        node.textContent?.startsWith(text),
      );
      const element = label?.querySelector<
        HTMLInputElement | HTMLSelectElement | HTMLTextAreaElement
      >("input,select,textarea");
      if (!element) throw Error(`field ${text}\n${host.textContent}`);
      return element;
    };
    const settled = async (check: () => void) => {
      await vi.waitFor(
        async () => {
          await act(async () => {
            await new Promise((resolve) => setTimeout(resolve, 10));
          });
          check();
        },
        { timeout: 10000, interval: 30 },
      );
    };
    const click = async (text: string) => {
      await act(() => button(text).click());
    };
    const link = async (text: string) => {
      const item = [...host.querySelectorAll("a")].find(
        (node) => node.textContent === text,
      );
      expect(item, `link ${text}`).toBeTruthy();
      await act(() =>
        item!.dispatchEvent(
          new MouseEvent("click", {
            bubbles: true,
            cancelable: true,
            button: 0,
          }),
        ),
      );
    };
    const change = async (text: string, value: string) => {
      await act(() => {
        const element = field(text);
        const proto =
          element instanceof HTMLSelectElement
            ? HTMLSelectElement.prototype
            : element instanceof HTMLTextAreaElement
              ? HTMLTextAreaElement.prototype
              : HTMLInputElement.prototype;
        Object.getOwnPropertyDescriptor(proto, "value")!.set!.call(
          element,
          value,
        );
        element.dispatchEvent(
          new Event(element instanceof HTMLSelectElement ? "change" : "input", {
            bubbles: true,
          }),
        );
      });
    };
    const submit = async (text: string) => {
      await act(() =>
        button(text)
          .closest("form")!
          .dispatchEvent(
            new Event("submit", { bubbles: true, cancelable: true }),
          ),
      );
    };
    try {
      await act(() =>
        root.render(
          <QueryClientProvider client={client}>
            <NavigationProvider
              api={api!}
              user="setup-admin"
              workspace="setup-ui"
            >
              <WorkspaceShell
                native={false}
                api={api!}
                user="setup-admin"
                workspace="setup-ui"
                name="Owned Workspace"
                role="admin"
                serverAdmin
                canExecute={false}
                members={[]}
                selector={null}
                management={null}
                account={null}
                connection={null}
                health="接続済み"
                onScreen={() => {}}
                onRevoked={() => {
                  throw Error("unexpected revocation");
                }}
              />
            </NavigationProvider>
          </QueryClientProvider>,
        ),
      );
      await settled(() =>
        expect(button("Owned modelを割り当てる").disabled).toBe(false),
      );
      await click("Owned modelを割り当てる");
      await settled(() =>
        expect(button("Owned modelの割当を取り消す")).toBeTruthy(),
      );
      await link("リポジトリ接続");
      await settled(() =>
        expect(host.textContent).toContain("リポジトリはありません"),
      );
      await click("共有接続");
      await settled(() =>
        expect(
          field("Gitea").querySelectorAll("option").length,
        ).toBeGreaterThan(1),
      );
      await click("OAuthアプリを追加");
      await change("アプリ名", "Owned UI Gitea");
      await change("Gitea URL", process.env.KOUJOU_SETUP_FORGE!);
      await change("クライアントID", "owned-client");
      await change("クライアントシークレット", "owned-client-secret");
      await submit("OAuthアプリを保存");
      await settled(() =>
        expect(
          [...field("Gitea").querySelectorAll("option")].some(
            (item) => item.textContent === "Owned UI Gitea",
          ),
        ).toBe(true),
      );
      const appOption = [...field("Gitea").querySelectorAll("option")].find(
        (item) => item.textContent === "Owned UI Gitea",
      )!;
      await change("Gitea", appOption.value);
      await click("Giteaにログイン");
      await settled(() => expect(authorization).toBeTruthy());
      await authorization;
      await settled(() =>
        expect(host.textContent).toContain("共有するGiteaアカウント"),
      );
      expect(host.textContent).toContain("owner");
      await change("共有接続名", "Shared UI account");
      await act(() => field("このWorkspaceでリポジトリ作成を許可").click());
      await submit("認証した接続を共有");
      await settled(() =>
        expect(host.textContent).toContain("Shared UI account ·"),
      );
      expect(
        host.querySelector('input[type="password"]')?.getAttribute("value") ??
          "",
      ).toBe("");
      await link("Projectチャット");
      await settled(() => expect(host.textContent).toContain("Projectを作成"));
      await link("Projectを作成");
      await settled(() =>
        expect(button("Projectを新規作成").disabled).toBe(false),
      );
      await click("Projectを新規作成");
      await settled(() => expect(field("名前")).toBeTruthy());
      await change("名前", "Owned Project");
      await click("下書きを共有版へ保存");
      await settled(() =>
        expect(host.textContent).toContain("共有版を確認済み"),
      );
      await link("Projectのリポジトリを設定");
      await settled(() => expect(button("Giteaリポジトリを作成")).toBeTruthy());
      await click("Giteaリポジトリを作成");
      await settled(() =>
        expect(
          field("共有接続").querySelectorAll("option").length,
        ).toBeGreaterThan(1),
      );
      const conn = [...field("共有接続").querySelectorAll("option")].find(
        (item) => item.textContent === "Shared UI account",
      )!;
      await change("共有接続", conn.value);
      await click("所有者を取得");
      await settled(() =>
        expect(
          field("所有者").querySelector('option[value="owner"]'),
        ).toBeTruthy(),
      );
      await change("所有者", "owner");
      await change("リポジトリ名", "new-repo");
      await change("公開範囲", "private");
      await submit("リポジトリを作成");
      await settled(() =>
        expect(host.textContent).toContain("owner/new-repo · 作成済み"),
      );
      expect(host.textContent).not.toContain("作成拒否の記録を削除");
      await click("Giteaリポジトリを作成");
      await click("所有者を取得");
      await settled(() =>
        expect(
          field("所有者").querySelector('option[value="owner"]'),
        ).toBeTruthy(),
      );
      await change("所有者", "owner");
      await change("リポジトリ名", "rejected-repo");
      await change("公開範囲", "private");
      await submit("リポジトリを作成");
      await settled(() =>
        expect(button("owner/rejected-repo · 作成拒否")).toBeTruthy(),
      );
      await click("owner/rejected-repo · 作成拒否");
      await click("作成拒否の記録を削除");
      await settled(() => {
        expect(host.textContent).not.toContain("owner/rejected-repo");
        expect(button("owner/new-repo · 作成済み")).toBeTruthy();
        expect(host.textContent).toContain("作成拒否の記録を削除しました。");
      });
      await click("リポジトリを再取得");
      expect(host.textContent).not.toContain("owner/rejected-repo");
      await link("Projectチャット");
      await settled(() => expect(button("Owned Project")).toBeTruthy());
      await click("Owned Project");
      await settled(() => expect(field("新しい相談の題名")).toBeTruthy());
      await change("新しい相談の題名", "最初の相談");
      await submit("スレッドを作成");
      await settled(() => expect(host.textContent).toContain("# 最初の相談"));
      await settled(() => expect(button("保存してAIへ相談")).toBeTruthy());
      await act(() =>
        [...host.querySelectorAll("summary")]
          .find((item) => item.textContent?.startsWith("モデル未選択"))!
          .click(),
      );
      await settled(() =>
        expect(
          field("利用する接続").querySelectorAll("option").length,
        ).toBeGreaterThan(1),
      );
      expect(
        [...field("利用する接続").querySelectorAll("option")].some((item) =>
          item.textContent?.includes("Owned model"),
        ),
      ).toBe(true);
      const model = [...field("利用する接続").querySelectorAll("option")].find(
        (item) => item.textContent?.includes("Owned model"),
      )!;
      await change("利用する接続", model.value);
      await change("相談・メッセージ", "新しいリポジトリで作業を始めたい");
      expect(button("保存してAIへ相談").disabled).toBe(false);
      expect(button("保存してAIへ相談")).toBeTruthy();
      expect(
        [...host.querySelectorAll("a")].some((item) =>
          item.textContent?.includes("Run・Workflow"),
        ),
      ).toBe(true);
      expect(errors).toEqual([]);
    } finally {
      await act(() => root.unmount());
      client.clear();
      host.remove();
      vi.restoreAllMocks();
    }
  },
  30000,
);
