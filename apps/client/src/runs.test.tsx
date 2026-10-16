import { renderToStaticMarkup } from "react-dom/server";
import { expect, it } from "vitest";
import { RunStatus } from "./runs";

it("keeps process success, inspection failure, unknown observation and cancellation separate", () => {
  const base = {
    input: { id: "run-input:fixture", version: "fixed" },
    state: "succeeded",
    observation: "known",
    reported_through: "13",
    cancel_requested: false,
  };
  const failedInspection = renderToStaticMarkup(
    <RunStatus
      value={{
        ...base,
        exit_code: 0,
        assessment: { state: "valid", verdict: "FAIL" },
      }}
    />,
  );
  expect(failedInspection).toContain("正常終了");
  expect(failedInspection).toContain("FAIL");
  expect(failedInspection).toContain("終了コード: 0");
  expect(failedInspection).not.toContain("PASS");
  const disconnected = renderToStaticMarkup(
    <RunStatus
      value={{
        ...base,
        state: "running",
        observation: "unknown",
        cancel_requested: true,
      }}
    />,
  );
  expect(disconnected).toContain("実行中");
  expect(disconnected).toContain("不明");
  expect(disconnected).toContain("取消の完了は確認していません");
  expect(disconnected).not.toContain("取消確認済み");
  const invalid = renderToStaticMarkup(
    <RunStatus value={{ ...base, assessment: { state: "invalid" } }} />,
  );
  expect(invalid).toContain("形式不正");
  expect(invalid).not.toContain("PASS");
  const completedWithoutStop = renderToStaticMarkup(
    <RunStatus
      value={{
        ...base,
        cancel_requested: true,
        execution: {
          environment: "windows",
          version: "fixed",
          version_observed: true,
          pause: false,
          stop: false,
        },
      }}
    />,
  );
  expect(completedWithoutStop).toContain("正常終了");
  expect(completedWithoutStop).toContain("停止: 未対応");
  expect(completedWithoutStop).not.toContain("実終了を待ちます");
});
