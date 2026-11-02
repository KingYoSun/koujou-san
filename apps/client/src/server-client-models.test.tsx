import { afterEach, expect, it, vi } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { ModelConnectionSettings } from "./model-connection-settings";
import {
  loadProfiles,
  modelReady,
  modelRequest,
  modelSnapshot,
  saveProfile,
  type ModelProfile,
} from "./model-connections";

const request = vi.hoisted(() => vi.fn());
vi.mock("./auth", () => ({ syncRequest: request }));
afterEach(() => {
  vi.unstubAllGlobals();
  request.mockReset();
});
const profile: ModelProfile = {
  id: "server:plan:owned",
  name: "Owned plan",
  endpoint: "https://app.example",
  model: "owned",
  allowed_models: ["owned"],
  auth: "none",
  server_connection: "plan",
  server_workspace: "one",
  revision: "server-version",
  contract: "",
  prices: {},
  valuation: "estimated",
  cost: null,
};

it("loads only the current Workspace's server choices and keeps direct connections and private keys separate", async () => {
  const entries = new Map<string, string>();
  vi.stubGlobal("localStorage", {
    getItem: (key: string) => entries.get(key) ?? null,
    setItem: (key: string, value: string) => entries.set(key, value),
  });
  const actor = "https://app.example\0alice";
  const direct = await saveProfile(
    false,
    actor,
    {
      ...profile,
      id: "direct",
      name: "Direct",
      server_connection: undefined,
      server_workspace: undefined,
    },
    "",
  );
  request.mockResolvedValue([
    {
      id: "plan",
      name: "Owned plan",
      revision: "server-version",
      models: [{ requested: "owned", allowed_actual: ["owned"] }],
    },
  ]);
  const profiles = await loadProfiles(false, actor, "one");
  expect(profiles.map((p) => p.id)).toEqual([direct.id, profile.id]);
  expect(request).toHaveBeenCalledWith(
    "/hopper/workspaces/one/model-connections",
    false,
    { method: "GET" },
  );
  expect(profiles[1]).toEqual(profile);
  expect(modelReady(false, actor, profiles[1])).toBe(true);
  expect(modelReady(true, actor, profiles[1])).toBe(false);
  expect(modelSnapshot(profiles[1], "one")).toMatchObject({
    server_connection: "plan",
    connection: profile.id,
    revision: profile.revision,
  });
  expect(() => modelSnapshot(profiles[1], "two")).toThrow(
    "server_model_workspace_changed",
  );
  const saved = await saveProfile(
    false,
    actor,
    { ...profiles[1], contract: "owned tariff" },
    "",
  );
  expect(saved.revision).toBe(profile.revision);
  expect((await loadProfiles(false, actor, "one"))[1].contract).toBe(
    "owned tariff",
  );
  request.mockResolvedValue([]);
  expect((await loadProfiles(false, actor, "two")).map((p) => p.id)).toEqual([
    direct.id,
  ]);
  expect([...entries.values()].join(" ")).not.toMatch(
    /access_token|refresh_token|credential_file/,
  );
});

it("shows server selection and cost settings without provider credentials or direct transport inputs", () => {
  const html = renderToStaticMarkup(
    <ModelConnectionSettings
      native={false}
      actor="https://app.example\0alice"
      workspace="one"
      selected={profile}
      onSelected={() => {}}
    />,
  );
  expect(html).toContain("サーバー接続を再取得");
  expect(html).toContain("Owned plan");
  expect(html).toContain("利用設定を保存");
  expect(html).not.toContain("接続用キー");
  expect(html).not.toContain("接続先URL（必須）");
  expect(html).not.toContain("モデル提供元へログイン");
});

it("never sends a server connection through the direct browser transport", async () => {
  const fetch = vi.fn();
  vi.stubGlobal("fetch", fetch);
  await expect(
    modelRequest(false, "actor", profile, { model: "owned" }, 1000),
  ).rejects.toThrow("server_operation_transport_required");
  expect(fetch).not.toHaveBeenCalled();
});
