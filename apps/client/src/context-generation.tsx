import {
  Screen,
  ScreenContent,
  useNavigation,
  useScreenActive,
} from "./navigation";
import {
  sourceKey,
  sourceRoute,
  pointerPath,
  pathPointer,
} from "./context-generation-source";
import { useEffect, useRef, useState } from "react";
import { syncRequest } from "./auth";
import { editorStore, identityKey, contextPath, type UI } from "./context-ui";
import { ClientSelection } from "./client-selection";
import type { GenerationPolicy } from "./client-selection-host";
import type { ModelProfile, VersionRef } from "./model-connections";
import { allowAnswerScope, answerScopeEpoch } from "./sync-store";
import { ContextGenerationResults } from "./context-generation-results";
import { ContextGenerationAutomation } from "./context-generation-automation";
import { useContextGenerationWorker } from "./context-generation-worker";

export type OutputContextSource = {
  workspace: string;
  run: VersionRef;
  output: VersionRef;
  channel: string;
  path?: string[];
};
type Preview = {
  run: VersionRef;
  output: {
    ref: VersionRef;
    path: string[];
    digest: string;
    selected_utf8: string;
  };
  extras: unknown[];
};
export function ContextGeneration({
  ui,
  source: request,
}: {
  ui: UI;
  source?: OutputContextSource;
}) {
  const navigation = useNavigation();
  const active = useScreenActive();
  const previewEpoch = useRef(0);
  const [store] = useState(() => editorStore());
  const [runID, setRunID] = useState(""),
    [outputID, setOutputID] = useState(""),
    [outputVersion, setOutputVersion] = useState(""),
    [pointer, setPointer] = useState(""),
    [channel, setChannel] = useState("report");
  const [source, setSource] = useState<GenerationPolicy["source"]>(),
    [preview, setPreview] = useState<Preview>(),
    [notice, setNotice] = useState(""),
    [busy, setBusy] = useState(false);
  const [fixed, setFixed] = useState<GenerationPolicy["fixed"]>([]),
    [groupScopes, setGroupScopes] = useState<GenerationPolicy["group_scopes"]>(
      [],
    ),
    [scopeID, setScopeID] = useState("");
  const [profile, setProfile] = useState<ModelProfile>(),
    [result, setResult] = useState<VersionRef>(),
    [initial, setInitial] = useState<VersionRef>();
  const target = useRef<VersionRef>({
    id: `context-result:${crypto.randomUUID()}`,
    version: "0",
  });
  const namespace = { api: ui.api, user: ui.user, workspace: ui.workspace };
  const base = `/client-operations/workspaces/${encodeURIComponent(ui.workspace)}`;
  const workerNotice = useContextGenerationWorker(ui, setResult);
  const route = navigation?.route;
  const routeSource =
    route?.target?.kind === "output" && route.origin?.kind === "run"
      ? {
          run: route.origin,
          output: route.target,
          path: pointerPath(route.query ?? ""),
        }
      : undefined;
  const matchesRoute =
    !routeSource || sourceKey(routeSource) === sourceKey(source);
  async function prepare(input?: OutputContextSource) {
    const attempt = ++previewEpoch.current;
    setBusy(true);
    setNotice("");
    const epoch = answerScopeEpoch(namespace);
    try {
      const run =
        input?.run ??
        ((await syncRequest(
          `/runs/workspaces/${encodeURIComponent(ui.workspace)}/records/${encodeURIComponent(runID)}?version=head`,
          ui.native,
          { method: "GET" },
        )) as VersionRef);
      const output = input?.output ?? { id: outputID, version: outputVersion };
      const selected = {
        run: { id: run.id, version: run.version },
        output: { id: output.id, version: output.version },
        path: input ? (input.path ?? []) : pointerPath(pointer),
      };
      const data = (await syncRequest(base + "/generation/preview", ui.native, {
        method: "POST",
        body: selected,
      })) as Preview;
      if (
        attempt !== previewEpoch.current ||
        !allowAnswerScope(namespace, epoch)
      )
        return;
      setSource(selected);
      setPreview(data);
      if (sourceKey(selected) !== sourceKey(source)) {
        setInitial(undefined);
        target.current = {
          id: `context-result:${crypto.randomUUID()}`,
          version: "0",
        };
      }
      if (!input && navigation)
        navigation.navigate(sourceRoute(ui.workspace, selected), {
          replace: true,
        });
      if (input) {
        setRunID(input.run.id);
        setOutputID(input.output.id);
        setOutputVersion(input.output.version);
        setChannel(input.channel);
        setPointer(pathPointer(input.path ?? []));
      }
    } catch {
      if (attempt !== previewEpoch.current) return;
      setNotice("固定した出力・対象箇所・現在の閲覧権限を確認してください。");
    } finally {
      if (attempt === previewEpoch.current) setBusy(false);
    }
  }
  useEffect(() => {
    if (!active) return;
    const route = navigation?.route;
    if (
      route?.screen === "context" &&
      route.view === "generation" &&
      route.target?.kind === "output" &&
      route.origin?.kind === "run"
    ) {
      if (sourceKey(routeSource) === sourceKey(source) && preview) return;
      setPreview(undefined);
      void prepare({
        workspace: ui.workspace,
        run: route.origin,
        output: route.target,
        channel:
          request?.output.id === route.target.id ? request.channel : "report",
        path: pointerPath(route.query ?? ""),
      });
    } else if (!navigation && request?.workspace === ui.workspace)
      void prepare(request);
  }, [
    request?.run.id,
    request?.run.version,
    request?.output.id,
    request?.output.version,
    request?.workspace,
    active,
    navigation?.route.target?.id,
    navigation?.route.target?.version,
    navigation?.route.origin?.id,
    navigation?.route.origin?.version,
    navigation?.route.query,
  ]);
  async function group() {
    try {
      const data = await ui.request<{
        ref: { kind: "scope"; id: string; revision: string };
        value: { kind: string; name: string };
      }>(
        `${contextPath(ui.workspace, { kind: "scope", id: scopeID })}?revision=head`,
      );
      if (data.data)
        setGroupScopes((current) => [
          ...current.filter((r) => r.id !== scopeID),
          data.data!.ref,
        ]);
    } catch {
      setNotice("所属先のScopeを取得できません。");
    }
  }
  function lockSelected() {
    const selected = ui.store.getState().selected;
    if (
      !selected ||
      !["sentence", "paragraph", "section", "document"].includes(selected.kind)
    )
      return;
    const key = identityKey(selected),
      draft = ui.engine.state.drafts[key],
      body = draft?.value ?? ui.engine.state.items[key]?.record.value;
    const ref =
      draft?.base.record.ref ?? ui.engine.state.items[key]?.record.ref;
    if (!body || !ref || ref.revision === "0") return;
    const fields = Object.fromEntries(
      ["name", selected.kind === "sentence" ? "text" : "title"]
        .filter((k) => k in body)
        .map((k) => [k, JSON.stringify(body[k])]),
    );
    setFixed((current) => [
      ...current.filter((v) => identityKey(v.ref) !== key),
      { ref, fields },
    ]);
  }
  async function openOperation(ref: VersionRef) {
    try {
      const op = (await syncRequest(
        `${base}/records/${encodeURIComponent(ref.id)}?version=head`,
        ui.native,
        { method: "GET" },
      )) as {
        value: { policy: VersionRef; target: VersionRef; applied?: VersionRef };
      };
      const policy = (await syncRequest(
        `${base}/records/${encodeURIComponent(op.value.policy.id)}?version=${encodeURIComponent(op.value.policy.version)}`,
        ui.native,
        { method: "GET" },
      )) as { value: { generation: GenerationPolicy } };
      target.current = op.value.applied ?? op.value.target;
      setSource(policy.value.generation.source);
      setFixed(policy.value.generation.fixed);
      setGroupScopes(policy.value.generation.group_scopes);
      setInitial(ref);
      setPreview(undefined);
      navigation?.navigate(
        sourceRoute(ui.workspace, policy.value.generation.source),
        { related: true },
      );
    } catch {
      setNotice("操作を取得できません。現在の権限を確認してください。");
    }
  }
  return (
    <>
      <ScreenContent>
        {
          <section className="team" aria-label="出力からのContext生成">
            <h3>出力からContextを作成・更新</h3>
            <details>
              <summary>元の出力をIDで指定</summary>
              <label>
                元Run ID
                <input
                  value={runID}
                  onChange={(e) => setRunID(e.target.value)}
                />
              </label>
              <label>
                出力ID
                <input
                  value={outputID}
                  onChange={(e) => setOutputID(e.target.value)}
                />
              </label>
              <label>
                出力の固定版
                <input
                  value={outputVersion}
                  onChange={(e) => setOutputVersion(e.target.value)}
                />
              </label>
            </details>
            <details>
              <summary>出力の一部分だけを対象にする</summary>
              <label>
                対象箇所（JSON pointer・空欄はこの出力全体）
                <input
                  value={pointer}
                  onChange={(e) => setPointer(e.target.value)}
                />
              </label>
            </details>
            <button
              disabled={busy || !runID || !outputID || !outputVersion}
              onClick={() => void prepare()}
            >
              固定した出力と対象箇所を確認
            </button>
            {preview && (
              <section aria-label="再利用する出力">
                <h4>再利用する出力</h4>
                <pre className="source-preview" tabIndex={0}>
                  {preview.output.selected_utf8}
                </pre>
                <details>
                  <summary>出力の照合情報</summary>
                  <small>digest: {preview.output.digest}</small>
                </details>
              </section>
            )}
            <details>
              <summary>
                更新対象の固定・Scopeへの所属（任意）
                {fixed.length || groupScopes.length
                  ? ` · 固定${fixed.length}件・所属先${groupScopes.length}件`
                  : ""}
              </summary>
              <button disabled={!ui.canCreate} onClick={lockSelected}>
                選択中Contextの本文・名称を固定
              </button>
              {fixed.map((lock) => (
                <p key={lock.ref.id}>
                  {lock.ref.kind}:{lock.ref.id} · 固定:{" "}
                  {Object.keys(lock.fields).join("、")}
                  <button
                    onClick={() => setFixed(fixed.filter((v) => v !== lock))}
                  >
                    固定を外す
                  </button>
                </p>
              ))}
              <label>
                所属先Scope ID（文書構成と別）
                <input
                  value={scopeID}
                  onChange={(e) => setScopeID(e.target.value)}
                />
              </label>
              <button disabled={!scopeID} onClick={() => void group()}>
                所属先を取得
              </button>
              {groupScopes.map((ref) => (
                <p key={ref.id}>
                  {ref.id}
                  <button
                    onClick={() =>
                      setGroupScopes(groupScopes.filter((r) => r.id !== ref.id))
                    }
                  >
                    所属先から外す
                  </button>
                </p>
              ))}
            </details>
            <ContextGenerationAutomation
              ui={ui}
              source={source}
              fixed={fixed}
              groupScopes={groupScopes}
              profile={profile}
              channel={channel}
              onOpen={(ref) => void openOperation(ref)}
            />
            {result && (
              <ContextGenerationResults
                ui={ui}
                result={result}
                source={routeSource ?? source}
                onOpen={(ref) =>
                  navigation
                    ? navigation.navigate(
                        {
                          ...navigation.route,
                          view: "documents",
                          target: {
                            kind: ref.kind,
                            id: ref.id,
                            version: ref.revision,
                          },
                        },
                        { related: true },
                      )
                    : ui.store.getState().choose(ref)
                }
              />
            )}
            <p role="status">{notice}</p>
            <p role="status">{workerNotice}</p>
          </section>
        }
      </ScreenContent>
      {source && (
        <Screen
          screens={["context"]}
          enabled={matchesRoute && !!preview}
          background
        >
          <ClientSelection
            ui={{ ...ui, store }}
            generation={{
              target: target.current,
              policy: { source, fixed, group_scopes: groupScopes },
              initial,
              onProfile: setProfile,
              onApplied: async (ref) => {
                target.current = { id: ref.id, version: ref.version };
                setResult(ref);
                await ui.engine.refresh();
              },
            }}
          />
        </Screen>
      )}
    </>
  );
}
