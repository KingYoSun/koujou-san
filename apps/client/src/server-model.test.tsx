import { renderToStaticMarkup } from "react-dom/server";
import { Children, isValidElement, type ReactNode } from "react";
import { describe, expect, it } from "vitest";
import {
  ServerModelFields,
  type ModelConnection,
  type SemanticSettings,
} from "./server-model";

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
    expect(html).toContain('<option value="owned" selected="">owned</option>');
  });

  it.each([
    ["api", "API fixture"],
    ["plan", "ChatGPTプラン fixture"],
  ])("keeps the selected connection name for %s", (id, name) => {
    const html = renderToStaticMarkup(
      <ServerModelFields
        settings={{ ...base, connection: id }}
        connections={[
          {
            id,
            name,
            revision: "owned-revision",
            models: [
              { requested: base.model, allowed_actual: base.allowed_models },
            ],
          },
        ]}
        onChange={() => {}}
      />,
    );
    expect(html).toContain(
      `<option value="${id}" selected="">${name}</option>`,
    );
  });

  it("keeps connection order, names and output controls across API → plan → API changes", () => {
    const connections: ModelConnection[] = [
      {
        id: "api",
        name: "API fixture",
        revision: "api-v1",
        models: [{ requested: "api-model", allowed_actual: ["api-actual"] }],
      },
      {
        id: "plan",
        name: "ChatGPTプラン fixture",
        type: "chatgpt",
        revision: "plan-v1",
        models: [{ requested: "plan-model", allowed_actual: ["plan-actual"] }],
      },
    ];
    let settings = { ...base, connection: "api" };
    const select = (
      node: ReactNode,
    ):
      | { onChange: (event: { target: { value: string } }) => void }
      | undefined => {
      for (const child of Children.toArray(node)) {
        if (
          !isValidElement<{
            children?: ReactNode;
            onChange: (event: { target: { value: string } }) => void;
          }>(child)
        )
          continue;
        if (child.type === "select") return child.props;
        const found = select(child.props.children);
        if (found) return found;
      }
    };
    for (const id of ["api", "plan", "api"]) {
      const fields = () =>
        ServerModelFields({
          settings,
          connections,
          onChange: (patch) => {
            settings = { ...settings, ...patch };
          },
        });
      select(fields())!.onChange({ target: { value: id } });
      const html = renderToStaticMarkup(fields());
      const options = html.match(/<select[^>]*>(.*?)<\/select>/)![1];
      expect(options.match(/value="[^"]*"/g)).toEqual([
        'value="api"',
        'value="plan"',
      ]);
      expect(options).toContain("API fixture");
      expect(options).toContain("ChatGPTプラン fixture");
      expect(settings.connection).toBe(id);
      expect(settings.revision).toBe(`${id}-v1`);
      expect(settings.model).toBe(`${id}-model`);
      expect(settings.allowed_models).toEqual([`${id}-actual`]);
      expect(settings.limits.input_bytes).toBe(4096);
      expect(settings.limits.seconds).toBe(30);
      expect(html.includes("<output>上限なし</output>")).toBe(id === "plan");
      expect(settings.limits.output_tokens).toBe(id === "plan" ? 0 : 4096);
    }
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
