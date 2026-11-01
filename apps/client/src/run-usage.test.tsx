import { beforeEach, expect, test, vi } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { UsageEntryView, queryRunUsage, type UsageEntry } from "./run-usage";
import { syncRequest } from "./auth";

vi.mock("./auth", async (original) => ({
  ...(await original<typeof import("./auth")>()),
  syncRequest: vi.fn(),
}));
beforeEach(() => vi.mocked(syncRequest).mockReset());
const entry = (): UsageEntry => ({
  key: "one",
  group: "run:one/model",
  kind: "usage",
  state: "confirmed",
  amount: null,
  currency: "",
  refs: [],
  quantities: {
    input: { value: "12", state: "reported", unit: "token" },
    output: { value: null, state: "missing", unit: "token" },
  },
  value: {
    actual_model: "model-fixed",
    measurement: {
      state: "final",
      protocol: "claude-jsonl",
      cli_version: "2.1.300",
      billing: "subscription",
      scope: "pipeline",
      estimate_usd: "0.0123",
      source_ref: "runner-log://owned",
      source_digest: "digest",
    },
  },
});

test("subscription estimates, missing quantities and settlement are separate", () => {
  const html = renderToStaticMarkup(<UsageEntryView entry={entry()} />);
  for (const text of [
    "model-fixed",
    "終了時取得",
    "12 token",
    "未取得",
    "定額契約",
    "CLIの参考推定額",
    "0.0123 USD",
  ])
    expect(html).toContain(text);
  expect(html).not.toContain("0 USD");
});

test("session totals with uncertain boundaries do not become Run quantities or prices", () => {
  const value = entry();
  value.state = "review_needed";
  value.value.measurement!.reason = "session_boundary_unconfirmed";
  const html = renderToStaticMarkup(<UsageEntryView entry={value} />);
  expect(html).toContain("今回のRunの消費を切り分けられない");
  expect(html).not.toContain("12 token");
  expect(html).not.toContain("0.0123 USD");
});

test("intermediate, unsupported, non-LLM and pending valuations stay distinct", () => {
  for (const [state, expected] of [
    ["provisional", "途中更新・暫定"],
    ["unavailable", "取得不可"],
    ["not_applicable", "LLM非該当"],
    ["partial", "一部取得"],
  ]) {
    const value = entry();
    value.value.measurement!.state = state;
    expect(renderToStaticMarkup(<UsageEntryView entry={value} />)).toContain(
      expected,
    );
  }
  const value = entry();
  value.kind = "estimated";
  value.state = "pending";
  const html = renderToStaticMarkup(<UsageEntryView entry={value} />);
  expect(html).toContain("再評価待ち");
  expect(html).toContain("単価からの推定額：未算定");
});

test("a Run query requests one indexed page and reuses the server cursor", async () => {
  vi.mocked(syncRequest).mockResolvedValue({
    data: { items: [] },
    meta: { next_cursor: null },
  });
  await queryRunUsage(
    false,
    "workspace",
    "run:one",
    "reported",
    "owned-cursor",
  );
  expect(syncRequest).toHaveBeenCalledWith(
    "/usage/workspaces/workspace/query",
    false,
    {
      method: "POST",
      body: {
        mode: "run",
        kind: "reported",
        attribution_id: "run:one",
        cursor: "owned-cursor",
        limit: 50,
      },
    },
  );
});
