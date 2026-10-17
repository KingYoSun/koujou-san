import { afterEach, describe, expect, it, vi } from "vitest";
import {
  clearModelKeys,
  loadProfiles,
  modelEndpoint,
  modelRequest,
  saveProfile,
  type ModelProfile,
} from "./model-connections";
const make = (): ModelProfile => ({
  id: "owned",
  name: "owned",
  endpoint: "https://gateway.example/v1",
  model: "fixed",
  allowed_models: ["fixed"],
  auth: "bearer",
  revision: "",
  contract: "",
  prices: {},
  valuation: "estimated",
});
function storage() {
  const entries = new Map<string, string>();
  vi.stubGlobal("localStorage", {
    getItem: (k: string) => entries.get(k) ?? null,
    setItem: (k: string, v: string) => entries.set(k, v),
  });
  return entries;
}
afterEach(() => {
  clearModelKeys();
  vi.unstubAllGlobals();
});
describe("client model credentials", () => {
  it("keeps keys out of persistent metadata and separates them from app credentials", async () => {
    const entries = storage();
    const p = await saveProfile(false, "actor", make(), "owned-key");
    expect([...entries.values()].join("")).not.toContain("owned-key");
    expect((await loadProfiles(false, "actor"))[0].revision).toBe(p.revision);
    const fetch = vi.fn<typeof globalThis.fetch>(
      async () =>
        new Response(JSON.stringify({ model: "fixed" }), { status: 200 }),
    );
    vi.stubGlobal("fetch", fetch);
    await modelRequest(false, "actor", p, { model: "fixed" }, 1000);
    expect(fetch.mock.calls[0]?.[1]).toMatchObject({
      credentials: "omit",
      redirect: "error",
      headers: { Authorization: "Bearer owned-key" },
    });
  });
  it("does not send the new key through a stale endpoint snapshot", async () => {
    storage();
    const old = await saveProfile(false, "actor", make(), "old-owned-key");
    const changed = await saveProfile(
      false,
      "actor",
      { ...old, endpoint: "https://another.example/v1" },
      "new-owned-key",
    );
    const fetch = vi.fn<typeof globalThis.fetch>();
    vi.stubGlobal("fetch", fetch);
    await expect(
      modelRequest(false, "actor", old, { model: "fixed" }, 1000),
    ).rejects.toThrow("model_connection_changed");
    expect(fetch).not.toHaveBeenCalled();
    expect(changed.revision).not.toBe(old.revision);
  });
  it("requires reentry after signout and isolates another user's memory", async () => {
    storage();
    const p = await saveProfile(false, "alice", make(), "owned-key");
    const fetch = vi.fn<typeof globalThis.fetch>();
    vi.stubGlobal("fetch", fetch);
    await expect(
      modelRequest(false, "bob", p, { model: "fixed" }, 1000),
    ).rejects.toThrow();
    clearModelKeys();
    await expect(
      modelRequest(false, "alice", p, { model: "fixed" }, 1000),
    ).rejects.toThrow("model_key_required");
    expect(fetch).not.toHaveBeenCalled();
  });
  it("supports an explicitly unauthenticated local endpoint without a dummy key", async () => {
    storage();
    const p = await saveProfile(
      false,
      "actor",
      { ...make(), endpoint: "http://127.0.0.1:2099/v1", auth: "none" },
      "",
    );
    const fetch = vi.fn<typeof globalThis.fetch>(
      async (_input: RequestInfo | URL, _init?: RequestInit) =>
        new Response("{}", { status: 200 }),
    );
    vi.stubGlobal("fetch", fetch);
    await modelRequest(false, "actor", p, { model: "fixed" }, 1000);
    expect(fetch.mock.calls[0]?.[1]).toMatchObject({
      headers: { "Content-Type": "application/json" },
    });
    expect(
      (fetch.mock.calls[0]?.[1] as RequestInit).headers,
    ).not.toHaveProperty("Authorization");
  });
  it("rejects URL credentials, nonlocal plaintext and a different requested model", async () => {
    for (const value of [
      "http://remote.example/v1",
      "https://user:secret@example.test/v1",
      "https://example.test/v1?key=x",
    ])
      expect(() => modelEndpoint(value)).toThrow();
    storage();
    const p = await saveProfile(false, "actor", make(), "owned-key");
    await expect(
      modelRequest(false, "actor", p, { model: "auto" }, 1000),
    ).rejects.toThrow("model_connection_changed");
  });
});
