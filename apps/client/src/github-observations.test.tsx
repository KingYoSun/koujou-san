import { renderToStaticMarkup } from "react-dom/server";
import { expect, it } from "vitest";
import { GithubSnapshot } from "./github-observations";

it("keeps a successful source conclusion separate from the expected commit and required skipped job", () => {
  const html = renderToStaticMarkup(
    <GithubSnapshot
      value={{
        url: "https://github.com/fixture/repo/actions/runs/123",
        source_version: "fixed-version",
        state: "completed",
        commit: "a".repeat(40),
        workflow_id: 42,
        run_id: 123,
        run_attempt: 2,
        conclusion: "success",
        jobs: [
          {
            id: 7,
            name: "required",
            status: "completed",
            conclusion: "skipped",
            run_attempt: 2,
          },
        ],
      }}
      expected={"b".repeat(40)}
      required={["required", "missing"]}
    />,
  );
  expect(html).toContain("CIの結論: success");
  expect(html).toContain("指定commitと不一致");
  expect(html).toContain("required: completed / skipped");
  expect(html).toContain("missing: 未取得・対象ジョブなし");
  expect(html).toContain("2回目");
  expect(html).not.toContain("PASS");
});
