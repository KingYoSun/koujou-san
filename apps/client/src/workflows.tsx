import { useRef, useState } from "react";
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
import { editField, fieldSource, nodeCount } from "./workflow-json";
import type { JSONPath } from "jsonc-parser";
import { WorkflowExecution } from "./workflow-execution";

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
};
const compose = (draft: Draft) =>
  Object.entries(draft.fields).reduce(
    (text, [path, value]) =>
      editField(text, JSON.parse(path) as JSONPath, value ?? undefined),
    draft.text,
  );

export function Workflows({
  native,
  api,
  workspace,
  user,
  role,
  canExecute,
}: {
  native: boolean;
  api: string;
  workspace: string;
  user: string;
  role: string;
  canExecute: boolean;
}) {
  const namespace = { api, user, workspace };
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
    const epoch = answerScopeEpoch(namespace);
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
    if (answerScopeEpoch(namespace) !== epoch) return;
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
  };
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
    void saveLocalDraft(
      native,
      namespace,
      target,
      value,
      "workflow-definition",
    ).catch(() => setNotice("下書きの保存を確認できません。"));
  };
  const field = (
    label: string,
    path: JSONPath,
    format: "text" | "json" | "number" = "text",
  ) => {
    if (!draft) return null;
    const key = JSON.stringify(path);
    let source = key in draft.fields ? (draft.fields[key] ?? "") : undefined;
    try {
      source ??= fieldSource(draft.text, path);
    } catch {
      source ??= "";
    }
    let value = source ?? "";
    if (format === "text") {
      try {
        value = JSON.parse(value) as string;
      } catch {
        value = "";
      }
    }
    const change = (next: string) =>
      persist({
        ...draft,
        pending: undefined,
        fields: {
          ...draft.fields,
          [key]: format === "text" ? JSON.stringify(next) : next || null,
        },
      });
    return (
      <label>
        {label}
        {format === "json" ? (
          <textarea
            rows={3}
            value={value}
            onChange={(e) => change(e.target.value)}
          />
        ) : (
          <input
            type={format === "number" ? "number" : "text"}
            value={value}
            onChange={(e) => change(e.target.value)}
          />
        )}
      </label>
    );
  };
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
  let count = 0;
  try {
    if (draft) count = nodeCount(compose(draft));
  } catch {
    /* The raw editor retains invalid input until corrected. */
  }
  return (
    <details className="team-details workflow-editor">
      <summary>工程定義・検証・採用と実行</summary>
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
        <fieldset disabled={busy || !canEdit}>
          <p>
            項目の入力はこの端末の下書きへ保存し、JSON反映または共有保存時に同じ定義へ反映します。
          </p>
          {field("定義の名前", ["name"])}
          {kind === "run-definition" ? (
            <>
              {field("commandの種類", ["spec", "input", "command", "kind"])}
              {field("commandの接続先", ["spec", "input", "command", "target"])}
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
              {field("必要な接続能力（JSON・省略可）", ["required"], "json")}
              {field("担当端末ID（省略可）", ["device_id"])}
            </>
          ) : (
            <>
              <p>
                工程ごとにRun定義の固定版、結果条件、入力接続と戻り先を指定します。実行予算の数値は自動で設定しません。
              </p>
              {field("同時に実行する上限", ["budget", "parallel"], "number")}
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
              {field("費用予算（JSON・省略可）", ["budget", "cost"], "json")}
              {field(
                "Context基準の固定参照（JSON・省略可）",
                ["baseline"],
                "json",
              )}
              <label>
                編集する工程
                <select
                  value={selected}
                  onChange={(e) => setSelected(Number(e.target.value))}
                >
                  {Array.from({ length: count }, (_, i) => (
                    <option key={i} value={i}>
                      {i + 1}
                    </option>
                  ))}
                </select>
              </label>
              <button
                onClick={() => {
                  try {
                    const text = editField(
                      compose(draft),
                      ["nodes", count],
                      JSON.stringify({ id: `node-${count + 1}`, join: "all" }),
                    );
                    persist({ ...draft, text, fields: {}, pending: undefined });
                    setSelected(count);
                  } catch {
                    setNotice("JSONの構文と項目の入力を確認してください。");
                  }
                }}
              >
                工程を追加
              </button>
              {count > 0 && (
                <>
                  {field("工程の識別名", ["nodes", selected, "id"])}
                  {field("Run定義ID", ["nodes", selected, "definition", "id"])}
                  {field("Run定義の固定版", [
                    "nodes",
                    selected,
                    "definition",
                    "version",
                  ])}
                  {field(
                    "判断待ちの選択肢（JSON・Run定義と択一）",
                    ["nodes", selected, "choices"],
                    "json",
                  )}
                  {field(
                    "依存する工程と結果（JSON）",
                    ["nodes", selected, "depends"],
                    "json",
                  )}
                  {field("合流の条件（all / any）", [
                    "nodes",
                    selected,
                    "join",
                  ])}
                  {field(
                    "前段出力の選択と入力接続（JSON）",
                    ["nodes", selected, "bindings"],
                    "json",
                  )}
                  {field(
                    "開始時に明示する入力（JSON・省略可）",
                    ["nodes", selected, "inputs"],
                    "json",
                  )}
                  {field(
                    "戻り先（結果名から工程ID、JSON・省略可）",
                    ["nodes", selected, "retry"],
                    "json",
                  )}
                </>
              )}
            </>
          )}
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
            <p>原文を直接編集すると、上の未反映の項目入力を置き換えます。</p>
            <textarea
              aria-label="定義のJSON原文"
              rows={15}
              value={draft.text}
              onChange={(e) =>
                persist({
                  ...draft,
                  text: e.target.value,
                  fields: {},
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
                    { definition: { id: record.id, version: record.version } },
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
                    definition: { id: record.id, version: record.version },
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
                {p.path}: {problems[p.code] ?? p.code}
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
      <WorkflowExecution
        native={native}
        api={api}
        user={user}
        workspace={workspace}
        definition={record}
        canEdit={canEdit}
        canExecute={canExecute}
      />
    </details>
  );
}
