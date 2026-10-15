import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderToStaticMarkup } from "react-dom/server";
import { expect, it } from "vitest";
import { App } from "./App";

it.each([false, true])(
  "renders the shared Context shell for native=%s",
  (native) => {
    const queryClient = new QueryClient();
    const html = renderToStaticMarkup(
      <QueryClientProvider client={queryClient}>
        <App apiBaseUrl="http://127.0.0.1:8080" native={native} />
      </QueryClientProvider>,
    );

    expect(html).toContain("<h1>koujou-san</h1>");
    expect(html).toContain(native ? "Tauri ネイティブ" : "Web");
    expect(html).toContain("Contextの取得・保存・入力選択と");
    queryClient.clear();
  },
);
