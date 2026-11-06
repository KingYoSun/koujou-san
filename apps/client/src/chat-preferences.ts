import type { VersionRef } from "./model-connections";

type Preferences = {
  project?: VersionRef;
  thread?: VersionRef;
  model?: string;
};
export const chatPreferenceKey = (
  api: string,
  user: string,
  workspace: string,
) => `koujou-chat:${JSON.stringify([api, user, workspace])}`;
export function chatPreferences(key: string): Preferences {
  try {
    return JSON.parse(localStorage.getItem(key) ?? "{}");
  } catch {
    return {};
  }
}
export function rememberChat(key: string, value: Partial<Preferences>) {
  localStorage.setItem(
    key,
    JSON.stringify({ ...chatPreferences(key), ...value }),
  );
}
