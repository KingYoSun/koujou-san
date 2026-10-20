import { ScreenContent } from "./navigation";
import { useEffect, useMemo, useReducer, useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useStore } from "zustand";
import { SyncEngine } from "./sync-engine";
import { syncStorage } from "./sync-store";
import { syncRequest } from "./auth";
import { editorStore, identityKey, requester } from "./context-ui";
import type { UI } from "./context-ui";
import { ContextExplorer } from "./context-explorer";
import { ContextEditor } from "./context-editor";
import { ContextPreview } from "./context-preview";
import {
  ContextGeneration,
  type OutputContextSource,
} from "./context-generation";
import { ClientSelection } from "./client-selection";
export function ContextWorkspace({
  native,
  api,
  workspace,
  user,
  role,
  onRevoked,
  source,
  onStatus,
}: {
  native: boolean;
  api: string;
  workspace: string;
  user: string;
  role: string;
  onRevoked: () => void;
  source?: OutputContextSource;
  onStatus?: (status: string) => void;
}) {
  const [, render] = useReducer((n) => n + 1, 0);
  const [engine, setEngine] = useState<SyncEngine>();
  const [error, setError] = useState("");
  const store = useMemo(() => editorStore(), [api, user, workspace]);
  const selected = useStore(store, (state) => state.selected);
  const client = useQueryClient();
  const prefix = useMemo(
    () => ["context-ui", api, user, workspace],
    [api, user, workspace],
  );
  const callback = useRef(onRevoked);
  callback.current = onRevoked;
  useEffect(() => {
    let notified = false;
    let accessEpoch = 0;
    const current = new SyncEngine(
      { api, user, workspace },
      syncStorage(native),
      (path, method, body) => syncRequest(path, native, { method, body }),
      () => {
        render();
        if (current.accessEpoch !== accessEpoch) {
          accessEpoch = current.accessEpoch;
          store.setState({ permissionEpoch: accessEpoch });
          void client.cancelQueries({ queryKey: [...prefix, "body"] });
          client.removeQueries({ queryKey: [...prefix, "body"] });
        }
        if (current.status === "revoked" && !notified) {
          notified = true;
          void client.cancelQueries({ queryKey: prefix });
          client.removeQueries({ queryKey: prefix });
          store.setState({
            selected: undefined,
            historical: undefined,
            reference: undefined,
            include: [],
            exclude: [],
          });
          callback.current();
        }
      },
    );
    setEngine(current);
    void current.start();
    const timer = setInterval(() => void current.refresh(), 2000);
    const online = () => void current.refresh();
    window.addEventListener("online", online);
    return () => {
      clearInterval(timer);
      window.removeEventListener("online", online);
      void current.stop();
      void client.cancelQueries({ queryKey: prefix });
      client.removeQueries({ queryKey: prefix });
    };
  }, [api, native, user, workspace, client, prefix, store]);
  const record = selected && engine?.state.items[identityKey(selected)]?.record;
  useEffect(() => {
    if (record)
      client.setQueryData([...prefix, "body", record.ref], { data: record });
    else if (
      engine?.status === "current" &&
      selected &&
      !engine.state.drafts[identityKey(selected)]
    ) {
      void client.cancelQueries({ queryKey: [...prefix, "body"] });
      client.removeQueries({ queryKey: [...prefix, "body"] });
      store.setState({ historical: undefined });
    }
  }, [
    record?.ref.revision,
    selected?.id,
    engine?.status,
    client,
    prefix,
    store,
  ]);
  useEffect(
    () =>
      onStatus?.(
        engine?.status === "current"
          ? "接続確認済み"
          : engine?.status === "revoked"
            ? "権限失効"
            : engine?.status === "offline"
              ? "オフライン"
              : "接続確認中",
      ),
    [engine?.status, onStatus],
  );
  if (!engine)
    return (
      <ScreenContent>
        <p role="status">端末内の下書きを確認しています…</p>
      </ScreenContent>
    );
  const ui: UI = {
    native,
    api,
    user,
    workspace,
    engine,
    store,
    prefix,
    canCreate: role === "admin" || role === "editor",
    request: requester(native),
    fail: (error) => {
      setError(String(error));
      void engine.refresh();
    },
  };
  return (
    <>
      <ScreenContent>
        {
          <section
            id="cost-context-editor"
            tabIndex={-1}
            className="context-workspace"
            aria-label="コンテキスト管理"
          >
            <header className="context-heading">
              <div>
                <p className="eyebrow">CONTEXT LIBRARY</p>
                <h2>コンテキスト</h2>
                <p>
                  文書を組み立て、Scopeで整理し、入力する固定版を確認します。
                </p>
              </div>
              <div className="context-connection">
                <strong>
                  {engine.status === "current"
                    ? "接続確認済み"
                    : engine.status === "checking"
                      ? "接続確認中"
                      : engine.status === "revoked"
                        ? "権限失効"
                        : "オフライン・保存済み表示"}
                </strong>
                <small>最終確認: {engine.state.confirmedAt ?? "未確認"}</small>
                <button
                  disabled={engine.busy}
                  onClick={() => void engine.refresh()}
                >
                  接続を確認
                </button>
              </div>
            </header>
            <p className="context-meta">
              {native
                ? "Windowsアプリ · OS連携はネイティブ側で扱います。"
                : "Web · ローカルCLIの実行能力はありません。"}{" "}
              既存下書きは対象単位の保存へ移行します。移行後は旧版アプリと混在させないでください。
            </p>
            {engine.notice && (
              <p role="status" className="context-warning">
                {engine.notice}
              </p>
            )}
            {error && (
              <p role="alert">
                {error}
                <button onClick={() => setError("")}>閉じる</button>
              </p>
            )}
            <div className="context-layout">
              <ContextExplorer ui={ui} />
              <ContextEditor ui={ui} />
              <ContextPreview ui={ui} />
            </div>
            <div id="cost-model-editor" tabIndex={-1}></div>
          </section>
        }
      </ScreenContent>
      <ClientSelection key={`${api}/${user}/${workspace}`} ui={ui} />
      <ContextGeneration
        key={`generation:${api}/${user}/${workspace}`}
        ui={ui}
        source={source}
      />
    </>
  );
}
