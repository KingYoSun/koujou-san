import { expect, test } from "vitest";
import { utf8Range } from "./chat-api";

test("selected Japanese text keeps the source UTF-8 range", () => {
  const text = "相談です。採用する仕様。余談です。";
  const range = utf8Range(
    text,
    "相談です。".length,
    "相談です。採用する仕様。".length,
  );
  expect(range).toEqual({ start_utf8: 15, end_utf8: 36 });
  expect(
    new TextDecoder().decode(
      new TextEncoder().encode(text).slice(range.start_utf8, range.end_utf8),
    ),
  ).toBe("採用する仕様。");
});
