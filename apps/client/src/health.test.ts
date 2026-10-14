import { afterEach, describe, expect, it, vi } from "vitest";
import { fetchHealth } from "./health";

afterEach(() => vi.unstubAllGlobals());

describe("bootstrap API connection", () => {
  it("connects to the configured API and preserves the cancellation signal", async () => {
    const body = {
      status: "ok",
      service: "koujou-san-api",
      stage: "bootstrap",
    };
    const fetchMock = vi.fn().mockResolvedValue(Response.json(body));
    vi.stubGlobal("fetch", fetchMock);
    const controller = new AbortController();

    await expect(
      fetchHealth("http://127.0.0.1:8080/", controller.signal),
    ).resolves.toEqual(body);
    expect(fetchMock).toHaveBeenCalledWith("http://127.0.0.1:8080/healthz", {
      signal: controller.signal,
    });
  });

  it.each([
    Response.json({ status: "ok" }),
    Response.json({
      status: "ok",
      service: "other-service",
      stage: "bootstrap",
    }),
    new Response("unavailable", { status: 503 }),
  ])(
    "does not report another service or an error response as healthy",
    async (response) => {
      vi.stubGlobal("fetch", vi.fn().mockResolvedValue(response));
      await expect(fetchHealth("http://127.0.0.1:8080")).rejects.toThrow();
    },
  );
});
