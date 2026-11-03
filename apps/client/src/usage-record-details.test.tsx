import { renderToStaticMarkup } from "react-dom/server";
import { afterEach, expect, it, vi } from "vitest";
import { NavigationProvider, readRoute } from "./navigation";
import { UsageRecordDetails } from "./usage-record-details";

afterEach(() => vi.unstubAllGlobals());
it("links only the source targets actually recorded, preserving each fixed version", () => {
  vi.stubGlobal("location", { search: "?workspace=one&screen=usage" });
  const origins = [
    { kind: "model-connection", id: "model", version: "model-fixed" },
    { kind: "sentence", id: "context", version: "90071992547409931234" },
    {
      kind: "workflow-definition",
      id: "workflow-definition:one",
      version: "definition-fixed",
    },
  ];
  const html = renderToStaticMarkup(
    <NavigationProvider api="api" user="alice" workspace="one">
      <UsageRecordDetails
        record={{
          id: "price:one",
          version: "usage-fixed",
          kind: "price",
          value: {},
          origins,
        }}
        onRead={() => {}}
      />
    </NavigationProvider>,
  );
  const routes = [...html.matchAll(/href="([^"]+)"/g)].map((match) =>
    readRoute(match[1].replaceAll("&amp;", "&"), "one"),
  );
  expect(routes.map((route) => route.target)).toEqual(origins);
  expect(routes.map((route) => route.screen)).toEqual([
    "models",
    "context",
    "workflows",
  ]);
  const missing = renderToStaticMarkup(
    <UsageRecordDetails
      record={{
        id: "price:one",
        version: "usage-fixed",
        kind: "price",
        value: {},
      }}
      onRead={() => {}}
    />,
  );
  expect(missing).not.toContain("href=");
});
