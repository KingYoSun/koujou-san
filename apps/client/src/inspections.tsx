import { Disclosure } from "./disclosure";
import { Field, jsonError } from "./field";
import { ScreenContent } from "./navigation";
import { DetailPane, SidebarPane } from "./panes";
import { StatusBadge } from "./status-badge";
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
type ReportData = {
  criteria_results?: CriterionResult[];
  unverified?: string[];
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
        report?: unknown;
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
const count = (p: Page, empty: string) =>
  p.items.length
    ? `${p.items.length}件を表示${p.next ? "（続きあり）" : ""}`
    : empty;
const acceptedReport = (v: View) =>
  v.run.value.assessment?.state === "valid"
    ? (v.run.value.assessment.report as ReportData | undefined)
    : undefined;
const verdicts: Record<string, string> = {
  PASS: "合格",
  FAIL: "不合格",
  INCONCLUSIVE: "判断不能",
};
const verdict = (code?: string) => (code ? (verdicts[code] ?? code) : "未判定");
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

export function InspectionResult({
  value: v,
  title,
  criteria = [],
}: {
  value: View;
  title: string;
  criteria?: { id: string; requirement: string }[];
}) {
  const report = acceptedReport(v);
  const results = report?.criteria_results ?? [];
  const rows = [
    ...criteria.map((criterion) => ({
      id: criterion.id,
      label: criterion.requirement,
      result: results.find((item) => item.criterion_id === criterion.id),
    })),
    ...results
      .filter(
        (item) =>
          !criteria.some((criterion) => criterion.id === item.criterion_id),
      )
      .map((result) => ({
        id: result.criterion_id,
        label: result.criterion_id,
        result,
      })),
  ];
  return (
    <section aria-label={title}>
      <h4>{title}</h4>
      <p>
        <StatusBadge
          label="判定"
          value={verdict(v.run.value.assessment?.verdict)}
        />{" "}
        <StatusBadge
          label="形式"
          value={v.run.value.assessment?.state ?? "未確認"}
        />{" "}
        <StatusBadge
          label="現在の適用"
          value={
            v.validity === "STALE"
              ? "採用中の基準と不一致"
              : "採用中の基準に一致"
          }
        />
      </p>
      <Disclosure title="検品環境・固定入力の分離">
        {() => (
          <>
            <p>
              実行：{v.run.value.state} / 終了コード：
              {v.run.value.exit_code ?? "未観測"} / Context：
              {v.launch.value.manifest.state === "none"
                ? "明示的に無注入"
                : v.launch.value.manifest.state}
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
          </>
        )}
      </Disclosure>
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
            {rows.map(({ id, label, result: c }) => (
              <tr key={id}>
                <td>{label}</td>
                <td>{verdict(c?.verdict)}</td>
                <td>
                  {c ? (c.executed ? "実行済み" : "未実行") : "実行未確認"}
                </td>
                <td>
                  <details>
                    <summary>証拠 {c?.evidence_refs?.length ?? 0}件</summary>
                    {c?.evidence_refs
                      ?.map((e) => `${e.id}@${e.version}`)
                      .join(", ") || "なし"}
                  </details>
                </td>
                <td>
                  {c ? c.unverified?.join(" / ") || "記載なし" : "報告待ち"}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <p>
        全体の未確認：
        {report
          ? report.unverified?.join(" / ") || "記載なし"
          : "形式未確認・不正のため未判定"}
      </p>
      <h4>証拠の現在の状態</h4>
      <ul>
        {Object.entries(v.evidence_current).map(([key, e]) => (
          <li key={key}>
            {key} · {sourceKinds[e.kind] ?? e.kind} · {e.state} ·{" "}
            {e.description}
            <br />
            {/^https?:\/\//.test(e.artifact.uri) && (
              <a href={e.artifact.uri} target="_blank" rel="noreferrer">
                この証拠を開く
              </a>
            )}
            <details>
              <summary>証拠の固定版・取得先</summary>
              <code>
                {e.artifact.uri} / {e.artifact.revision} / {e.artifact.digest}
              </code>
            </details>
          </li>
        ))}
      </ul>
      <Disclosure title="固定入力・Manifest・原レポート">
        {() => <pre>{format(v)}</pre>}
      </Disclosure>
    </section>
  );
}

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
  const fetching = (label: string) =>
    busy ? `${label}を取得中…` : `${label}を取得`;
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
        setNotice("閲覧・編集・実行権限が不足しています。");
      } else setNotice("操作を完了できませんでした。");
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
          acceptedReport(result)?.criteria_results?.map(
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
    setNotice("端末内に新しい下書きを作成しました。");
  };
  return (
    <ScreenContent>
      <SidebarPane>
        <section aria-label="検品定義一覧">
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
          <button
            data-variant="secondary"
            disabled={busy}
            aria-busy={busy}
            onClick={() => void act(() => list())}
          >
            {fetching("検品・証拠一覧")}
          </button>
          {page && (
            <>
              <p>
                {count(
                  page,
                  type === "definition"
                    ? "検品定義はありません。"
                    : "証拠はありません。",
                )}
              </p>
              <ul>
                {page.items.map((e) => (
                  <li key={e.id}>
                    <button
                      data-variant="secondary"
                      disabled={busy}
                      aria-current={e.id === id ? "true" : undefined}
                      onClick={() => void act(() => load(e.id, "head"))}
                    >
                      {String(e.value.name ?? e.id)} · {e.version}
                    </button>
                  </li>
                ))}
              </ul>
              {page.next && (
                <button
                  data-variant="secondary"
                  disabled={busy}
                  onClick={() => void act(() => list(page.next))}
                >
                  一覧の次のページ
                </button>
              )}
            </>
          )}
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
          <details>
            <summary>IDと版を指定して取得</summary>
            <Field
              label="検品・証拠ID"
              value={id}
              onChange={(value) => {
                setID(value);
                clear();
              }}
            />
            <Field label="取得する版" value={version} onChange={setVersion} />
            <button
              data-variant="secondary"
              disabled={busy || !id}
              aria-busy={busy}
              onClick={() => void act(() => load())}
            >
              {fetching("固定版")}
            </button>
          </details>
        </section>
      </SidebarPane>
      <section className="workflow-editor">
        {draft && (
          <>
            <p>
              共有版：{record?.version ?? "未保存"} / 下書きの前提版：
              {draft.version}
            </p>
            {canEdit && (
              <>
                <Field
                  label="検品・証拠のJSON"
                  rows={12}
                  value={draft.text}
                  onChange={(text) => persist({ ...draft, text })}
                  error={jsonError(draft.text)}
                />
                <button
                  disabled={busy || !!jsonError(draft.text)}
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
                      setNotice("共有へ保存しました。");
                    })
                  }
                >
                  共有へ保存
                </button>
              </>
            )}
            {record && (
              <Disclosure title="取得した共有の内容">
                {() => <pre>{format(record.value)}</pre>}
              </Disclosure>
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
                  setNotice("この固定版を採用しました。");
                })
              }
            >
              取得した固定版を採用
            </button>
            <p>
              採用記録：
              {adoption
                ? `${adoption.id}@${adoption.version}`
                : "未取得・未採用"}
            </p>
            <Field label="接続端末ID" value={device} onChange={setDevice} />
            <Field
              label="固定リポジトリ（JSON）"
              rows={4}
              value={repository}
              onChange={setRepository}
              error={jsonError(repository)}
            />
            <button
              disabled={
                busy ||
                !canExecute ||
                !adoption ||
                !device ||
                !!jsonError(repository)
              }
              title={
                !canExecute
                  ? "実行権限がありません"
                  : !adoption
                    ? "固定版を採用してください"
                    : !device
                      ? "接続端末IDを入力してください"
                      : undefined
              }
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
                  setNotice("共通Runへ検品の実行を依頼しました。");
                })
              }
            >
              採用版で新しい検品を依頼
            </button>
            <button
              data-variant="secondary"
              disabled={busy}
              aria-busy={busy}
              onClick={() => void act(() => showHistory())}
            >
              {fetching("検品・判断の履歴")}
            </button>
            {history && (
              <>
                <p>{count(history, "履歴はありません。")}</p>
                <ul>
                  {history.items.map((e) => (
                    <li key={`${e.id}@${e.version}`}>
                      {e.kind} · {e.id}@{e.version}
                      {e.kind === "launch" ? (
                        <button
                          data-variant="secondary"
                          disabled={busy}
                          onClick={() =>
                            void act(() => showRun(String(e.value.run_id)))
                          }
                        >
                          この検品の判定を表示
                        </button>
                      ) : (
                        <Disclosure title="記録の内容">
                          {() => <pre>{format(e.value)}</pre>}
                        </Disclosure>
                      )}
                    </li>
                  ))}
                </ul>
              </>
            )}
            {history?.next && (
              <button
                data-variant="secondary"
                disabled={busy}
                onClick={() => void act(() => showHistory(history.next))}
              >
                履歴の次のページ
              </button>
            )}
          </>
        )}
        <Field label="検品Run ID" value={run} onChange={setRun} />
        <button
          data-variant="secondary"
          disabled={busy || !run}
          aria-busy={busy}
          onClick={() => void act(() => showRun())}
        >
          {fetching("判定と現在の証拠")}
        </button>
        {view && (
          <>
            <Field
              label="比較する検品Run ID"
              value={compare}
              onChange={setCompare}
            />
            <button
              data-variant="secondary"
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
            {other && <InspectionResult value={other} title="比較する判定" />}
            {canEdit && (
              <>
                <Field
                  label="人の判断JSON"
                  rows={8}
                  value={decision}
                  onChange={setDecision}
                  error={jsonError(decision)}
                />
                <button
                  disabled={busy || !!jsonError(decision)}
                  onClick={() =>
                    void act(async () => {
                      await mutate(
                        `/judgments/inspection-judgment:${crypto.randomUUID()}`,
                        "0",
                        JSON.parse(decision) as object,
                      );
                      setNotice("人の判断を保存しました。");
                    })
                  }
                >
                  人の判断を保存
                </button>
              </>
            )}
          </>
        )}
        <p role="status">{notice}</p>
      </section>
      {view && (
        <DetailPane>
          <InspectionResult value={view} title="選択した検品の判定" />
        </DetailPane>
      )}
    </ScreenContent>
  );
}
