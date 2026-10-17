import { useRef, useState } from "react";
import { syncRequest, workspaceAccessLost } from "./auth";
import { SyncHTTPError } from "./sync-error";
import {
  allowAnswerScope,
  answerScopeEpoch,
  readLocalDraft,
  saveLocalDraft,
  syncStorage,
} from "./sync-store";

type Ref = { id: string; version: string };
type Entry = Ref & {
  owner_id: string;
  kind: string;
  value: Record<string, unknown>;
};
type Page = { items: Entry[]; next: string; through?: string };
type Draft = { text: string; version: string };
type CriterionResult = {
  criterion_id: string;
  verdict: string;
  executed: boolean;
  evidence_refs: Ref[];
  unverified: string[];
};
type View = {
  launch: Entry & {
    value: {
      definition: Ref;
      run_id: string;
      manifest: { digest: string; state: string };
      separation: Record<
        string,
        { level: string; origin: string; method: string }
      >;
    };
  };
  run: Ref & {
    value: {
      state: string;
      input: Ref;
      exit_code?: number;
      assessment?: {
        state: string;
        verdict?: string;
        report?: {
          criteria_results?: CriterionResult[];
          unverified?: string[];
        };
      };
    };
  };
  validity: string;
  evidence_current: Record<
    string,
    {
      kind: string;
      state: string;
      description: string;
      artifact: { uri: string; revision: string; digest: string };
    }
  >;
};
const format = (v: unknown) => JSON.stringify(v, null, 2);
const sourceKinds: Record<string, string> = {
  "machine-observation": "実行・機器の観測",
  "human-confirmation": "人の確認",
  "ai-comment": "AIコメント",
  "implementer-claim": "実装者の申告",
};
const levels: Record<string, string> = {
  unobserved: "未確認",
  declared: "宣言",
  observed: "ログによる確認",
  enforced: "環境で強制",
};
const dimensions: Record<string, string> = {
  session: "会話",
  workspace: "作業場所",
  permissions: "読み書き権限",
  external_access: "外部接続",
  criteria: "固定基準",
  context: "Context注入",
};

export function Inspections({
  native,
  api,
  user,
  workspace,
  role,
  canExecute,
}: {
  native: boolean;
  api: string;
  user: string;
  workspace: string;
  role: string;
  canExecute: boolean;
}) {
  const namespace = { api, user, workspace },
    base = `/inspections/workspaces/${encodeURIComponent(workspace)}`;
  const canEdit = role !== "viewer";
  const [id, setID] = useState(""),
    [version, setVersion] = useState("head"),
    [type, setType] = useState("definition");
  const [record, setRecord] = useState<Entry>(),
    [draft, setDraft] = useState<Draft>(),
    [page, setPage] = useState<Page>(),
    [history, setHistory] = useState<Page>();
  const [adoption, setAdoption] = useState<Ref>(),
    [device, setDevice] = useState(""),
    [repository, setRepository] = useState("null"),
    [run, setRun] = useState("");
  const [view, setView] = useState<View>(),
    [compare, setCompare] = useState(""),
    [other, setOther] = useState<View>(),
    [decision, setDecision] = useState("");
  const [busy, setBusy] = useState(false),
    [notice, setNotice] = useState("");
  const operations = useRef(new Map<string, string>());
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
  const clear = () => {
    setRecord(undefined);
    setDraft(undefined);
    setPage(undefined);
    setHistory(undefined);
    setAdoption(undefined);
    setView(undefined);
    setOther(undefined);
    setDecision("");
  };
  const act = async (work: () => Promise<void>) => {
    setBusy(true);
    setNotice("");
    try {
      await work();
    } catch (e) {
      if (
        e instanceof SyncHTTPError &&
        (e.status === 401 || e.status === 403)
      ) {
        clear();
        if (await workspaceAccessLost(native, workspace, e))
          await syncStorage(native).remove(namespace);
        else if (id)
          await saveLocalDraft<Draft>(
            native,
            namespace,
            id,
            undefined,
            "inspection",
          );
        setNotice("現在の閲覧・編集・実行権限を確認してください。");
      } else
        setNotice(
          "操作を完了できませんでした。入力、分離能力、現在の版・接続を確認してください。",
        );
    } finally {
      setBusy(false);
    }
  };
  const mutate = async (path: string, expected: string, body: object) => {
    const key = format({ path, expected, body });
    if (!operations.current.has(key))
      operations.current.set(key, crypto.randomUUID());
    return (await call(path, {
      ...body,
      operation_id: operations.current.get(key),
      expected_version: expected,
    })) as { data: Ref };
  };
  const persist = (value: Draft, target = id) => {
    setDraft(value);
    void saveLocalDraft(native, namespace, target, value, "inspection").catch(
      () => setNotice("下書きを保存できませんでした。"),
    );
  };
  const load = async (target = id, requested = version) => {
    const epoch = answerScopeEpoch(namespace);
    const entry = (await call(
      `/records/${encodeURIComponent(target)}?version=${encodeURIComponent(requested)}`,
    )) as Entry;
    const saved = await readLocalDraft<Draft>(
      native,
      namespace,
      target,
      "inspection",
    );
    if (!allowAnswerScope(namespace, epoch)) return;
    setID(target);
    setRecord(entry);
    setDraft(saved ?? { version: entry.version, text: format(entry.value) });
    setAdoption(undefined);
    setHistory(undefined);
    setView(undefined);
    setOther(undefined);
    setDecision("");
    if (saved)
      setNotice(
        saved.version === entry.version
          ? "未送信の下書きを復元しました。"
          : "下書きの前提版が古くなっています。共有の現在版と比較してください。",
      );
    if (entry.kind === "definition") {
      const adopted = (await call(
        `/records/${encodeURIComponent("inspection-adoption:" + target)}`,
      ).catch((e: unknown) => {
        if (e instanceof SyncHTTPError && e.status === 403) return undefined;
        throw e;
      })) as Entry | undefined;
      if (allowAnswerScope(namespace, epoch))
        setAdoption(adopted && { id: adopted.id, version: adopted.version });
    }
  };
  const list = async (after = "") => {
    const result = (await call("/query", {
      kind: "heads",
      type,
      after,
      limit: 50,
    })) as { data: Page };
    setPage(result.data);
  };
  const showHistory = async (after = "") => {
    const result = (await call("/query", {
      kind: "history",
      id: record?.owner_id ?? id,
      after,
      through: after ? history?.through : undefined,
      limit: 25,
    })) as { data: Page };
    setHistory(result.data);
  };
  const showRun = async (target = run) => {
    const result = (await call(
      `/current/${encodeURIComponent(target)}`,
    )) as View;
    setRun(result.launch.value.run_id);
    setView(result);
    setOther(undefined);
    setDecision(
      format({
        launch: { id: result.launch.id, version: result.launch.version },
        source: { id: result.run.id, version: result.run.version },
        criteria:
          result.run.value.assessment?.report?.criteria_results?.map(
            (c) => c.criterion_id,
          ) ?? [],
        decision: "accept-with-exceptions",
        reason: "",
        unverified: [],
      }),
    );
  };
  const fresh = (evidence: boolean) => {
    clear();
    const target = `${evidence ? "inspection-evidence" : "inspection"}:${crypto.randomUUID()}`;
    setID(target);
    setVersion("head");
    persist(
      {
        version: "0",
        text: format(
          evidence
            ? {
                artifact: {
                  uri: "",
                  revision: "",
                  range: "",
                  digest: "",
                  delivery: "reference",
                  state: "ready",
                },
                targets: [],
                criteria: [],
                kind: "machine-observation",
                description: "",
                state: "unobserved",
                checked_at: new Date().toISOString(),
              }
            : {
                name: "",
                kind: "InspectionRun",
                purpose: "fresh",
                template: { id: "", version: "" },
                targets: [],
                criteria: [],
                selection: { mode: "none" },
                required_separation: {},
              },
        ),
      },
      target,
    );
    setNotice("端末内の下書きです。共有への保存・採用・実行は別の操作です。");
  };
  const renderView = (v: View, title: string) => (
    <section aria-label={title}>
      <h4>
        {title}: {v.launch.value.run_id}
      </h4>
      <p>
        過去の判定:{" "}
        <strong>{v.run.value.assessment?.verdict ?? "未判定"}</strong> / 形式:{" "}
        {v.run.value.assessment?.state ?? "未確認"} / 現在の適用:{" "}
        <strong>
          {v.validity === "STALE"
            ? "Stale（基準変更・再検品が必要）"
            : "採用中の基準に一致"}
        </strong>
      </p>
      <p>
        実行: {v.run.value.state} / 終了コード:{" "}
        {v.run.value.exit_code ?? "未観測"} / Context:{" "}
        {v.launch.value.manifest.state === "none"
          ? "明示的に無注入"
          : v.launch.value.manifest.state}
      </p>
      <p className="note">
        固定基準・Context注入の強制は、版と注入内容の固定です。CLIから読める全ファイル・記憶の分離を保証するものではありません。
      </p>
      <dl className="status-list">
        {Object.entries(v.launch.value.separation).map(([name, s]) => (
          <div key={name}>
            <dt>{dimensions[name] ?? name}</dt>
            <dd>
              {levels[s.level] ?? s.level} · {s.method || "方法未確認"}
            </dd>
          </div>
        ))}
      </dl>
      <div className="table-scroll">
        <table className="inspection-results">
          <thead>
            <tr>
              <th>条件</th>
              <th>判定</th>
              <th>実行</th>
              <th>証拠</th>
              <th>未確認</th>
            </tr>
          </thead>
          <tbody>
            {v.run.value.assessment?.report?.criteria_results?.map((c) => (
              <tr key={c.criterion_id}>
                <td>{c.criterion_id}</td>
                <td>{c.verdict}</td>
                <td>{c.executed ? "実行済み" : "未実行"}</td>
                <td>
                  {c.evidence_refs
                    ?.map((e) => `${e.id}@${e.version}`)
                    .join(", ") || "なし"}
                </td>
                <td>{c.unverified?.join(" / ") || "記載なし"}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <p>
        全体の未確認:{" "}
        {v.run.value.assessment?.report?.unverified?.join(" / ") || "記載なし"}
      </p>
      <h4>証拠の現在の状態</h4>
      <ul>
        {Object.entries(v.evidence_current).map(([key, e]) => (
          <li key={key}>
            {key} · {sourceKinds[e.kind] ?? e.kind} · {e.state} ·{" "}
            {e.description}
            <br />
            <code>
              {e.artifact.uri} / {e.artifact.revision} / {e.artifact.digest}
            </code>
          </li>
        ))}
      </ul>
      <details>
        <summary>固定入力・Manifest・原レポート</summary>
        <pre>{format(v)}</pre>
      </details>
    </section>
  );
  return (
    <details className="panel workflow-editor" aria-label="独立検品・証拠">
      <summary>独立検品・証拠</summary>
      <p>
        固定成果物と選んだ条件・入力で検品します。初見検品はContextを無注入にします。実装の会話や自己評価は自動で継承しません。
      </p>
      <label>
        一覧の種類
        <select
          value={type}
          onChange={(e) => {
            setType(e.target.value);
            setPage(undefined);
          }}
        >
          <option value="definition">検品の定義</option>
          <option value="evidence">証拠</option>
        </select>
      </label>
      <button disabled={busy} onClick={() => void act(() => list())}>
        一覧を取得
      </button>
      <ul>
        {page?.items.map((e) => (
          <li key={e.id}>
            <button
              disabled={busy}
              onClick={() => void act(() => load(e.id, "head"))}
            >
              {String(e.value.name ?? e.id)} · {e.version}
            </button>
          </li>
        ))}
      </ul>
      {page?.next && (
        <button disabled={busy} onClick={() => void act(() => list(page.next))}>
          次の一覧ページ
        </button>
      )}
      <label>
        検品・証拠ID
        <input
          value={id}
          onChange={(e) => {
            setID(e.target.value);
            clear();
          }}
        />
      </label>
      <label>
        取得する版
        <input value={version} onChange={(e) => setVersion(e.target.value)} />
      </label>
      <button disabled={busy || !id} onClick={() => void act(() => load())}>
        固定版を取得
      </button>
      {canEdit && (
        <>
          <button disabled={busy} onClick={() => fresh(false)}>
            検品定義を新規作成
          </button>
          <button disabled={busy} onClick={() => fresh(true)}>
            証拠を新規登録
          </button>
        </>
      )}
      {draft && (
        <>
          <p>
            共有版: {record?.version ?? "未保存"} / 下書きの前提版:{" "}
            {draft.version}
          </p>
          <label>
            検品・証拠のJSON
            <textarea
              className="workflow-editor"
              readOnly={!canEdit}
              value={draft.text}
              onChange={(e) => persist({ ...draft, text: e.target.value })}
            />
          </label>
          <button
            disabled={busy || !canEdit}
            onClick={() =>
              void act(async () => {
                const r = await mutate(
                  `/records/${encodeURIComponent(id)}`,
                  draft.version,
                  { value_utf8: draft.text },
                );
                await saveLocalDraft<Draft>(
                  native,
                  namespace,
                  id,
                  undefined,
                  "inspection",
                );
                await load(r.data.id, r.data.version);
                setNotice("共有へ保存しました。採用・開始はしていません。");
              })
            }
          >
            内容を確認して共有へ保存
          </button>
          {record && (
            <details>
              <summary>取得した共有の内容</summary>
              <pre>{format(record.value)}</pre>
            </details>
          )}
        </>
      )}
      {record?.kind === "definition" && (
        <>
          <button
            disabled={busy || !canEdit}
            onClick={() =>
              void act(async () => {
                const r = await mutate("/adopt", adoption?.version ?? "0", {
                  definition: { id: record.id, version: record.version },
                });
                setAdoption(r.data);
                setNotice("この固定版を採用しました。実行は始まっていません。");
              })
            }
          >
            取得した固定版を採用
          </button>
          <p>
            採用記録:{" "}
            {adoption ? `${adoption.id}@${adoption.version}` : "未取得・未採用"}
          </p>
          <label>
            接続端末ID
            <input value={device} onChange={(e) => setDevice(e.target.value)} />
          </label>
          <label>
            固定リポジトリ指定（不要ならnull）
            <textarea
              value={repository}
              onChange={(e) => setRepository(e.target.value)}
            />
          </label>
          <button
            disabled={busy || !canExecute || !adoption || !device}
            onClick={() =>
              void act(async () => {
                const target = `run:${crypto.randomUUID()}`;
                await mutate(`/start/${encodeURIComponent(target)}`, "0", {
                  adoption,
                  device_id: device,
                  repository: JSON.parse(repository) as unknown,
                });
                setRun(target);
                await showRun(target);
                setNotice(
                  "許可と必須分離能力を照合し、共通Runへ実行を依頼しました。",
                );
              })
            }
          >
            採用版で新しい検品を依頼
          </button>
          <button disabled={busy} onClick={() => void act(() => showHistory())}>
            検品・判断の履歴を取得
          </button>
          <ul>
            {history?.items.map((e) => (
              <li key={`${e.id}@${e.version}`}>
                {e.kind} · {e.id}@{e.version}
                {e.kind === "launch" ? (
                  <button
                    disabled={busy}
                    onClick={() =>
                      void act(() => showRun(String(e.value.run_id)))
                    }
                  >
                    この検品の判定を表示
                  </button>
                ) : (
                  <details>
                    <summary>記録の内容</summary>
                    <pre>{format(e.value)}</pre>
                  </details>
                )}
              </li>
            ))}
          </ul>
          {history?.next && (
            <button
              disabled={busy}
              onClick={() => void act(() => showHistory(history.next))}
            >
              次の履歴ページ
            </button>
          )}
        </>
      )}
      <label>
        検品Run ID
        <input value={run} onChange={(e) => setRun(e.target.value)} />
      </label>
      <button disabled={busy || !run} onClick={() => void act(() => showRun())}>
        判定と現在の証拠を取得
      </button>
      {view && (
        <>
          {renderView(view, "選択した判定")}
          <label>
            比較する検品Run ID
            <input
              value={compare}
              onChange={(e) => setCompare(e.target.value)}
            />
          </label>
          <button
            disabled={busy || !compare}
            onClick={() =>
              void act(async () =>
                setOther(
                  (await call(
                    `/current/${encodeURIComponent(compare)}`,
                  )) as View,
                ),
              )
            }
          >
            別の判定を並べて表示
          </button>
          {other && renderView(other, "比較する判定")}
          {canEdit && (
            <>
              <p>
                人の例外判断は別の記録として保存します。元のFAIL・INCONCLUSIVEや実行権限は変わりません。
              </p>
              <label>
                人の判断JSON
                <textarea
                  className="workflow-editor"
                  value={decision}
                  onChange={(e) => setDecision(e.target.value)}
                />
              </label>
              <button
                disabled={busy}
                onClick={() =>
                  void act(async () => {
                    await mutate(
                      `/judgments/inspection-judgment:${crypto.randomUUID()}`,
                      "0",
                      JSON.parse(decision) as object,
                    );
                    setNotice(
                      "人の判断を保存しました。元の判定と実行権限は保持されています。",
                    );
                  })
                }
              >
                内容を確認して人の判断を保存
              </button>
            </>
          )}
        </>
      )}
      <p role="status" aria-live="polite">
        {notice}
      </p>
    </details>
  );
}
