import { Field } from "./field";
import {
  ScreenContent,
  NavigationLink,
  useNavigation,
  useScreenActive,
} from "./navigation";
import { useEffect, useRef, useState } from "react";
import { RunUsage } from "./run-usage";
import { syncRequest, workspaceAccessLost } from "./auth";
import {
  answerScopeEpoch,
  allowAnswerScope,
  readLocalDraft,
  saveLocalDraft,
  syncStorage,
} from "./sync-store";
import { SyncHTTPError } from "./sync-error";
import {
  PauseAnswer,
  PauseResponse,
  QuestionView,
  question,
} from "./run-pause";
import type { Pause } from "./run-pause";
import type { OutputContextSource } from "./context-generation";
import { RunRecords } from "./runs";
import { Tabs, TabPanel } from "./tabs";
import { StatusBadge } from "./status-badge";
import { WorkflowExecution } from "./workflow-execution";
import { InspectionResult } from "./inspections";

type Ref = { id: string; version: string };
type Summary = {
  name?: string;
  definition?: Ref;
  controller?: string;
  state: string;
  observation?: string;
  stage?: string;
  reason?: string;
  requested_by?: string;
  device?: string;
  recipient_name?: string;
  updated_at?: string;
  active_pause?: Ref;
  assessment?: { state: string; verdict?: string };
};
type Entry<T = Summary> = Ref & { kind: string; value: T };
type Page<T = Summary> = { items: Entry<T>[]; next: string; snapshot?: string };
type Artifact = {
  uri: string;
  revision: string;
  digest?: string;
  range?: string;
  delivery?: string;
  state?: string;
};
type InputMeta = {
  artifacts?: Artifact[];
  previous?: Ref;
  sources?: Ref[];
  derivation?: { name: string; version: string };
};
type Note = {
  kind: "route" | "return";
  subject: Ref;
  pause?: Ref;
  recipient?: string;
  reason: string;
  targets?: Artifact[];
  criteria?: string[];
  problem?: string;
  expected?: string;
  by?: string;
  at?: string;
};
type Member = { id: string; name: string; role: string; active: boolean };
const stateNames: Record<string, string> = {
  queued: "未実行",
  running: "実行中",
  paused: "質問・条件待ち",
  succeeded: "正常終了",
  failed: "実行失敗",
  cancelled: "取消確認済み",
  waiting: "待機中",
  completed: "工程完了",
  stopped: "工程停止",
  unknown: "未確認",
};
const reasons: Record<string, string> = {
  human_decision: "人の判断待ち",
  capacity: "実行枠待ち",
  resource: "実行条件待ち",
  budget: "予算条件の確認待ち",
};
export function nextAction(v: Summary) {
  if (v.observation === "unknown") return "端末の報告・所在を照合する";
  if (v.assessment?.verdict === "FAIL" || v.state === "failed")
    return "問題と証拠を確認し、差し戻しを判断する";
  if (v.assessment?.verdict === "INCONCLUSIVE")
    return "未確認事項と追加の判断を確認する";
  if (v.state === "paused" || v.state === "waiting")
    return "質問・待機理由と回答できる操作を確認する";
  if (v.state === "queued") return "開始の依頼・割当状況を確認する";
  return "成果物・実行状況・証拠を確認する";
}
export function WorkState({
  value: v,
  stale = false,
  unsent = false,
}: {
  value: Summary;
  stale?: boolean;
  unsent?: boolean;
}) {
  return (
    <div className="work-state">
      {stale && (
        <p role="status">
          古いキャッシュです。現在の状態と同期確定は未確認です。
        </p>
      )}
      {unsent && <p>未送信（端末内の回答案）</p>}
      <StatusBadge label="実行" value={stateNames[v.state] ?? v.state} />
      <StatusBadge
        label="観測"
        tone={v.observation === "unknown" || stale ? "warning" : "neutral"}
        value={
          v.observation === "unknown"
            ? "Unknown（所在・継続は未確認）"
            : stale
              ? "現在は未確認"
              : "取得済み"
        }
      />
      {v.assessment && (
        <StatusBadge
          label="検品"
          tone={
            v.assessment.state === "invalid" || v.assessment.verdict === "FAIL"
              ? "negative"
              : v.assessment.verdict === "PASS"
                ? "positive"
                : "warning"
          }
          value={
            v.assessment.state === "invalid"
              ? "形式不正・判定を利用できません"
              : (v.assessment.verdict ?? "判定なし")
          }
        />
      )}
    </div>
  );
}

export function Workbench({
  native,
  api,
  workspace,
  user,
  role,
  canExecute,
  members,
  membersNext,
  onGenerate,
}: {
  native: boolean;
  api: string;
  workspace: string;
  user: string;
  role: string;
  canExecute: boolean;
  members: Member[];
  membersNext?: string;
  onGenerate?: (source: OutputContextSource) => void;
}) {
  const namespace = { api, user, workspace };
  const navigation = useNavigation();
  const navigationRef = useRef(navigation);
  navigationRef.current = navigation;
  const visible = useScreenActive();
  const detailEpoch = useRef(0);
  const listEpoch = useRef(0);
  const base = `/workbench/workspaces/${encodeURIComponent(workspace)}`;
  const runBase = `/runs/workspaces/${encodeURIComponent(workspace)}`;
  const [page, setPage] = useState<Page>();
  const [mine, setMine] = useState(false);
  const [directID, setDirectID] = useState("");
  const [directVersion, setDirectVersion] = useState("head");
  const [stale, setStale] = useState(true);
  const [detailStale, setDetailStale] = useState(true);
  const [syncedAt, setSyncedAt] = useState("");
  const [selected, setSelected] = useState<Entry>();
  const [input, setInput] = useState<InputMeta>();
  const [inspection, setInspection] =
    useState<Parameters<typeof InspectionResult>[0]["value"]>();
  const [inspectionCriteria, setInspectionCriteria] = useState<
    { id: string; requirement: string }[]
  >([]);
  const detailHeading = useRef<HTMLHeadingElement>(null);
  const [pauses, setPauses] = useState<Page<Pause>>();
  const [related, setRelated] = useState<Page<{ previous: Ref; next: Ref }>>();
  const [notes, setNotes] = useState<Page<Note>>();
  const [history, setHistory] = useState<Page<Note>>();
  const [busy, setBusy] = useState(false),
    [notice, setNotice] = useState("");
  const [recipient, setRecipient] = useState("");
  const [routeReason, setRouteReason] = useState("");
  const [criteria, setCriteria] = useState("");
  const [problem, setProblem] = useState(""),
    [expected, setExpected] = useState("");
  const [targetURI, setTargetURI] = useState("");
  const [targetVersion, setTargetVersion] = useState("");
  const [people, setPeople] = useState<Member[]>(members);
  const [memberCursor, setMemberCursor] = useState(membersNext ?? "");
  const [memberMore, setMemberMore] = useState(!!membersNext);
  const pending = useRef(new Map<string, { id: string; operation: string }>());
  const canEdit = role !== "viewer";
  const clearDetail = () => {
    setSelected(undefined);
    setInput(undefined);
    setInspection(undefined);
    setInspectionCriteria([]);
    setPauses(undefined);
    setRelated(undefined);
    setNotes(undefined);
    setHistory(undefined);
  };
  const request = async (path: string, body?: unknown) => {
    const epoch = answerScopeEpoch(namespace);
    const result = await syncRequest(path, native, {
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
        setPage(undefined);
        clearDetail();
        setStale(true);
        setDetailStale(true);
        await saveLocalDraft(
          native,
          namespace,
          "list",
          undefined,
          "workbench-cache",
        );
        if (await workspaceAccessLost(native, workspace, error))
          await syncStorage(native).remove(namespace);
        setNotice(
          "現在の閲覧・回答権限を確認してください。共有内容を画面から取り除きました。",
        );
      } else {
        setStale(true);
        setDetailStale(true);
        setNotice(
          "取得・保存を確認できません。表示済みの情報は古い可能性があります。接続と現在の版を確認してください。",
        );
      }
    } finally {
      setBusy(false);
    }
  };
  const query = async <T,>(body: unknown) =>
    ((await request(base + "/query", body)) as { data: Page<T> }).data;
  const list = async (after = "", filter = mine) => {
    const epoch = ++listEpoch.current;
    const value = await query<Summary>({
      kind: "work",
      mine: filter,
      after,
      limit: 50,
    });
    if (epoch !== listEpoch.current) return;
    const at = new Date().toISOString();
    setPage(value);
    setStale(false);
    setSyncedAt(at);
    await saveLocalDraft(
      native,
      namespace,
      "list",
      { page: value, at, mine: filter },
      "workbench-cache",
    );
  };
  const notePage = (entry: Ref, kind = "notes", after = "") =>
    query<Note>({
      kind,
      subject: { id: entry.id, version: entry.version },
      after,
      limit: 50,
    });
  const readDetail = async (ref: Ref, kind = "run", name?: string) => {
    const mine = ++detailEpoch.current;
    const prefix =
      kind === "execution"
        ? `/workflows/workspaces/${encodeURIComponent(workspace)}`
        : runBase;
    const r = (await request(
      `${prefix}/records/${encodeURIComponent(ref.id)}?version=${encodeURIComponent(ref.version)}`,
    )) as Entry;
    if (kind === "execution" && !r.value.name && !name && r.value.definition) {
      const labels = (await request(`${prefix}/labels`, [
        r.value.definition,
      ])) as { name?: string }[];
      name = labels[0]?.name;
    }
    const entry = {
      ...r,
      kind,
      value: {
        ...r.value,
        name: r.value.name ?? name,
        requested_by: r.value.requested_by ?? r.value.controller,
      },
    };
    const noteItems = await notePage(entry);
    let meta: InputMeta | undefined;
    let questions: Page<Pause> | undefined;
    let links: Page<{ previous: Ref; next: Ref }> | undefined;
    let view: Parameters<typeof InspectionResult>[0]["value"] | undefined;
    let criteria: { id: string; requirement: string }[] = [];
    if (kind === "run") {
      const [inputResult, pauseResult, linkResult] = await Promise.all([
        request(
          `${base}/input/${encodeURIComponent(r.id)}?version=${encodeURIComponent(r.version)}`,
        ),
        request(runBase + "/query", {
          kind: "pause",
          run_id: r.id,
          version: r.version,
          limit: 50,
        }),
        request(runBase + "/query", {
          kind: "related",
          run_id: r.id,
          version: r.version,
          limit: 50,
        }),
      ]);
      meta = inputResult as InputMeta;
      questions = (pauseResult as { data: Page<Pause> }).data;
      links = (linkResult as { data: Page<{ previous: Ref; next: Ref }> }).data;
      const definition = meta.sources?.find((v) =>
        v.id.startsWith("inspection:"),
      );
      const launch = meta.sources?.find((v) =>
        v.id.startsWith("inspection-launch:"),
      );
      if (definition && launch) {
        const [result, fixed] = await Promise.all([
          request(
            `/inspections/workspaces/${encodeURIComponent(workspace)}/current/${encodeURIComponent(r.id)}`,
          ),
          request(
            `/inspections/workspaces/${encodeURIComponent(workspace)}/records/${encodeURIComponent(definition.id)}?version=${encodeURIComponent(definition.version)}`,
          ),
        ]);
        view = result as Parameters<typeof InspectionResult>[0]["value"];
        criteria = (fixed as { value: { criteria: typeof criteria } }).value
          .criteria;
      }
    }
    if (mine !== detailEpoch.current) return;
    setCriteria("");
    setProblem("");
    setExpected("");
    setRecipient("");
    setRouteReason("");
    setTargetURI(meta?.artifacts?.[0]?.uri ?? "");
    setTargetVersion(meta?.artifacts?.[0]?.revision ?? "");
    setInput(meta);
    setPauses(questions);
    setRelated(links);
    setInspection(view);
    setInspectionCriteria(criteria);
    setSelected(entry);
    setNotes(noteItems);
    setHistory(undefined);
    setDetailStale(false);
    const currentNavigation = navigationRef.current;
    if (
      currentNavigation?.route.screen === "work" &&
      currentNavigation.route.target?.id === r.id
    )
      currentNavigation.navigate(
        {
          ...currentNavigation.route,
          target: {
            id: r.id,
            version: r.version,
            kind,
            name: entry.value.name,
          },
        },
        { replace: true },
      );
  };
  const open = async (ref: Ref, kind = "run", name?: string, view?: string) => {
    if (navigation)
      navigation.navigate(
        {
          ...navigation.route,
          workspace,
          screen: "work",
          view,
          target: { ...ref, kind, name },
        },
        { related: !navigation.route.target },
      );
    else await readDetail(ref, kind, name);
  };
  useEffect(() => {
    if (!visible || !navigation || navigation.route.screen !== "work") return;
    const target = navigation.route.target;
    if (!target) {
      detailEpoch.current++;
      clearDetail();
      return;
    }
    if (!["run", "execution"].includes(target.kind)) return;
    if (selected?.id === target.id && selected.version === target.version)
      return;
    void act(() => readDetail(target, target.kind, target.name));
  }, [
    visible,
    navigation?.route.target?.id,
    navigation?.route.target?.version,
    navigation?.route.target?.kind,
  ]);
  useEffect(() => {
    if (
      !visible ||
      navigation?.route.target ||
      navigation?.route.view === "prepare"
    )
      return;
    let active = true;
    const filter = navigation?.route.query === "mine";
    const cursor = navigation?.route.cursor ?? "";
    setMine(filter);
    void act(async () => {
      const saved = await readLocalDraft<{
        page: Page;
        at: string;
        mine: boolean;
      }>(native, namespace, "list", "workbench-cache");
      if (!active) return;
      if (saved && saved.mine === filter && !cursor) {
        setPage(saved.page);
        setSyncedAt(saved.at);
        setStale(true);
      }
      await list(cursor, filter);
    });
    return () => {
      active = false;
      listEpoch.current++;
    };
  }, [
    native,
    api,
    user,
    workspace,
    visible,
    navigation?.route.query,
    navigation?.route.cursor,
    navigation?.route.target?.id,
    navigation?.route.view === "prepare",
  ]);
  useEffect(() => {
    setPeople(members);
    setMemberCursor(membersNext ?? "");
    setMemberMore(!!membersNext);
  }, [members, membersNext]);
  useEffect(() => {
    if (selected) {
      detailHeading.current?.focus();
      detailHeading.current?.scrollIntoView({ block: "start" });
    }
  }, [selected?.id, selected?.version]);
  const saveNote = async (note: Note) => {
    const key = JSON.stringify(note);
    if (!pending.current.has(key))
      pending.current.set(key, {
        id: "work-note:" + crypto.randomUUID(),
        operation: crypto.randomUUID(),
      });
    const operation = pending.current.get(key)!;
    const response = (await request(
      `${base}/notes/${encodeURIComponent(operation.id)}`,
      { note, operation_id: operation.operation, expected_version: "0" },
    )) as { data?: Ref };
    if (!response.data) throw Error("note_not_accepted");
    setNotes(await notePage(note.subject));
    pending.current.delete(key);
    setNotice(
      note.kind === "route"
        ? "回付を保存しました。相手の閲覧・回答・実行権限は変更していません。通知の確認や回答の受領は別です。"
        : "理由付き差し戻しを保存しました。修正・再検品のRunはまだ開始していません。",
    );
  };
  const morePeople = async (after = "") => {
    const response = (await request(
      `/access/workspaces/${encodeURIComponent(workspace)}/members${after ? "?after=" + encodeURIComponent(after) : ""}`,
    )) as { data: { items: Member[]; next_cursor?: string } };
    setPeople((old) => [
      ...new Map(
        [...old, ...response.data.items].map((m) => [m.id, m]),
      ).values(),
    ]);
    setMemberCursor(response.data.next_cursor ?? "");
    setMemberMore(!!response.data.next_cursor);
  };
  const selectedRef = selected && {
    id: selected.id,
    version: selected.version,
  };
  const answerable =
    selected?.kind === "run"
      ? pauses?.items.find(
          (r) =>
            r.id === selected.value.active_pause?.id &&
            r.version === selected.value.active_pause.version &&
            r.value.response_kind !== "event",
        )
      : undefined;
  const summary =
    page?.items.find(
      (item) => item.id === selected?.id && item.version === selected?.version,
    )?.value ?? selected?.value;
  const waitingReason =
    summary?.reason ??
    pauses?.items.find((p) => p.id === selected?.value.active_pause?.id)?.value
      .reason;
  const prepare = navigation?.route.view === "prepare";
  const detailView = [
    "overview",
    "input",
    "questions",
    "evidence",
    "history",
  ].includes(navigation?.route.view ?? "")
    ? navigation!.route.view!
    : "overview";
  const changeView = (view: string) =>
    navigation?.navigate({ ...navigation.route, view }, { replace: true });
  const returnList = () =>
    navigation?.navigate({
      workspace,
      screen: "work",
      query: navigation.route.query,
      cursor: navigation.route.cursor,
    });
  return (
    <WorkflowExecution
      native={native}
      api={api}
      user={user}
      workspace={workspace}
      selected={selected?.kind === "execution" ? selectedRef : undefined}
      visible={
        visible &&
        ["overview", "questions"].includes(detailView) &&
        selected?.kind === "execution"
      }
      canEdit={canEdit}
      canExecute={canExecute}
      onSelect={(ref) => void act(() => open(ref, "execution"))}
      render={(execution) => (
        <RunRecords
          native={native}
          api={api}
          workspace={workspace}
          user={user}
          role={role}
          canExecute={canExecute}
          selected={selected?.kind === "run" ? selected : undefined}
          onGenerate={onGenerate}
          view={visible ? detailView : undefined}
          onSelect={(ref) => open(ref)}
          onPrepare={(id) =>
            navigation?.navigate({
              ...navigation.route,
              target: {
                id,
                version: "0",
                kind: "run-preparation",
                name: "新しいRunの下書き",
              },
              view: "prepare",
            })
          }
        >
          {(run) => (
            <ScreenContent>
              {
                <section
                  className="workbench workflow-editor"
                  aria-label="作業と判断"
                >
                  <p>
                    目的と待機理由から、成果物・質問・証拠へ進めます。画面を読むだけでは実行を開始しません。
                  </p>
                  {notice && <p role="status">{notice}</p>}
                  <button
                    disabled={!canEdit}
                    onClick={() =>
                      navigation?.navigate({
                        ...navigation.route,
                        target: {
                          ...run.preparation,
                          kind: "run-preparation",
                          name: "新しいRunの下書き",
                        },
                        view: "prepare",
                      })
                    }
                  >
                    新しいRunを準備
                  </button>
                  {!canEdit && <p>Runの準備・保存には編集権限が必要です。</p>}
                  {prepare && (
                    <section aria-label="Runの準備">
                      <button onClick={returnList}>作業一覧へ戻る</button>
                      {run.prepare}
                    </section>
                  )}
                  {!prepare && !navigation?.route.target && (
                    <>
                      <details>
                        <summary>RunをIDで開く（詳細操作）</summary>
                        <Field
                          label="Run ID"
                          value={directID}
                          onChange={setDirectID}
                        />
                        <Field
                          label="Runの版"
                          value={directVersion}
                          onChange={setDirectVersion}
                          hint="headは最新を確認してから、その固定版を開きます。"
                        />
                        <button
                          disabled={!directID.trim() || !directVersion.trim()}
                          onClick={() =>
                            void act(() =>
                              open({
                                id: directID.startsWith("run:")
                                  ? directID
                                  : "run:" + directID,
                                version: directVersion,
                              }),
                            )
                          }
                        >
                          指定したRunを開く
                        </button>
                      </details>
                      <div className="team-controls">
                        <label>
                          <input
                            type="checkbox"
                            checked={mine}
                            disabled={busy}
                            onChange={(e) => {
                              if (navigation)
                                navigation.navigate(
                                  {
                                    ...navigation.route,
                                    query: e.target.checked
                                      ? "mine"
                                      : undefined,
                                    cursor: undefined,
                                  },
                                  { replace: true },
                                );
                              else {
                                setMine(e.target.checked);
                                void act(() => list("", e.target.checked));
                              }
                            }}
                          />
                          自分に回された判断依頼
                        </label>
                        <button
                          disabled={busy}
                          onClick={() =>
                            void act(() => list(navigation?.route.cursor ?? ""))
                          }
                        >
                          作業一覧を更新
                        </button>
                      </div>
                      <p>
                        最終同期：
                        {syncedAt
                          ? new Date(syncedAt).toLocaleString()
                          : "未取得"}{" "}
                        /{" "}
                        {stale
                          ? "古い可能性あり・現在は未確認"
                          : "一覧のAPI取得を確認"}
                      </p>
                      {page && (
                        <>
                          <div
                            className="table-scroll"
                            role="region"
                            aria-label="作業一覧"
                            tabIndex={0}
                          >
                            <table className="work-list">
                              <thead>
                                <tr>
                                  <th scope="col">作業・固定版</th>
                                  <th scope="col">実行・検品・観測</th>
                                  <th scope="col">工程・待機理由</th>
                                  <th scope="col">担当・端末</th>
                                  <th scope="col">次の操作</th>
                                </tr>
                              </thead>
                              <tbody>
                                {page.items.map((entry) => (
                                  <tr key={entry.id}>
                                    <th scope="row">
                                      <NavigationLink
                                        id={`work-row-${entry.id}`}
                                        className="record-link"
                                        related
                                        to={{
                                          workspace,
                                          screen: "work",
                                          query: navigation?.route.query,
                                          cursor: navigation?.route.cursor,
                                          target: {
                                            id: entry.id,
                                            version: entry.version,
                                            kind: entry.kind,
                                            name: entry.value.name,
                                          },
                                        }}
                                      >
                                        {entry.value.name ?? "作業"}
                                      </NavigationLink>
                                      <small>
                                        {entry.kind} · {entry.version}
                                      </small>
                                    </th>
                                    <td>
                                      <WorkState
                                        value={entry.value}
                                        stale={stale}
                                      />
                                    </td>
                                    <td>
                                      {entry.value.stage ?? "Run"}
                                      <p>
                                        {reasons[entry.value.reason ?? ""] ??
                                          entry.value.reason ??
                                          "待機理由の報告なし"}
                                      </p>
                                    </td>
                                    <td>
                                      {entry.value.recipient_name ??
                                        entry.value.requested_by ??
                                        "担当未確認"}
                                      <p>
                                        {entry.value.device ??
                                          "端末の割当情報なし"}
                                      </p>
                                    </td>
                                    <td>
                                      <NavigationLink
                                        related
                                        to={{
                                          workspace,
                                          screen: "work",
                                          query: navigation?.route.query,
                                          cursor: navigation?.route.cursor,
                                          target: {
                                            id: entry.id,
                                            version: entry.version,
                                            kind: entry.kind,
                                            name: entry.value.name,
                                          },
                                          view:
                                            entry.value.observation ===
                                            "unknown"
                                              ? "overview"
                                              : entry.value.state ===
                                                    "paused" ||
                                                  entry.value.state ===
                                                    "waiting"
                                                ? "questions"
                                                : "evidence",
                                        }}
                                      >
                                        {nextAction(entry.value)}
                                      </NavigationLink>
                                    </td>
                                  </tr>
                                ))}
                              </tbody>
                            </table>
                          </div>
                          {!page.items.length && (
                            <p>このページに表示できる作業はありません。</p>
                          )}
                          {page.next && (
                            <button
                              disabled={busy}
                              onClick={() =>
                                navigation
                                  ? navigation.navigate(
                                      {
                                        ...navigation.route,
                                        cursor: page.next,
                                      },
                                      { replace: true },
                                    )
                                  : void act(() => list(page.next))
                              }
                            >
                              作業の次のページ
                            </button>
                          )}
                        </>
                      )}
                    </>
                  )}
                  {!prepare &&
                    selected &&
                    selectedRef &&
                    (!navigation ||
                      (navigation.route.target?.id === selected.id &&
                        navigation.route.target.version ===
                          selected.version)) && (
                      <article aria-label="選択した作業">
                        <button onClick={returnList}>作業一覧へ戻る</button>
                        <h3 ref={detailHeading} tabIndex={-1}>
                          {selected.value.name ?? "作業の詳細"}
                        </h3>
                        <WorkState value={selected.value} stale={detailStale} />
                        <p>
                          待機理由：
                          {reasons[waitingReason ?? ""] ??
                            waitingReason ??
                            "報告なし"}{" "}
                          / 担当：
                          {summary?.recipient_name ??
                            people.find((p) => p.id === summary?.requested_by)
                              ?.name ??
                            summary?.requested_by ??
                            "未確認"}{" "}
                          / 端末：{summary?.device ?? "割当情報なし"}
                        </p>
                        <p>次の操作：{nextAction(selected.value)}</p>
                        <button
                          disabled={busy}
                          onClick={() =>
                            void act(() =>
                              readDetail(
                                { ...selectedRef, version: "head" },
                                selected.kind,
                                selected.value.name,
                              ),
                            )
                          }
                        >
                          この作業の状態・質問を更新
                        </button>
                        <Tabs
                          id="work-detail"
                          label="作業の詳細"
                          items={[
                            { id: "overview", label: "概要" },
                            { id: "input", label: "入力" },
                            { id: "questions", label: "質問・判断" },
                            { id: "evidence", label: "成果・検品" },
                            { id: "history", label: "履歴・利用量" },
                          ]}
                          value={detailView}
                          onChange={changeView}
                        />
                        <TabPanel
                          id="work-detail"
                          value="overview"
                          active={detailView}
                        >
                          {run.overview}
                          {selected.kind === "execution" && execution}
                        </TabPanel>
                        <TabPanel
                          id="work-detail"
                          value="input"
                          active={detailView}
                        >
                          {run.input}
                        </TabPanel>
                        <TabPanel
                          id="work-detail"
                          value="evidence"
                          active={detailView}
                        >
                          {input && (
                            <section aria-label="対象と由来">
                              <h4>今回の対象・固定成果物</h4>
                              {input.artifacts?.length ? (
                                <ul>
                                  {input.artifacts.map((a) => (
                                    <li key={a.uri + a.revision}>
                                      {a.uri} / 版：{a.revision} /{" "}
                                      {a.state === "unfetched"
                                        ? "未取得"
                                        : (a.state ?? "状態未確認")}
                                      {/^https?:\/\//.test(a.uri) && (
                                        <a
                                          href={a.uri}
                                          target="_blank"
                                          rel="noreferrer"
                                        >
                                          成果物を開く
                                        </a>
                                      )}
                                    </li>
                                  ))}
                                </ul>
                              ) : (
                                <p>
                                  入力に指定された固定成果物はありません。質問と原出力の証拠も確認してください。
                                </p>
                              )}
                              {input.previous && (
                                <button
                                  onClick={() =>
                                    void act(() => open(input.previous!))
                                  }
                                >
                                  関連元のRun・判断を確認
                                </button>
                              )}
                              {related?.items.map((r) => (
                                <button
                                  key={r.id}
                                  onClick={() =>
                                    void act(() => open(r.value.next))
                                  }
                                >
                                  関連する修正・再検品Runを確認
                                </button>
                              ))}
                              {related?.next && (
                                <button
                                  onClick={() =>
                                    void act(async () => {
                                      const v = (await request(
                                        runBase + "/query",
                                        {
                                          kind: "related",
                                          run_id: selected.id,
                                          version: selected.version,
                                          after: related.next,
                                          snapshot: related.snapshot,
                                          limit: 50,
                                        },
                                      )) as { data: typeof related };
                                      setRelated(v.data);
                                    })
                                  }
                                >
                                  関連Runの次のページ
                                </button>
                              )}
                            </section>
                          )}
                          {inspection && (
                            <section aria-label="検品条件と証拠">
                              <h4>検品時の条件</h4>
                              <ul>
                                {inspectionCriteria.map((c) => (
                                  <li key={c.id}>
                                    {c.requirement}（{c.id}）
                                  </li>
                                ))}
                              </ul>
                              <InspectionResult
                                value={inspection}
                                title="検品結果・未確認事項・証拠"
                              />
                            </section>
                          )}

                          {canEdit && selected.kind === "run" && (
                            <details>
                              <summary>理由付き差し戻しを記録</summary>
                              <fieldset disabled={busy || detailStale}>
                                <p>
                                  この版の成果物と問題を記録します。記録だけでは修正commandを起動しません。
                                </p>
                                <Field
                                  label="対象成果物のURL・識別子"
                                  value={targetURI}
                                  onChange={setTargetURI}
                                />
                                <label>
                                  対象成果物の固定版
                                  <input
                                    value={targetVersion}
                                    onChange={(e) =>
                                      setTargetVersion(e.target.value)
                                    }
                                  />
                                </label>
                                <label>
                                  該当する条件
                                  <textarea
                                    value={criteria}
                                    onChange={(e) =>
                                      setCriteria(e.target.value)
                                    }
                                    placeholder={pauses?.items
                                      .flatMap(
                                        (p) =>
                                          question(
                                            p.value.request,
                                          )?.criteria?.map((c) => c.id) ?? [],
                                      )
                                      .join(", ")}
                                    rows={2}
                                  />
                                </label>
                                <Field
                                  label="観測した問題"
                                  value={problem}
                                  onChange={setProblem}
                                  rows={3}
                                />
                                <Field
                                  label="期待する結果"
                                  value={expected}
                                  onChange={setExpected}
                                  rows={3}
                                />
                                <button
                                  disabled={
                                    !criteria ||
                                    !problem ||
                                    !expected ||
                                    !targetURI ||
                                    !targetVersion
                                  }
                                  onClick={() =>
                                    void act(() =>
                                      saveNote({
                                        kind: "return",
                                        subject: selectedRef,
                                        reason: "修正と再検品を依頼",
                                        targets: [
                                          input?.artifacts?.find(
                                            (a) =>
                                              a.uri === targetURI &&
                                              a.revision === targetVersion,
                                          ) ?? {
                                            uri: targetURI,
                                            revision: targetVersion,
                                            delivery: "reference",
                                            state: "unfetched",
                                          },
                                        ],
                                        criteria: criteria
                                          .split(/[,\n]/)
                                          .map((s) => s.trim())
                                          .filter(Boolean),
                                        problem,
                                        expected,
                                      }),
                                    )
                                  }
                                >
                                  この版への差し戻しを保存
                                </button>
                              </fieldset>
                            </details>
                          )}
                          {run.records}
                        </TabPanel>
                        <TabPanel
                          id="work-detail"
                          value="questions"
                          active={detailView}
                        >
                          {selected.kind === "execution" && execution}
                          {!canEdit && (
                            <p>
                              閲覧権限では回答・回付・差し戻しを保存できません。
                            </p>
                          )}
                          {selected.kind === "run" &&
                            pauses &&
                            !pauses.items.length && (
                              <p>このページには質問がありません。</p>
                            )}
                          {pauses?.items.map((r) => (
                            <section key={r.id} aria-label="質問と回答">
                              <p>{r.value.reason}</p>
                              <QuestionView value={r.value.request} />
                              <p>
                                質問の状態：
                                {(
                                  {
                                    waiting: "未回答",
                                    received: "API受付済み",
                                    delivered: "Runner配送確認済み",
                                    resolved: "継続確認済み",
                                    cancelled: "終了により回答待ちを解除",
                                  } as Record<string, string>
                                )[r.value.state] ?? r.value.state}
                              </p>
                              {r.value.response !== undefined && (
                                <>
                                  <p>
                                    API回答受付：
                                    {r.value.answered_at ?? "未確認"} / 回答者：
                                    {people.find(
                                      (m) => m.id === r.value.answered_by,
                                    )?.name ?? "履歴で確認"}
                                  </p>
                                  <PauseResponse
                                    value={r.value.response}
                                    request={r.value.request}
                                  />
                                </>
                              )}
                              <p>
                                Runner配送：{r.value.delivered_at ?? "未確認"} /
                                実際の継続：
                                {r.value.resumed_at ?? "未確認"}
                              </p>
                              {canEdit && answerable?.id === r.id && (
                                <PauseAnswer
                                  native={native}
                                  namespace={namespace}
                                  runID={selected.id}
                                  record={r}
                                  busy={busy || detailStale}
                                  onAccepted={() =>
                                    act(() =>
                                      readDetail(
                                        { ...selectedRef, version: "head" },
                                        selected.kind,
                                        selected.value.name,
                                      ),
                                    )
                                  }
                                  onDenied={(error) =>
                                    act(async () => {
                                      throw error;
                                    })
                                  }
                                />
                              )}
                            </section>
                          ))}
                          {pauses?.next && (
                            <button
                              onClick={() =>
                                void act(async () => {
                                  const v = (await request(runBase + "/query", {
                                    kind: "pause",
                                    run_id: selected.id,
                                    version: selected.version,
                                    after: pauses.next,
                                    limit: 50,
                                  })) as { data: typeof pauses };
                                  setPauses(v.data);
                                })
                              }
                            >
                              質問の次のページ
                            </button>
                          )}
                          {canEdit && (
                            <details>
                              <summary>判断依頼を担当者へ回す</summary>
                              <fieldset disabled={busy || detailStale}>
                                <label>
                                  担当者
                                  <select
                                    value={recipient}
                                    onChange={(e) =>
                                      setRecipient(e.target.value)
                                    }
                                  >
                                    <option value="">担当者を選択</option>
                                    {people
                                      .filter(
                                        (m) => m.active && m.role !== "viewer",
                                      )
                                      .map((m) => (
                                        <option key={m.id} value={m.id}>
                                          {m.name}
                                        </option>
                                      ))}
                                  </select>
                                </label>
                                {memberMore && (
                                  <button
                                    onClick={() =>
                                      void act(() => morePeople(memberCursor))
                                    }
                                  >
                                    担当者候補の次のページ
                                  </button>
                                )}
                                <Field
                                  label="回す理由"
                                  value={routeReason}
                                  onChange={setRouteReason}
                                  rows={2}
                                />
                                <button
                                  disabled={!recipient || !routeReason}
                                  onClick={() =>
                                    void act(() =>
                                      saveNote({
                                        kind: "route",
                                        subject: selectedRef,
                                        ...(answerable
                                          ? {
                                              pause: {
                                                id: answerable.id,
                                                version: answerable.version,
                                              },
                                            }
                                          : {}),
                                        recipient,
                                        reason: routeReason,
                                      }),
                                    )
                                  }
                                >
                                  この判断依頼を回す
                                </button>
                              </fieldset>
                            </details>
                          )}
                        </TabPanel>

                        <TabPanel
                          id="work-detail"
                          value="history"
                          active={detailView}
                        >
                          {selected.kind === "run" && (
                            <RunUsage
                              key={workspace + selected.id}
                              native={native}
                              workspace={workspace}
                              runID={selected.id}
                              onDenied={(e) => {
                                void act(async () => {
                                  throw e;
                                });
                              }}
                            />
                          )}
                          {run.records}
                          <section aria-label="回付と差し戻しの記録">
                            <h4>判断依頼・差し戻し</h4>
                            {notes?.items.map((n) => (
                              <div key={n.id}>
                                <p>
                                  {n.value.kind === "route"
                                    ? "担当者への回付"
                                    : "理由付き差し戻し"}
                                  ：{n.value.reason}
                                </p>
                                {n.value.problem && (
                                  <p>
                                    問題：{n.value.problem} / 期待結果：
                                    {n.value.expected}
                                  </p>
                                )}
                                {n.value.criteria && (
                                  <p>条件：{n.value.criteria.join(", ")}</p>
                                )}
                                <details>
                                  <summary>対象版・記録の詳細</summary>
                                  <pre>{JSON.stringify(n.value, null, 2)}</pre>
                                </details>
                              </div>
                            ))}
                            {notes?.next && (
                              <button
                                onClick={() =>
                                  void act(async () =>
                                    setNotes(
                                      await notePage(
                                        selectedRef,
                                        "notes",
                                        notes.next,
                                      ),
                                    ),
                                  )
                                }
                              >
                                補足記録の次のページ
                              </button>
                            )}
                            <button
                              disabled={busy}
                              onClick={() =>
                                void act(async () =>
                                  setHistory(
                                    await notePage(selectedRef, "history"),
                                  ),
                                )
                              }
                            >
                              回付・差し戻しの履歴を確認
                            </button>
                            {history?.items.map((r) => (
                              <details key={r.id + r.version}>
                                <summary>
                                  {r.value.kind === "route"
                                    ? "回付"
                                    : "差し戻し"}{" "}
                                  / {r.value.at}
                                </summary>
                                <pre>{JSON.stringify(r.value, null, 2)}</pre>
                              </details>
                            ))}
                            {history?.next && (
                              <button
                                onClick={() =>
                                  void act(async () =>
                                    setHistory(
                                      await notePage(
                                        selectedRef,
                                        "history",
                                        history.next,
                                      ),
                                    ),
                                  )
                                }
                              >
                                履歴の次のページ
                              </button>
                            )}
                          </section>
                        </TabPanel>
                      </article>
                    )}
                </section>
              }
            </ScreenContent>
          )}
        </RunRecords>
      )}
    />
  );
}
