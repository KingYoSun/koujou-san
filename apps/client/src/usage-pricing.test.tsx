import { renderToStaticMarkup } from "react-dom/server";
import { expect, it } from "vitest";
import { UsageTable } from "./usage-pricing";

const meta = { next_cursor: null };
it("renders period aggregates as a table with units, currency and the bucket day in the headers", () => {
  const html = renderToStaticMarkup(
    <UsageTable
      mode="aggregate"
      busy={false}
      onRead={() => {}}
      page={{
        data: {
          as_of: "2026-10-19T00:00:00Z",
          through: 10,
          timezone: "Asia/Tokyo",
          kind: "api_equivalent",
          items: [
            {
              key: "a",
              day: "2026-10-19",
              attribution_kind: "run",
              attribution_id: "run:1",
              workflow: "",
              model: "fixture-model",
              currency: "USD",
              kind: "api_equivalent",
              records: 3,
              amount: "1.25",
              incomplete: 1,
              position: 10,
              quantities: [
                {
                  category: "output",
                  unit: "token",
                  value: null,
                  missing: 1,
                  not_applicable: 0,
                },
                {
                  category: "input",
                  unit: "token",
                  value: "1200",
                  missing: 0,
                  not_applicable: 0,
                },
              ],
            },
          ],
        },
        meta,
      }}
    />,
  );
  expect(html).toContain("<th>帰属</th><th>モデル</th>");
  expect(html).toContain(
    '<th data-numeric="true">入力トークン</th><th data-numeric="true">出力トークン</th>',
  );
  expect(html).toContain('<th data-numeric="true">金額（USD）</th>');
  expect(html).toContain("<th>集計日（Asia/Tokyo）</th>");
  expect(html).toContain("Run実行 / run:1");
  expect(html).toContain('<td data-numeric="true">1200</td>');
  expect(html).toContain('<td data-numeric="true">未取得</td>');
  expect(html).toContain("1.25（未算定 1件を除く）");
  expect(html).not.toContain("<pre>");
});

it("lists fixed usage records with the selected version marked and no amount", () => {
  const html = renderToStaticMarkup(
    <UsageTable
      mode="records"
      busy={false}
      onRead={() => {}}
      selected={{ id: "usage:one", version: "v1" }}
      page={{
        data: {
          as_of: "2026-10-19T00:00:00Z",
          through: 3,
          timezone: "UTC",
          kind: "usage",
          items: [
            {
              id: "usage:one",
              version: "v1",
              position: 3,
              kind: "usage",
              value: {
                connection: "fixture",
                actual_model: "fixture-model",
                at: "2026-10-19T01:02:03Z",
                attribution: { kind: "run", id: "run:1", scopes: [] },
                quantities: {
                  input: { value: "10", state: "estimated", unit: "token" },
                  output: {
                    value: null,
                    state: "not_applicable",
                    unit: "token",
                  },
                },
              },
            },
            {
              id: "usage:two",
              version: "v1",
              position: 4,
              kind: "usage",
              value: {},
            },
          ],
        },
        meta,
      }}
    />,
  );
  expect(html).toContain("<th>記録</th>");
  expect(html).toContain("<th>取得時点</th>");
  expect(html.split('aria-current="true"').length - 1).toBe(1);
  expect(html).toContain("usage:one @ v1");
  expect(html).toContain("fixture / fixture-model");
  expect(html).toContain("10（推定）");
  expect(html).toContain('<td data-numeric="true">非該当</td>');
  expect(html).not.toContain("未算定");
});
