import { renderToStaticMarkup } from "react-dom/server";
import { expect, it } from "vitest";
import { EnvelopeStatus } from "./context-status";
it("shows the engine failure reason even when the envelope also has an error code", () => {
  const html = renderToStaticMarkup(
    <EnvelopeStatus
      value={{
        error: { code: "unavailable" },
        meta: {
          position: null,
          completeness: { state: "pending", reason: "graph_unavailable" },
        },
      }}
    />,
  );
  expect(html).toContain("関係探索へ接続できません。");
  expect(html).not.toContain("指定した範囲の取得を確認しました");
});
