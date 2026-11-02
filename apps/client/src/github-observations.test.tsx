import { renderToStaticMarkup } from "react-dom/server";
import { expect, it } from "vitest";
import { GithubSnapshot, ObservationDetail } from "./github-observations";

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
  expect(html).toContain("CIの結論：success");
  expect(html).toContain("指定commit：不一致");
  expect(html).toContain("required：completed / skipped");
  expect(html).toContain("missing：未取得・対象ジョブなし");
  expect(html).toContain("2回目");
  expect(html).not.toContain("PASS");
});

it("shows the selected observation's times, snapshot, source link and history in one detail section", () => {
  const snapshot = {
    url: "https://github.com/fixture/repo/pull/5",
    source_version: "v1",
    state: "open",
    title: "fixture",
  };
  const html = renderToStaticMarkup(
    <ObservationDetail
      native={false}
      item={{
        target: {
          id: "target:one",
          repository: "fixture/repo",
          repository_id: 1,
          kind: "pull_request",
          external_id: "5",
        },
        observation_state: "observed",
        checked_at: "2026-01-02T03:04:05Z",
        observed_at: "2026-01-02T03:04:05Z",
        due_at: "2026-01-02T03:19:05Z",
        delayed: false,
        version: 3,
        snapshot,
      }}
      history={{
        items: [{ version: 2, observed_at: "2026-01-01T00:00:00Z", snapshot }],
        next_cursor: "cursor",
      }}
      onHistory={() => {}}
    />,
  );
  expect(html).toContain('aria-label="選択した観測の詳細"');
  expect(html).toContain("<h4>fixture/repo / pull_request 5</h4>");
  expect(html).toContain("最終確認：");
  expect(html).toContain("次回照合予定：");
  expect(html).toContain('href="https://github.com/fixture/repo/pull/5"');
  expect(html).toContain("最新の履歴を取得");
  expect(html).toContain("観測版 2 / ");
  expect(html).toContain("履歴の前のページ");
});
