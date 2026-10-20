import { ScreenContent, useNavigation, useScreenActive } from "./navigation";
import { useEffect, useRef, useState } from "react";
import { invoke } from "@tauri-apps/api/core";
import { syncRequest, workspaceAccessLost } from "./auth";
import { SyncHTTPError } from "./sync-error";
import {
  allowAnswerScope,
  answerScopeEpoch,
  readLocalDraft,
  saveLocalDraft,
  syncStorage,
} from "./sync-store";
import {
  applyFields,
  fieldSource,
  mergeFields,
  selectDefinitionContext,
} from "./workflow-json";
import type { DefinitionContextInput } from "./context-preview";
import { JSONField } from "./json-field";
import type { JSONPath } from "jsonc-parser";
import { WorkflowExecution } from "./workflow-execution";
import { DefinitionRun } from "./definition-run";
import { WorkflowNodeEditor } from "./workflow-node-editor";
import type { DefinitionLabel } from "./workflow-map";
import type { DefinitionPage } from "./definition-picker";
import { DefinitionGeneration } from "./definition-generation";
import { definitionFields } from "./definition-proposal";

type Ref = { id: string; version: string };
type DefinitionRecord = Ref & { kind: string; value_utf8: string };
type Page = {
  items: (Ref & { value: { name?: string; state?: string } })[];
  next: string;
};
type Validation = {
  definition: Ref;
  valid: boolean;
  problems: { path: string; code: string }[];
  checked: Ref[];
};
type Draft = {
  manual_paths?: string[];
  version: string;
  text: string;
  fields: Record<string, string | null>;
  pending?: {
    operation_id: string;
    expected_version: string;
    value_utf8: string;
  };
};
const problems: Record<string, string> = {
  input_unconnected: "必須入力が接続されていません",
  return_missing: "戻り先がありません",
  dependency_missing: "依存する工程がありません",
  dependency_cycle: "依存関係が循環しています",
  finite_budget_required: "差し戻しには有限の実行回数を指定してください",
  capability_missing: "接続の必要能力が不足しています",
  binding_missing: "入力の受渡し方式が未対応です",
  capability_unconfirmed: "接続の能力が未確認です",
  explicit_budget_required: "並列数などの実行予算を確認してください",
  none_context_binding: "無注入のContextへ出力を接続できません",
  run_or_decision_required: "Run定義か判断の選択肢のいずれかを指定してください",
  result_condition_required: "先行結果・差し戻しの条件を指定してください",
  fixed_definition_required: "Run定義の固定版を指定してください",
  input_dependency_required: "入力元を先行工程にも指定してください",
  output_selection_invalid:
    "出力の種類・チャンネル・位置・キーを確認してください",
  input_target_invalid: "入力先はcontext・prompt・commandから指定してください",
  join_policy_required: "先行工程の合流条件を指定してください",
  return_run_required: "差し戻す先にはRunノードを指定してください",
};
const compose = (draft: Draft) => applyFields(draft.text, draft.fields);

export function Workflows({
  native,
  api,
  workspace,
  user,
  role,
  canExecute,
  definitionInput,
  onDefinitionInputUsed,
  onContextSelection,
}: {
  native: boolean;
  api: string;
  workspace: string;
  user: string;
  role: string;
  canExecute: boolean;
  definitionInput?: DefinitionContextInput;
  onDefinitionInputUsed?: () => void;
  onContextSelection?: (value: DefinitionContextInput) => void;
}) {
  const namespace = { api, user, workspace };
  const navigation = useNavigation();
  const navigationRef = useRef(navigation);
  navigationRef.current = navigation;
  const visible = useScreenActive();
  const loadSerial = useRef(0);
  const base = `/workflows/workspaces/${encodeURIComponent(workspace)}`;
  const canEdit = role !== "viewer";
  const [kind, setKind] = useState("run-definition");
  const [id, setID] = useState("");
  const [requestedVersion, setRequestedVersion] = useState("head");
  const [record, setRecord] = useState<DefinitionRecord>();
  const [draft, setDraft] = useState<Draft>();
  const [page, setPage] = useState<Page>();
  const [validation, setValidation] = useState<Validation>();
  const [verified, setVerified] = useState<Ref>();
  const [adoption, setAdoption] = useState<Ref>();
  const [capDevice, setCapDevice] = useState("");
  const [capVersion, setCapVersion] = useState("head");
  const [capabilities, setCapabilities] = useState("");
  const [selected, setSelected] = useState(0);
  const [busy, setBusy] = useState(false),
    [notice, setNotice] = useState("");
  const actionIDs = useRef(new Map<string, string>());
  const call = async (path: string, body?: unknown) => {
    const epoch = answerScopeEpoch(namespace);
    const result = await syncRequest(base + path, native, {
      method: body === undefined ? "GET" : "POST",
      body,
    });
    if (!allowAnswerScope(namespace, epoch))
      throw new SyncHTTPError(403, undefined);
    return result;
  };
  const act = async (work: () => Promise<void>) => {
    setBusy(true);
    setNotice("");
    try {
      await work();
    } catch (error) {
      if (
        error instanceof SyncHTTPError &&
        (error.status === 401 || error.status === 403)
      ) {
        setRecord(undefined);
        setDraft(undefined);
        setPage(undefined);
        setValidation(undefined);
        setCapabilities("");
        if (await workspaceAccessLost(native, workspace, error))
          await syncStorage(native).remove(namespace);
        else if (id)
          await saveLocalDraft<Draft>(
            native,
            namespace,
            id,
            undefined,
            "workflow-definition",
          );
        setNotice("現在の閲覧・編集・実行依頼の権限を確認してください。");
      } else
        setNotice(
          "操作を完了できませんでした。入力・接続・現在の版を確認してください。",
        );
    } finally {
      setBusy(false);
    }
  };
  const requestMutation = async (
    path: string,
    version: string,
    value: object,
  ) => {
    const key = JSON.stringify({ path, version, value });
    if (!actionIDs.current.has(key))
      actionIDs.current.set(key, crypto.randomUUID());
    const result = (await call(path, {
      ...value,
      expected_version: version,
      operation_id: actionIDs.current.get(key)!,
    })) as { data: Ref };
    return { data: { id: result.data.id, version: result.data.version } };
  };
  const persist = (value: Draft) => {
    setDraft(value);
    setValidation(undefined);
    setVerified(undefined);
    if (id)
      void saveLocalDraft(
        native,
        namespace,
        id,
        value,
        "workflow-definition",
      ).then(
        () =>
          setNotice(
            "この端末へ下書きを保存しました。自動送信・実行はしません。",
          ),
        () => setNotice("下書きの保存を確認できません。"),
      );
  };
  const load = async (target: string, version = "head") => {
    const serial = ++loadSerial.current;
    const epoch = answerScopeEpoch(namespace);
    if (version === "0") {
      const saved = await readLocalDraft<Draft>(
        native,
        namespace,
        target,
        "workflow-definition",
      );
      if (serial !== loadSerial.current || !allowAnswerScope(namespace, epoch))
        return;
      if (!saved) {
        setNotice("この端末には指定された定義の下書きがありません。");
        return;
      }
      setID(target);
      setKind(
        target.startsWith("run-definition:")
          ? "run-definition"
          : "workflow-definition",
      );
      setDraft(saved);
      setRecord(undefined);
      setAdoption(undefined);
      setValidation(undefined);
      setVerified(undefined);
      setSelected(0);
      return;
    }
    const value = (await call(
      `/records/${encodeURIComponent(target)}?version=${encodeURIComponent(version)}&format=source`,
    )) as DefinitionRecord;
    if (!allowAnswerScope(namespace, epoch)) return;
    const saved = await readLocalDraft<Draft>(
      native,
      namespace,
      value.id,
      "workflow-definition",
    );
    const adopted = (await call(
      `/records/${encodeURIComponent("adoption:" + value.id)}?version=head`,
    ).catch((e: unknown) => {
      if (e instanceof SyncHTTPError && e.status === 403) return undefined;
      throw e;
    })) as (Ref & { value: { definition: Ref } }) | undefined;
    if (answerScopeEpoch(namespace) !== epoch || serial !== loadSerial.current)
      return;
    setID(value.id);
    setKind(value.kind);
    setRecord(value);
    setSelected(0);
    setValidation(undefined);
    setVerified(undefined);
    setDraft(
      saved ?? { version: value.version, text: value.value_utf8, fields: {} },
    );
    setAdoption(adopted?.value.definition);
    if (saved)
      setNotice(
        saved.version === value.version
          ? "端末内の未送信下書きを復元しました。"
          : "下書きの前提版が古くなっています。現在の内容と比較してください。",
      );
    const currentNavigation = navigationRef.current;
    if (
      currentNavigation?.route.screen === "workflows" &&
      (currentNavigation.route.target?.id !== value.id ||
        currentNavigation.route.target.version !== value.version)
    )
      currentNavigation.navigate(
        {
          ...currentNavigation.route,
          target: { id: value.id, version: value.version, kind: value.kind },
        },
        { replace: currentNavigation.route.target?.id === value.id },
      );
  };
  useEffect(() => {
    const target = navigation?.route.target;
    if (
      !visible ||
      navigation?.route.screen !== "workflows" ||
      !target ||
      !["run-definition", "workflow-definition"].includes(target.kind)
    )
      return;
    if (
      id === target.id &&
      (record?.version === target.version || draft?.version === target.version)
    )
      return;
    void act(() => load(target.id, target.version));
  }, [
    visible,
    navigation?.route.target?.id,
    navigation?.route.target?.version,
  ]);
  const fresh = (type: string, text?: string) => {
    const target = `${type}:${crypto.randomUUID()}`;
    setID(target);
    setRequestedVersion("head");
    setNotice(
      "新しい定義の下書きをこの端末で編集中です。共有への保存・採用・実行はしていません。",
    );
    setKind(type);
    setRecord(undefined);
    setAdoption(undefined);
    setSelected(0);
    setValidation(undefined);
    setVerified(undefined);
    const value = {
      version: "0",
      text:
        text ??
        JSON.stringify(
          type === "run-definition"
            ? {
                schema: "run-definition-v1",
                name: "",
                spec: {
                  input: {
                    command: { kind: "cli", target: "", parameters: [] },
                  },
                },
              }
            : {
                schema: "workflow-definition-v1",
                name: "",
                nodes: [],
                budget: {},
              },
          null,
          2,
        ),
      fields: {},
    };
    setDraft(value);
    navigation?.navigate({
      workspace,
      screen: "workflows",
      view: "edit",
      target: { kind: type, id: target, version: "0" },
    });
    void saveLocalDraft(
      native,
      namespace,
      target,
      value,
      "workflow-definition",
    ).catch(() => setNotice("下書きの保存を確認できません。"));
  };
  useEffect(() => {
    if (
      !visible ||
      !canEdit ||
      !definitionInput ||
      !draft ||
      id !== definitionInput.definition.id
    )
      return;
    if (draft.version !== definitionInput.definition.version) {
      setNotice(
        "定義の前提版が変わりました。現在の定義からContextを選び直してください。",
      );
      return;
    }
    try {
      persist({
        ...draft,
        text: selectDefinitionContext(
          compose(draft),
          definitionInput.selection,
        ),
        fields: {},
        pending: undefined,
      });
      onDefinitionInputUsed?.();
    } catch {
      setNotice("定義の未保存JSONを確認してからContextを反映してください。");
    }
  }, [visible, canEdit, definitionInput, id, draft?.version]);
  const field = (
    label: string,
    path: JSONPath,
    format: "text" | "json" | "number" = "text",
  ) =>
    draft && (
      <JSONField
        label={label}
        path={path}
        format={format}
        text={draft.text}
        fields={draft.fields}
        onChange={(key, raw) =>
          persist({
            ...draft,
            pending: undefined,
            fields: { ...draft.fields, [key]: raw },
          })
        }
      />
    );
  const save = async () => {
    if (!draft) return;
    const text = compose(draft);
    const pending =
      draft.pending?.value_utf8 === text
        ? draft.pending
        : {
            operation_id: crypto.randomUUID(),
            expected_version: draft.version,
            value_utf8: text,
          };
    await saveLocalDraft(
      native,
      namespace,
      id,
      { ...draft, pending },
      "workflow-definition",
    );
    setDraft({ ...draft, pending });
    const value = (await call(
      `/records/${encodeURIComponent(id)}`,
      pending,
    )) as { data: Ref };
    await saveLocalDraft<Draft>(
      native,
      namespace,
      id,
      undefined,
      "workflow-definition",
    );
    await load(value.data.id, value.data.version);
    setNotice("定義の下書きを共有へ保存しました。採用・実行はしていません。");
  };
  return (
    <>
      <ScreenContent>
        {
          <section
            id="cost-definition-editor"
            tabIndex={-1}
            className="team-details workflow-editor"
          >
            <h3>工程定義・検証・採用と実行</h3>
            <p>
              定義の保存・検証・採用と、実行開始を分けます。必要な固定入力と接続だけを指定します。
            </p>
            <div className="team-controls">
              <label>
                定義の種類
                <select
                  disabled={busy}
                  value={kind}
                  onChange={(e) => {
                    setKind(e.target.value);
                    setPage(undefined);
                    setID("");
                    setRecord(undefined);
                    setDraft(undefined);
                    setValidation(undefined);
                    setVerified(undefined);
                    setAdoption(undefined);
                  }}
                >
                  <option value="run-definition">Run定義</option>
                  <option value="workflow-definition">工程定義</option>
                </select>
              </label>
              <button
                disabled={busy}
                onClick={() =>
                  void act(async () =>
                    setPage(
                      (
                        (await call("/query", {
                          kind: "heads",
                          type: kind,
                          limit: 50,
                        })) as { data: Page }
                      ).data,
                    ),
                  )
                }
              >
                定義一覧を取得
              </button>
              <label>
                定義ID
                <input
                  disabled={busy}
                  value={id}
                  onChange={(e) => {
                    setID(e.target.value);
                    setRecord(undefined);
                    setDraft(undefined);
                  }}
                />
              </label>
              <label>
                定義の版（headは現在版）
                <input
                  disabled={busy}
                  value={requestedVersion}
                  onChange={(e) => setRequestedVersion(e.target.value)}
                />
              </label>
              <button
                disabled={busy || !id}
                onClick={() => void act(() => load(id, requestedVersion))}
              >
                定義を取得・更新
              </button>
              {canEdit && (
                <button disabled={busy} onClick={() => fresh(kind)}>
                  新しい定義の下書き
                </button>
              )}
            </div>
            {page && (
              <>
                <ul>
                  {page.items.map((r) => (
                    <li key={r.id}>
                      <button
                        disabled={busy}
                        onClick={() => void act(() => load(r.id, r.version))}
                      >
                        {r.value.name || r.id} · {r.version}
                      </button>
                    </li>
                  ))}
                </ul>
                {page.next && (
                  <button
                    disabled={busy}
                    onClick={() =>
                      void act(async () =>
                        setPage(
                          (
                            (await call("/query", {
                              kind: "heads",
                              type: kind,
                              after: page.next,
                              limit: 50,
                            })) as { data: Page }
                          ).data,
                        ),
                      )
                    }
                  >
                    定義一覧の次のページ
                  </button>
                )}
              </>
            )}
            {canEdit && (
              <label>
                定義ファイルを新しい下書きとして読み込む
                <input
                  type="file"
                  accept="application/json,.json"
                  disabled={busy}
                  onChange={(e) => {
                    const file = e.target.files?.[0];
                    if (file)
                      void act(async () => {
                        const text = await file.text();
                        const schema = JSON.parse(
                          fieldSource(text, ["schema"]),
                        ) as string;
                        if (
                          schema !== "run-definition-v1" &&
                          schema !== "workflow-definition-v1"
                        )
                          throw Error("schema");
                        fresh(schema.slice(0, -3), text);
                        setNotice(
                          "ファイルを端末内の新しい下書きへ読み込みました。共有への保存・実行はしていません。",
                        );
                      });
                  }}
                />
              </label>
            )}
            {record && (
              <p>
                {record.id} · {record.version} / 共有下書き。採用版:{" "}
                {adoption?.version ?? "未採用"}
              </p>
            )}
            {draft && (
              <section>
                <fieldset disabled={busy || !canEdit}>
                  <p>
                    項目の入力はこの端末の下書きへ保存し、JSON反映または共有保存時に同じ定義へ反映します。
                  </p>
                  {field("定義の名前", ["name"])}
                  {kind === "run-definition" ? (
                    <>
                      {field("commandの種類", [
                        "spec",
                        "input",
                        "command",
                        "kind",
                      ])}
                      {field("commandの接続先", [
                        "spec",
                        "input",
                        "command",
                        "target",
                      ])}
                      {field(
                        "commandの引数・要求（JSON）",
                        ["spec", "input", "command", "parameters"],
                        "json",
                      )}
                      {field(
                        "入力の受渡し（JSON）",
                        ["spec", "input", "command", "binding"],
                        "json",
                      )}
                      {field(
                        "固定Contextの選択（JSON・省略可）",
                        ["selection"],
                        "json",
                      )}
                      {onContextSelection && (
                        <button
                          disabled={busy || !canEdit}
                          onClick={() =>
                            void act(async () => {
                              const value =
                                JSON.parse(
                                  fieldSource(compose(draft), ["selection"]) ||
                                    "{}",
                                ) ?? {};
                              const mode = value.mode ?? "select";
                              if (
                                !["none", "select"].includes(mode) ||
                                (value.include &&
                                  !Array.isArray(value.include)) ||
                                (value.exclude && !Array.isArray(value.exclude))
                              )
                                throw new Error("invalid_selection");
                              onContextSelection({
                                definition: { id, version: draft.version },
                                selection: {
                                  mode,
                                  include: value.include ?? [],
                                  exclude: value.exclude ?? [],
                                },
                              });
                            })
                          }
                        >
                          Contextを選択・定義の入力をプレビュー
                        </button>
                      )}
                      {field(
                        "prompt（JSON・省略可）",
                        ["spec", "input", "prompt"],
                        "json",
                      )}
                      {field(
                        "検品などの派生契約（JSON・省略可）",
                        ["spec", "derivation"],
                        "json",
                      )}
                      {field(
                        "前段から受ける必須入力（JSON・省略可）",
                        ["inputs"],
                        "json",
                      )}
                      {field(
                        "必要な接続能力（JSON・省略可）",
                        ["required"],
                        "json",
                      )}
                      {field("担当端末ID（省略可）", ["device_id"])}
                    </>
                  ) : (
                    <>
                      <p>
                        工程ごとにRun定義の固定版、結果条件、入力接続と戻り先を指定します。実行予算の数値は自動で設定しません。
                      </p>
                      <details>
                        <summary>実行予算とContext基準を確認・編集</summary>
                        {field(
                          "同時に実行する上限",
                          ["budget", "parallel"],
                          "number",
                        )}
                        {field(
                          "生成するRun数の上限（循環時は必須）",
                          ["budget", "max_runs"],
                          "number",
                        )}
                        {field(
                          "工程の制御時間（秒・省略可）",
                          ["budget", "seconds"],
                          "number",
                        )}
                        {field(
                          "費用予算（JSON・省略可）",
                          ["budget", "cost"],
                          "json",
                        )}
                        {field(
                          "Context基準の固定参照（JSON・省略可）",
                          ["baseline"],
                          "json",
                        )}
                      </details>
                    </>
                  )}
                </fieldset>
                {kind === "workflow-definition" && (
                  <WorkflowNodeEditor
                    disabled={busy || !canEdit}
                    text={(() => {
                      try {
                        return compose(draft);
                      } catch {
                        return undefined;
                      }
                    })()}
                    selected={selected}
                    onSelect={setSelected}
                    loadLabels={async (refs) =>
                      (await call("/labels", refs)) as DefinitionLabel[]
                    }
                    loadDefinitions={async (after) =>
                      (
                        (await call("/query", {
                          kind: "heads",
                          type: "run-definition",
                          after,
                          limit: 50,
                        })) as { data: DefinitionPage }
                      ).data
                    }
                    onChange={(text) =>
                      persist({
                        ...draft,
                        text,
                        fields: {},
                        pending: undefined,
                      })
                    }
                  />
                )}
                <fieldset disabled={busy || !canEdit}>
                  <button
                    onClick={() => {
                      try {
                        persist({
                          ...draft,
                          text: compose(draft),
                          fields: {},
                          pending: undefined,
                        });
                      } catch {
                        setNotice("項目のJSON構文を確認してください。");
                      }
                    }}
                  >
                    項目をJSONへ反映
                  </button>
                  <details>
                    <summary>定義のJSON原文を編集</summary>
                    <p>
                      原文を直接編集すると、上の未反映の項目入力を置き換えます。
                    </p>
                    <textarea
                      aria-label="定義のJSON原文"
                      rows={15}
                      value={draft.text}
                      onChange={(e) =>
                        persist({
                          ...draft,
                          text: e.target.value,
                          fields: {},
                          manual_paths: definitionFields.map(([path]) => path),
                          pending: undefined,
                        })
                      }
                    />
                  </details>
                  {record && draft.version !== record.version && (
                    <>
                      <p>
                        下書きの前提版: {draft.version}
                        。共有の現在版と比較してから適用してください。
                      </p>
                      <textarea
                        aria-label="共有側の定義"
                        readOnly
                        rows={8}
                        value={record.value_utf8}
                      />
                      <button
                        onClick={() =>
                          persist({
                            ...draft,
                            version: record.version,
                            pending: undefined,
                          })
                        }
                      >
                        比較した下書きを現在版へ手動適用
                      </button>
                    </>
                  )}
                  <button disabled={!id} onClick={() => void act(save)}>
                    定義の下書きを共有へ保存（実行しない）
                  </button>
                </fieldset>
              </section>
            )}
            {record && (
              <div className="team-controls">
                <button
                  disabled={busy}
                  onClick={() =>
                    void act(async () =>
                      setValidation(
                        (await call("/validate", {
                          id: record.id,
                          version: record.version,
                        })) as Validation,
                      ),
                    )
                  }
                >
                  この固定版を静的検証（実行しない）
                </button>
                {canEdit && (
                  <button
                    disabled={busy}
                    onClick={() =>
                      void act(async () => {
                        const key = "validation:" + record.id;
                        const old = (await call(
                          `/records/${encodeURIComponent(key)}?version=head`,
                        ).catch((e: unknown) => {
                          if (e instanceof SyncHTTPError && e.status === 403)
                            return undefined;
                          throw e;
                        })) as Ref | undefined;
                        const value = await requestMutation(
                          "/verify",
                          old?.version ?? "0",
                          {
                            definition: {
                              id: record.id,
                              version: record.version,
                            },
                          },
                        );
                        setVerified(value.data);
                        setValidation(
                          (await call("/validate", {
                            id: record.id,
                            version: record.version,
                          })) as Validation,
                        );
                        setNotice(
                          "検証結果を保存しました。採用・実行はしていません。",
                        );
                      })
                    }
                  >
                    検証結果を版付きで保存
                  </button>
                )}
                {canEdit && verified && validation?.valid && (
                  <button
                    disabled={busy}
                    onClick={() =>
                      void act(async () => {
                        const old = (await call(
                          `/records/${encodeURIComponent("adoption:" + record.id)}?version=head`,
                        ).catch((e: unknown) => {
                          if (e instanceof SyncHTTPError && e.status === 403)
                            return undefined;
                          throw e;
                        })) as Ref | undefined;
                        await requestMutation("/adopt", old?.version ?? "0", {
                          definition: {
                            id: record.id,
                            version: record.version,
                          },
                          validation: verified,
                        });
                        setAdoption({ id: record.id, version: record.version });
                        setNotice(
                          "この固定版を採用しました。実行開始は依頼していません。",
                        );
                      })
                    }
                  >
                    検証した固定版を採用（実行しない）
                  </button>
                )}
                {native ? (
                  <button
                    disabled={busy}
                    onClick={() =>
                      void act(async () => {
                        const result = await invoke<{ path: string }>(
                          "export_definition",
                          { workspace, id: record.id, version: record.version },
                        );
                        setNotice(`定義を保存しました: ${result.path}`);
                      })
                    }
                  >
                    この定義版をファイルへ保存
                  </button>
                ) : (
                  <a
                    download
                    href={`${base}/definitions/${encodeURIComponent(record.id)}/export?version=${encodeURIComponent(record.version)}`}
                  >
                    この定義版をファイルへ保存
                  </a>
                )}
                {canExecute && (
                  <p>実行開始は採用した工程版から別の操作で依頼します。</p>
                )}
              </div>
            )}
            {validation && (
              <section aria-label="定義の検証結果">
                <p>
                  {validation.valid
                    ? "静的検証済み（実行していません）"
                    : "定義に未解決の箇所があります"}
                </p>
                <ul>
                  {validation.problems.map((p, i) => (
                    <li key={i}>
                      <button
                        data-variant="secondary"
                        onClick={() => {
                          const index = /^\/nodes\/(\d+)(?:\/|$)/.exec(p.path);
                          if (index) {
                            setSelected(Number(index[1]));
                            requestAnimationFrame(() =>
                              document
                                .getElementById("workflow-node-settings")
                                ?.focus(),
                            );
                          } else
                            document
                              .getElementById("cost-definition-editor")
                              ?.focus();
                        }}
                      >
                        {p.path}: {problems[p.code] ?? p.code} — 該当箇所を開く
                      </button>
                    </li>
                  ))}
                </ul>
              </section>
            )}
            {notice && <p role="status">{notice}</p>}
            <details>
              <summary>接続能力の版を確認</summary>
              <label>
                端末ID
                <input
                  value={capDevice}
                  onChange={(e) => setCapDevice(e.target.value)}
                />
              </label>
              <label>
                能力の固定版（headは現在版）
                <input
                  value={capVersion}
                  onChange={(e) => setCapVersion(e.target.value)}
                />
              </label>
              <button
                disabled={busy || !capDevice}
                onClick={() =>
                  void act(async () => {
                    const result = await call(
                      `/capabilities/${encodeURIComponent(capDevice)}?version=${encodeURIComponent(capVersion)}`,
                    );
                    setCapabilities(JSON.stringify(result, null, 2));
                  })
                }
              >
                能力を取得（実行しない）
              </button>
              {capabilities && <pre>{capabilities}</pre>}
            </details>
          </section>
        }
      </ScreenContent>
      {kind === "run-definition" && draft && id && (
        <DefinitionGeneration
          key={`${api}:${user}:${workspace}:${id}`}
          native={native}
          api={api}
          user={user}
          workspace={workspace}
          canEdit={canEdit && !draft.pending}
          target={{ id, version: draft.version }}
          text={compose(draft)}
          manualPaths={Array.from(
            new Set([
              ...(draft.manual_paths ?? []),
              ...Object.keys(draft.fields).map(
                (path) => "/" + (JSON.parse(path) as JSONPath).join("/"),
              ),
            ]),
          ).filter((path) =>
            definitionFields.some(([allowed]) => allowed === path),
          )}
          onDraft={(text) =>
            persist({
              ...draft,
              text: mergeFields(compose(draft), text),
              fields: {},
              manual_paths: definitionFields.map(([path]) => path),
            })
          }
          onApplied={async (ref, fixedPaths, isCurrent) => {
            const applied = (await call(
              `/records/${encodeURIComponent(ref.id)}?version=${encodeURIComponent(ref.version)}`,
            )) as { value: { definition: Ref } };
            const saved = (await call(
              `/records/${encodeURIComponent(applied.value.definition.id)}?version=${encodeURIComponent(applied.value.definition.version)}&format=source`,
            )) as DefinitionRecord;
            const next = {
              manual_paths: fixedPaths,
              version: saved.version,
              text: saved.value_utf8,
              fields: {},
            };
            if (!isCurrent()) return false;
            setRecord(saved);
            setDraft(next);
            setAdoption(applied.value.definition);
            setVerified(undefined);
            setValidation(undefined);
            await saveLocalDraft(
              native,
              namespace,
              id,
              next,
              "workflow-definition",
            );
            return true;
          }}
        />
      )}
      {record?.kind === "run-definition" &&
        adoption?.version === record.version && (
          <DefinitionRun
            key={`${api}:${user}:${workspace}:${record.id}:${record.version}`}
            native={native}
            api={api}
            user={user}
            workspace={workspace}
            definition={record}
            canEdit={canEdit}
          />
        )}
      <WorkflowExecution
        native={native}
        api={api}
        user={user}
        workspace={workspace}
        definition={record}
        canEdit={canEdit}
        canExecute={canExecute}
        onSelect={(ref) =>
          navigation?.navigate({
            workspace,
            screen: "work",
            target: { ...ref, kind: "execution" },
            view: "overview",
          })
        }
      />
    </>
  );
}
