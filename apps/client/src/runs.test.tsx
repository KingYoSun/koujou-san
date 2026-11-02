import { renderToStaticMarkup } from "react-dom/server";
import { expect, it } from "vitest";
import { RunStatus } from "./runs";

it("keeps exit code, cancellation and stop capability separate from the state badges", () => {
  const base = {
    input: { id: "run-input:fixture", version: "fixed" },
    state: "succeeded",
    observation: "known",
    reported_through: "13",
    cancel_requested: false,
  };
  const finished = renderToStaticMarkup(
    <RunStatus value={{ ...base, exit_code: 0 }} />,
  );
  expect(finished).toContain("終了コード：0");
  expect(finished).not.toContain("実行：");
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
  expect(disconnected).toContain("取消の完了は確認していません");
  expect(disconnected).not.toContain("取消確認済み");
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
  expect(completedWithoutStop).toContain("停止：未対応");
  expect(completedWithoutStop).not.toContain("実終了を待ちます");
});
