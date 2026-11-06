import { renderToStaticMarkup } from "react-dom/server";
import { expect, it } from "vitest";
import { ChatMarkdown } from "./chat-markdown";

it("renders the actual response structure without executing model HTML or fetching embedded images", () => {
  const html = renderToStaticMarkup(
    <ChatMarkdown
      text={
        "### 確認結果\n\n**設定済み**\n\n|項目|状態|\n|---|---|\n|CLI|未取得|\n\n- README\n\n```ts\nconst ready = true;\n```\n\n[根拠](https://example.test/file)\n\n<script>alert(1)</script>\n\n[危険](javascript:alert(1))\n\n![画像](https://example.test/tracking)"
      }
    />,
  );
  expect(html).toContain("<h3>確認結果</h3>");
  expect(html).toContain("<strong>設定済み</strong>");
  expect(html).toContain("<table>");
  expect(html).toContain("<li>README</li>");
  expect(html).toContain('class="language-ts"');
  expect(html).toContain('href="https://example.test/file"');
  expect(html).not.toMatch(/<script|<img|href="javascript:/);
});
