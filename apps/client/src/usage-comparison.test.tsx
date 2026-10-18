import { ComparisonConditions } from "./usage-comparison-conditions";
import { storedCondition } from "./usage-comparison-model";
import { renderToStaticMarkup } from "react-dom/server";
import { expect, it } from "vitest";
import { ComparisonResults } from "./usage-comparison-results";
import { newCondition, type CostPage } from "./usage-comparison-model";
it("shows missing measurements and historical quality without turning them into payment or current independent PASS", () => {
  const condition = newCondition();
  condition.kind = "api_equivalent";
  const page: CostPage = {
    data: {
      as_of: "2026-10-19T00:00:00Z",
      through: 100,
      timezone: "Asia/Tokyo",
      kind: condition.kind,
      items: [
        {
          key: "missing",
          attribution_kind: "operation",
          measurement: "partial",
          billing: "subscription",
          state: "unrated",
          amount: null,
          quantities: [
            {
              category: "input",
              unit: "token",
              value: null,
              reported: 0,
              estimated: 0,
              missing: 1,
              not_applicable: 0,
            },
          ],
        },
      ],
      summary: {
        runs: 1,
        completed: 1,
        failed: 0,
        unfinished: 0,
        rework: 0,
        execution_seconds: null,
        waiting_seconds: null,
        time_unavailable: 1,
        run_metadata_unavailable: 0,
        quality: [
          {
            source: "self-report",
            state: "valid",
            verdict: "PASS",
            records: 1,
          },
        ],
      },
    },
    meta: { next_cursor: "next-owned-page" },
  };
  const html = renderToStaticMarkup(
    <ComparisonResults
      page={page}
      condition={condition}
      mode="compare"
      onNext={() => {}}
      onDetails={() => {}}
      onRead={() => {}}
      onReevaluate={() => {}}
      busy={false}
      editable={true}
    />,
  );
  expect(html).toContain("未算定");
  expect(html).toContain("CLIの自己申告");
  expect(html).toContain("現在の有効性は検品の詳細で確認");
  expect(html).toContain("定額契約");
  expect(html).toContain("比較結果の次の50件");
  expect(html).not.toContain("0 USD");
  expect(html).toContain("API換算額は支払額や節約額ではありません");
});

it("loads Go conditions whose empty optional selectors are omitted", () => {
  const wire = {
    mode: "compare",
    kind: "usage",
    from: "2026-10-19T00:00:00Z",
    until: "2026-10-20T00:00:00Z",
    timezone: "Asia/Tokyo",
    limit: 50,
  };
  const html = renderToStaticMarkup(
    <ComparisonConditions
      name="比較A"
      value={storedCondition(wire)}
      onChange={() => {}}
      call={async () => {
        throw Error("no request during render");
      }}
      workspace="owned"
      disabled={false}
    />,
  );
  expect(html).toContain("Workspace内の閲覧可能な範囲すべて");
  expect(html).toContain("比較Aの比較条件");
});

it("shows one provenance link per fixed record and keeps detail measurement metadata", () => {
  const condition = newCondition();
  const record = { id: "usage:one", version: "fixed" };
  const page: CostPage = {
    data: {
      as_of: "2026-10-19T00:00:00Z",
      through: 1,
      timezone: "UTC",
      kind: "api_equivalent",
      items: [
        {
          key: "detail",
          group: "one",
          refs: [record, record],
          value: {
            measurement: {
              state: "partial",
              billing: "subscription",
              scope: "main-only",
            },
          },
          amount: null,
          state: "unrated",
        },
      ],
    },
    meta: { next_cursor: null },
  };
  const html = renderToStaticMarkup(
    <ComparisonResults
      page={page}
      condition={condition}
      mode="compare_details"
      onNext={() => {}}
      onDetails={() => {}}
      onRead={() => {}}
      onReevaluate={() => {}}
      busy={false}
      editable={true}
    />,
  );
  expect(html.split("usage:one").length - 1).toBe(1);
  expect(html).toContain("一部計測");
  expect(html).toContain("定額契約");
  expect(html).toContain("main-only");
});
