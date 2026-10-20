import { expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { JSONField } from "./json-field";
import { applyFields, fieldSource } from "./workflow-json";

it("retains an incomplete raw field and describes the error without changing the source", () => {
  const text =
    '{"condition":{"field":"run.state","op":"eq","value":"failed"},"untouched":900719925474099312345}';
  const fields = { '["condition"]': '{"field":' };
  const html = renderToStaticMarkup(
    <JSONField
      label="型付き条件"
      path={["condition"]}
      format="json"
      text={text}
      fields={fields}
      onChange={() => {}}
    />,
  );
  expect(html).toContain('aria-invalid="true"');
  expect(html).toContain("入力は保持しています");
  expect(html).toContain("{&quot;field&quot;:");
  expect(() => applyFields(text, fields)).toThrow();
  expect(fieldSource(text, ["untouched"])).toBe("900719925474099312345");
});

it("composes multiple structured edits and explicit removals while preserving fixed refs and exact raw numbers", () => {
  const text =
    '{"name":"old","action":{"template":{"id":"run-definition:a","version":"90071992547409931234"},"start":true},"bounds":{"depth":3,"extra":900719925474099312345},"problem":{"mode":"hold"}}';
  const result = applyFields(text, {
    '["name"]': '"new"',
    '["action","start"]': "false",
    '["bounds","depth"]': "4",
    '["problem"]': null,
  });
  expect(fieldSource(result, ["action", "template", "version"])).toBe(
    '"90071992547409931234"',
  );
  expect(fieldSource(result, ["bounds", "extra"])).toBe(
    "900719925474099312345",
  );
  expect(fieldSource(result, ["action", "start"])).toBe("false");
  expect(fieldSource(result, ["problem"])).toBe("");
});

it("distinguishes an explicit false option from a missing or checked option", () => {
  const render = (value: string) =>
    renderToStaticMarkup(
      <JSONField
        label="通常の割当へ進める"
        path={["start"]}
        format="boolean"
        text={`{"start":${value}}`}
        fields={{}}
        onChange={() => {}}
      />,
    );
  expect(render("true")).toContain('checked=""');
  expect(render("false")).not.toContain('checked=""');
});
