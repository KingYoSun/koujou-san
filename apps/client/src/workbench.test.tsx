import { beforeEach, expect, test, vi } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { Workbench, WorkState, nextAction } from "./workbench";
import { PauseResponse, QuestionView, submitPauseAnswer } from "./run-pause";
import { syncRequest } from "./auth";
import { saveAnswerDraft } from "./sync-store";

vi.mock("./auth", async (original) => ({
  ...(await original<typeof import("./auth")>()),
  syncRequest: vi.fn(),
}));
vi.mock("./sync-store", async (original) => ({
  ...(await original<typeof import("./sync-store")>()),
  saveAnswerDraft: vi.fn().mockResolvedValue(undefined),
}));
beforeEach(() => {
  vi.mocked(syncRequest).mockReset();
  vi.mocked(saveAnswerDraft).mockClear();
});

test("a viewer on the Web is offered reading without command editing or native execution controls", () => {
  const html = renderToStaticMarkup(
    <Workbench
      native={false}
      api="fixture"
      workspace="one"
      user="viewer"
      role="viewer"
      canExecute={false}
      members={[]}
    />,
  );
  expect(html).toContain("作業一覧を更新");
  for (const text of [
    "新しいRunを作成",
    "command・接続先",
    "この端末で割当を確認・準備",
    "ローカル実行の接続設定",
  ])
    expect(html).not.toContain(text);
});

test("execution, unknown observation, inspection verdict and local synchronization stay separate", () => {
  const cases = [
    [{ state: "running", observation: "known" }, "実行中"],
    [{ state: "paused", observation: "known" }, "質問・条件待ち"],
    [
      { state: "running", observation: "unknown" },
      "Unknown（所在・継続は未確認）",
    ],
    [
      { state: "succeeded", assessment: { state: "valid", verdict: "FAIL" } },
      "検品：FAIL",
    ],
    [
      {
        state: "succeeded",
        assessment: { state: "valid", verdict: "INCONCLUSIVE" },
      },
      "検品：INCONCLUSIVE",
    ],
    [{ state: "queued" }, "未実行"],
  ] as const;
  for (const [value, expected] of cases)
    expect(renderToStaticMarkup(<WorkState value={value} />)).toContain(
      expected,
    );
  expect(
    renderToStaticMarkup(<WorkState value={{ state: "paused" }} unsent />),
  ).toContain("未送信（端末内の回答案）");
  const cached = renderToStaticMarkup(
    <WorkState value={{ state: "succeeded" }} stale />,
  );
  expect(cached).toContain("古いキャッシュ");
  expect(cached).toContain("同期確定は未確認");
  expect(nextAction({ state: "running", observation: "unknown" })).toContain(
    "所在を照合",
  );
  expect(
    nextAction({
      state: "succeeded",
      assessment: { state: "valid", verdict: "FAIL" },
    }),
  ).toContain("差し戻し");
});
const request = {
  question: "保存後の案内を読めますか？",
  background: "固定ビルドの表示確認です。",
  instructions: "成果物を開いてラベルを確認してください。",
  waiting_for: "画面の受入",
  expertise: "普段の利用者として判断できます。",
  response_format: "verdict",
  target: {
    label: "確認対象の画面",
    revision: "build-v1",
    uri: "https://example.test/build",
  },
  criteria: [{ id: "AC-label", requirement: "保存して戻ると表示される" }],
  options: [
    {
      value: "FAIL",
      label: "条件を満たさない",
      impact: "修正と再検品を判断します。",
    },
    {
      value: "INCONCLUSIVE",
      label: "判断できない",
      impact: "未確認として残します。",
    },
  ],
};
test("a review request provides its purpose, fixed target, instructions, choices and impacts without requiring raw JSON", () => {
  const html = renderToStaticMarkup(<QuestionView value={request} />);
  for (const value of [
    request.question,
    request.background,
    request.instructions,
    request.waiting_for,
    "build-v1",
    "保存して戻る",
    "条件を満たさない",
    "修正と再検品",
    "判断できない",
  ])
    expect(html).toContain(value);
  expect(html).toContain('href="https://example.test/build"');
  expect(
    renderToStaticMarkup(<QuestionView value="既存の自由文の質問" />),
  ).toContain("既存の自由文の質問");
  const response = renderToStaticMarkup(
    <PauseResponse
      request={request}
      value={{
        verdict: "INCONCLUSIVE",
        reason: "倍率が未確認",
        evidence: "https://example.test/note",
      }}
    />,
  );
  expect(response).toContain("判断：INCONCLUSIVE");
  expect(response).toContain("理由：倍率が未確認");
});
const namespace = { api: "fixture", user: "editor", workspace: "one" };
const record = {
  id: "run-pause:build:review",
  version: "pause-v1",
  value: { id: "review" },
};
test("the shared answer path records a response without claiming delivery or resumption", async () => {
  vi.mocked(syncRequest)
    .mockResolvedValueOnce({
      version: "run-v2",
      value: {
        state: "paused",
        active_pause: { id: record.id, version: record.version },
      },
    })
    .mockResolvedValueOnce({ data: { id: "run:build", version: "accepted" } });
  await submitPauseAnswer(
    false,
    namespace,
    "run:build",
    record,
    {
      pauseVersion: "pause-v1",
      value: '{"verdict":"INCONCLUSIVE","reason":"未確認"}',
      json: true,
    },
    "same-operation",
  );
  expect(syncRequest).toHaveBeenCalledTimes(2);
  expect(vi.mocked(syncRequest).mock.calls[1][2]).toEqual({
    method: "POST",
    body: {
      operation_id: "same-operation",
      expected_version: "run-v2",
      pause_id: "review",
      value: { verdict: "INCONCLUSIVE", reason: "未確認" },
    },
  });
  expect(saveAnswerDraft).toHaveBeenCalledWith(false, namespace, record.id);
});
test("a changed question keeps the local draft and sends no answer", async () => {
  vi.mocked(syncRequest).mockResolvedValueOnce({
    version: "new-run",
    value: {
      state: "paused",
      active_pause: { id: record.id, version: "new-pause" },
    },
  });
  await expect(
    submitPauseAnswer(
      false,
      namespace,
      "run:build",
      record,
      { pauseVersion: "pause-v1", value: "answer", json: false },
      "operation",
    ),
  ).rejects.toThrow("質問の状態が変わりました");
  expect(syncRequest).toHaveBeenCalledTimes(1);
  expect(saveAnswerDraft).not.toHaveBeenCalled();
});
