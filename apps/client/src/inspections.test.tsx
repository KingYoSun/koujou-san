import { expect, test } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { InspectionResult, Inspections } from "./inspections";

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

test("a malformed stored report keeps the verdict without rendering invalid criterion rows", () => {
  const original = {
    criteria_results: "malformed-original",
    unverified: { malformed: true },
  };
  const html = renderToStaticMarkup(
    <InspectionResult value={view("invalid", original)} title="検品" />,
  );
  expect(html).toContain("invalid");
  expect(html).toContain("判断不能");
  expect(html).toContain("固定入力・Manifest・原レポート");
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
  expect(html).toContain("<td>不合格</td>");
  expect(html).toContain("inspection-evidence:one@fixed");
});

test.each(["invalid", "valid"])(
  "fixed review criteria stay readable before a usable %s report exists",
  (state) => {
    const html = renderToStaticMarkup(
      <InspectionResult
        value={view(state, { unverified: [] })}
        title="検品"
        criteria={[
          {
            id: "AC-1",
            requirement: "同じ依頼を二度送っても一件だけ登録される",
          },
        ]}
      />,
    );
    expect(html).toContain("同じ依頼を二度送っても一件だけ登録される");
    expect(html).toContain("未判定");
    expect(html).toContain("実行未確認");
  },
);

test("the definition list sits in the sidebar and the judgment pane waits for a selection", () => {
  const html = renderToStaticMarkup(
    <Inspections
      native={false}
      api=""
      user="u"
      workspace="w"
      role="editor"
      canExecute
    />,
  );
  expect(html).toContain('aria-label="検品定義一覧"');
  expect(html).toContain("IDと版を指定して取得");
  expect(html).toContain("検品定義を新規作成");
  expect(html).not.toContain("選択した検品の判定");
});
