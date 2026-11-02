import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { ServerModelFields, type SemanticSettings } from "./server-model";

describe("server model output limits", () => {
  const base: SemanticSettings = {
    connection: "owned",
    revision: "owned-revision",
    model: "owned-model",
    allowed_models: ["owned-model"],
    fallbacks: [],
    instructions: "owned instruction",
    limits: { input_bytes: 4096, output_tokens: 4096, seconds: 30 },
    reevaluate_evidence: true,
  };

  it("keeps the API output token input", () => {
    const html = renderToStaticMarkup(
      <ServerModelFields
        settings={base}
        connections={[]}
        onChange={() => {}}
      />,
    );
    expect(html).toContain("出力上限（token）");
    expect(html).toContain('value="4096"');
    expect(html).not.toContain("上限なし");
  });

  it("shows unlimited output for the saved ChatGPT policy while retaining input and time bounds", () => {
    const html = renderToStaticMarkup(
      <ServerModelFields
        settings={{
          ...base,
          unlimited_output: true,
          limits: { ...base.limits, output_tokens: 0 },
        }}
        connections={[
          {
            id: "owned",
            name: "Owned plan",
            type: "chatgpt",
            revision: "owned-revision",
            models: [
              { requested: "owned-model", allowed_actual: ["owned-model"] },
            ],
          },
        ]}
        onChange={() => {}}
      />,
    );
    expect(html).toContain("<output>上限なし</output>");
    expect(html).not.toContain('value="0"');
    expect(html).toContain("入力上限（UTF-8 byte）");
    expect(html).toContain("要求の時間上限（秒）");
    expect(html).toContain('value="30"');
  });
});
