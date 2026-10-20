import { expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { ComparisonResults } from "./usage-comparison-results";
import { newCondition } from "./usage-comparison-model";

it("opens the fixed Run and definition behind a comparison without linking mutable or missing versions", () => {
  const html = renderToStaticMarkup(
    <ComparisonResults
      page={{
        data: {
          items: [
            {
              key: "measured",
              refs: [
                { id: "run:measured", version: "" },
                { id: "run:measured", version: "fixed-run" },
                { id: "run:measured", version: "fixed-run" },
                { id: "run:pending", version: "0" },
                { id: "run:mutable", version: "head" },
                { id: "run-definition:measured", version: "fixed-definition" },
                {
                  id: "workflow-definition:measured",
                  version: "fixed-workflow",
                },
              ],
            },
          ],
          as_of: "2026-10-21T00:00:00Z",
          through: 1,
          timezone: "Asia/Tokyo",
          kind: "usage",
        },
        meta: { next_cursor: null },
      }}
      condition={newCondition()}
      mode="compare_details"
      onNext={() => {}}
      onDetails={() => {}}
      onRead={() => {}}
      onReevaluate={() => {}}
      busy={false}
      editable={false}
    />,
  );
  expect(html).toContain(
    "kind=run&amp;id=run%3Ameasured&amp;version=fixed-run",
  );
  expect(html).toContain(
    "kind=run-definition&amp;id=run-definition%3Ameasured&amp;version=fixed-definition",
  );
  expect(html).toContain(
    "kind=workflow-definition&amp;id=workflow-definition%3Ameasured&amp;version=fixed-workflow",
  );
  expect(html).not.toContain("id=run%3Apending");
  expect(html).not.toContain("id=run%3Amutable");
  expect(html.match(/kind=run&amp;id=run%3Ameasured/g)).toHaveLength(1);
});
