import {
  Screen,
  ScreenContent,
  useNavigation,
  useScreenActive,
} from "./navigation";
import { Tabs } from "./tabs";
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
import { ContextPreview, type DefinitionContextInput } from "./context-preview";
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
  definitionInput,
  onDefinitionInput,
}: {
  native: boolean;
  api: string;
  workspace: string;
  user: string;
  role: string;
  onRevoked: () => void;
  source?: OutputContextSource;
  onStatus?: (status: string) => void;
  definitionInput?: DefinitionContextInput;
  onDefinitionInput?: (value: DefinitionContextInput) => void;
}) {
  const navigation = useNavigation();
  const visible = useScreenActive();
  const mode = ["documents", "scopes", "input", "generation"].includes(
    navigation?.route.view ?? "",
  )
    ? navigation!.route.view!
    : "documents";
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
  const navigationRef = useRef(navigation);
  navigationRef.current = navigation;
  const restoring = useRef(false);
  useEffect(
    () =>
      store.subscribe((next, previous) => {
        if (
          restoring.current ||
          (next.selected === previous.selected &&
            next.historical === previous.historical)
        )
          return;
        const nav = navigationRef.current;
        if (
          nav?.route.screen !== "context" ||
          nav.route.view === "generation" ||
          !next.selected
        )
          return;
        const fixed =
          next.historical ??
          engine?.state.items[identityKey(next.selected)]?.record.ref;
        nav.navigate(
          {
            ...nav.route,
            target: {
              kind: next.selected.kind,
              id: next.selected.id,
              version: fixed?.revision ?? "head",
            },
          },
          {
            focus: nav.route.view === "input" ? false : "context-editor-title",
          },
        );
      }),
    [store, engine],
  );
  useEffect(() => {
    const target = navigation?.route.target;
    if (
      !visible ||
      navigation?.route.screen !== "context" ||
      !target ||
      ![
        "sentence",
        "paragraph",
        "section",
        "document",
        "scope",
        "membership",
      ].includes(target.kind)
    )
      return;
    const selected = { kind: target.kind, id: target.id };
    const fixed = engine?.state.items[identityKey(selected)]?.record.ref;
    restoring.current = true;
    store.setState({
      selected,
      historical:
        target.version !== "head" && target.version !== fixed?.revision
          ? { ...selected, revision: target.version }
          : undefined,
    });
    restoring.current = false;
  }, [
    visible,
    navigation?.route.target?.id,
    navigation?.route.target?.kind,
    navigation?.route.target?.version,
    engine,
    store,
  ]);
  useEffect(() => {
    const nav = navigationRef.current;
    const state = store.getState();
    if (
      !record ||
      !visible ||
      nav?.route.screen !== "context" ||
      nav.route.view === "generation" ||
      nav.route.target?.id !== record.ref.id
    )
      return;
    if (state.historical?.revision === record.ref.revision) {
      restoring.current = true;
      store.setState({ historical: undefined });
      restoring.current = false;
    }
    if (!state.historical)
      nav.navigate(
        {
          ...nav.route,
          target: {
            kind: record.ref.kind,
            id: record.ref.id,
            version: record.ref.revision,
          },
        },
        { replace: true },
      );
  }, [record?.ref.revision, visible, store]);
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
              <details>
                <summary>
                  Contextの同期・保存について（
                  {engine.status === "current"
                    ? "同期確認済み"
                    : engine.status === "revoked"
                      ? "権限失効"
                      : engine.status === "offline"
                        ? "オフライン"
                        : "接続確認中"}
                  ）
                </summary>
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
                  <small>
                    最終確認: {engine.state.confirmedAt ?? "未確認"}
                  </small>
                  <button
                    disabled={engine.busy}
                    onClick={() => void engine.refresh()}
                  >
                    接続を確認
                  </button>
                </div>
              </details>
            </header>
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
            <Tabs
              id="context-mode"
              label="Contextの作業"
              items={[
                { id: "documents", label: "文書を探す・編集" },
                { id: "scopes", label: "Scopeで整理" },
                { id: "input", label: "Run入力を選択" },
                { id: "generation", label: "出力から反映" },
              ]}
              value={mode}
              onChange={(view) =>
                navigation?.navigate(
                  { ...navigation.route, view },
                  { replace: true },
                )
              }
            />
            {navigation?.route.origin && (
              <details>
                <summary>呼び出し元の固定版</summary>
                <p>
                  呼び出し元：{navigation.route.origin.kind} ·{" "}
                  {navigation.route.origin.id} ·{" "}
                  {navigation.route.origin.version}
                </p>
              </details>
            )}
            <div id="cost-model-editor" tabIndex={-1}></div>
          </section>
        }
      </ScreenContent>
      <section
        hidden={!visible}
        role="tabpanel"
        id={`context-mode-panel-${mode}`}
        aria-labelledby={`context-mode-tab-${mode}`}
        tabIndex={0}
      >
        <div
          className="context-layout"
          hidden={!visible}
          data-mode={mode}
          role="region"
          aria-label="Contextの作業領域"
        >
          <ContextExplorer ui={ui} active={mode !== "generation"} mode={mode} />
          <ContextEditor
            ui={ui}
            active={
              mode === "documents" ||
              (mode === "scopes" &&
                ["scope", "membership"].includes(selected?.kind ?? ""))
            }
          />
          <ContextPreview
            ui={ui}
            active={mode === "input"}
            definitionInput={definitionInput}
            onDefinitionInput={onDefinitionInput}
          />
        </div>
        <Screen screens={["context"]} enabled={mode === "input"} background>
          <ClientSelection key={api + user + workspace} ui={ui} />
        </Screen>
        <Screen
          screens={["context"]}
          enabled={mode === "generation"}
          background
        >
          <ContextGeneration
            key={`generation:${api}/${user}/${workspace}`}
            ui={ui}
            source={source}
          />
        </Screen>
      </section>
    </>
  );
}
