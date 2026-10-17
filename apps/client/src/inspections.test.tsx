import { expect, test } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { InspectionResult } from "./inspections";

const view = (state: string, report: unknown) =>
  ({
    launch: {
      id: "inspection-launch:one",
      version: "one",
      owner_id: "inspection:one",
      kind: "launch",
      value: {
        definition: { id: "inspection:one", version: "one" },
        run_id: "run:one",
        manifest: { digest: "fixed", state: "none" },
        separation: {},
      },
    },
    run: {
      id: "run:one",
      version: "result",
      value: {
        state: "succeeded",
        input: { id: "run-input:one", version: "one" },
        exit_code: 0,
        assessment: { state, verdict: "INCONCLUSIVE", report },
      },
    },
    validity: "current",
    evidence_current: {},
  }) as Parameters<typeof InspectionResult>[0]["value"];

test("a malformed stored report remains visible without rendering invalid criterion rows", () => {
  const original = {
    criteria_results: "malformed-original",
    unverified: { malformed: true },
  };
  const html = renderToStaticMarkup(
    <InspectionResult value={view("invalid", original)} title="検品" />,
  );
  expect(html).toContain("invalid");
  expect(html).toContain("INCONCLUSIVE");
  expect(html).toContain("malformed-original");
  expect(html).not.toContain("<td");
});

test("a valid report shows the criterion verdict and its evidence", () => {
  const report = {
    criteria_results: [
      {
        criterion_id: "AC-1",
        verdict: "FAIL",
        executed: true,
        evidence_refs: [{ id: "inspection-evidence:one", version: "fixed" }],
        unverified: [],
      },
    ],
    unverified: [],
  };
  const html = renderToStaticMarkup(
    <InspectionResult value={view("valid", report)} title="検品" />,
  );
  expect(html).toContain("<td>AC-1</td>");
  expect(html).toContain("<td>FAIL</td>");
  expect(html).toContain("inspection-evidence:one@fixed");
});
