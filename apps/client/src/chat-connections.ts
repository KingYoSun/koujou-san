import { useEffect, useRef, useState } from "react";
import { mcpIdentity } from "./auth";
import {
  loadProfiles,
  modelReady,
  type ModelProfile,
} from "./model-connections";
import {
  chatPreferenceKey,
  chatPreferences,
  rememberChat,
} from "./chat-preferences";
import type { OperationAddress } from "./client-operation-port";

export function useChatConnections(
  { native, api, user, workspace }: OperationAddress,
  visible: boolean,
  onError: (text: string) => void,
) {
  const key = chatPreferenceKey(api, user, workspace);
  const [profile, setProfile] = useState<ModelProfile>();
  const [mcp, setMCP] = useState("確認中");
  const alive = useRef(true);
  function selectModel(value: ModelProfile) {
    setProfile(value);
    rememberChat(key, { model: value.id });
  }
  async function checkMCP() {
    setMCP("確認中");
    try {
      const id = await mcpIdentity(native),
        ready = id === user;
      if (alive.current)
        setMCP(ready ? "認証済み" : id ? "別アカウント" : "未接続");
      return ready;
    } catch {
      if (alive.current) setMCP("確認失敗");
      return false;
    }
  }
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
    };
  }, []);
  useEffect(() => {
    if (!visible) return;
    let active = true;
    const restore = () => {
      void checkMCP();
      void loadProfiles(native, api + "\0" + user, workspace)
        .then((profiles) => {
          if (!active) return;
          const selected =
            profiles.find((p) => p.id === chatPreferences(key).model) ??
            profiles.find((p) => modelReady(native, api + "\0" + user, p));
          setProfile(selected);
          if (selected) rememberChat(key, { model: selected.id });
        })
        .catch(() => {
          if (active) onError("モデル接続を取得できませんでした。");
        });
    };
    restore();
    window.addEventListener("focus", restore);
    return () => {
      active = false;
      window.removeEventListener("focus", restore);
    };
  }, [visible, native, key]);
  return { profile, selectModel, mcp, checkMCP };
}
