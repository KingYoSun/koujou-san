import { expect, test } from "vitest";
import { priorityEvidenceLabel } from "./automation";

test("a user can choose the displayed recommendation's evidence by reason instead of internal identifiers", () => {
  const record = {
    job: "internal-job",
    number: 2,
    state: "evaluated",
    decision: { priority: "P1", reason: "公開中の作業が停止している" },
  };
  const label = priorityEvidenceLabel(record, {
    job: "internal-job",
    recommendation: "P1",
  });
  for (const text of [
    "現在の推奨の根拠",
    "推奨 P1",
    "公開中の作業が停止している",
    "試行2",
  ])
    expect(label).toContain(text);
  expect(label).not.toContain("internal-job");
  expect(
    priorityEvidenceLabel(record, { job: "other-job", recommendation: "P1" }),
  ).not.toContain("現在の推奨の根拠");
});
test("an unknown evaluation shows the missing evidence without inventing a priority", () => {
  const label = priorityEvidenceLabel({
    state: "unknown",
    number: 1,
    reason: "影響範囲の証拠が不足",
  });
  expect(label).toContain("影響範囲の証拠が不足");
  expect(label).not.toContain("推奨 P");
  expect(label).not.toContain("現在の推奨の根拠");
});
