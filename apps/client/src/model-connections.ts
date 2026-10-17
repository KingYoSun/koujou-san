import { invoke } from "@tauri-apps/api/core";
export type VersionRef = { id: string; version: string };
export type ModelProfile = {
  id: string;
  name: string;
  endpoint: string;
  model: string;
  allowed_models: string[];
  auth: "bearer" | "none";
  revision: string;
  contract: string;
  prices: Record<string, VersionRef>;
  valuation: string;
  cost?: { amount: string; currency: string; valuation: string } | null;
};
export function modelSnapshot(p: ModelProfile, workspace: string) {
  const prefix = workspace + "\0";
  return {
    connection: p.id,
    revision: p.revision,
    endpoint: p.endpoint,
    requested: p.model,
    allowed: p.allowed_models,
    contract: p.contract,
    valuation: p.valuation,
    cost: p.cost ?? undefined,
    prices: Object.fromEntries(
      Object.entries(p.prices ?? {})
        .filter(([key]) => key.startsWith(prefix))
        .map(([key, value]) => [key.slice(prefix.length), value]),
    ),
  };
}
const keys = new Map<string, { key: string; endpoint: string }>();
const memoryKey = (actor: string, id: string) => `${actor}\0${id}`;
export function clearModelKeys() {
  keys.clear();
}
export function modelReady(
  native: boolean,
  actor: string,
  profile: ModelProfile,
) {
  return (
    native ||
    profile.auth === "none" ||
    keys.get(memoryKey(actor, profile.id))?.endpoint === profile.endpoint
  );
}
export function modelEndpoint(value: string) {
  const u = new URL(value);
  const local = ["localhost", "127.0.0.1", "[::1]"].includes(u.hostname);
  if (
    (u.protocol !== "https:" && !(u.protocol === "http:" && local)) ||
    u.username ||
    u.password ||
    u.search ||
    u.hash
  )
    throw new Error("invalid_model_endpoint");
  return u;
}
export async function loadProfiles(
  native: boolean,
  actor: string,
): Promise<ModelProfile[]> {
  if (native) return invoke("model_connection_load");
  const value = localStorage.getItem(`koujou-model-profiles:${actor}`);
  return value ? (JSON.parse(value) as ModelProfile[]) : [];
}
export async function saveProfile(
  native: boolean,
  actor: string,
  profile: ModelProfile,
  key: string,
) {
  modelEndpoint(profile.endpoint);
  if (native)
    return invoke<ModelProfile>("model_connection_save", { profile, key });
  const profiles = await loadProfiles(false, actor);
  const old = profiles.find((p) => p.id === profile.id);
  if (profile.auth === "bearer") {
    if (key)
      keys.set(memoryKey(actor, profile.id), {
        key,
        endpoint: profile.endpoint,
      });
    else if (
      !old ||
      old.endpoint !== profile.endpoint ||
      keys.get(memoryKey(actor, profile.id))?.endpoint !== profile.endpoint
    )
      throw new Error("model_key_required");
  } else keys.delete(memoryKey(actor, profile.id));
  const saved = { ...profile, revision: crypto.randomUUID() };
  localStorage.setItem(
    `koujou-model-profiles:${actor}`,
    JSON.stringify([...profiles.filter((p) => p.id !== profile.id), saved]),
  );
  return saved;
}
export type ModelHTTP = { status: number; body: unknown };
export async function modelRequest(
  native: boolean,
  actor: string,
  profile: ModelProfile,
  body: unknown,
  timeout_ms: number,
): Promise<ModelHTTP> {
  const input = body as { model?: unknown; stream?: unknown } | null;
  if (
    !input ||
    input.model !== profile.model ||
    input.stream === true ||
    timeout_ms <= 0
  )
    throw new Error("model_connection_changed");
  if (native)
    return invoke("model_connection_request", {
      id: profile.id,
      revision: profile.revision,
      body,
      timeoutMs: timeout_ms,
    });
  const current = (await loadProfiles(false, actor)).find(
    (p) => p.id === profile.id,
  );
  if (
    !current ||
    current.revision !== profile.revision ||
    current.endpoint !== profile.endpoint
  )
    throw new Error("model_connection_changed");
  const target = modelEndpoint(profile.endpoint);
  target.pathname = `${target.pathname.replace(/\/$/, "")}/chat/completions`;
  const headers: Record<string, string> = {
    "Content-Type": "application/json",
  };
  if (profile.auth === "bearer") {
    const stored = keys.get(memoryKey(actor, profile.id));
    if (!stored || stored.endpoint !== profile.endpoint)
      throw new Error("model_key_required");
    headers.Authorization = `Bearer ${stored.key}`;
  }
  const response = await fetch(target, {
    method: "POST",
    headers,
    body: JSON.stringify(body),
    credentials: "omit",
    redirect: "error",
    signal: AbortSignal.timeout(timeout_ms),
  });
  const text = await response.text();
  let value: unknown;
  try {
    value = JSON.parse(text);
  } catch {
    value = text;
  }
  return { status: response.status, body: value };
}
