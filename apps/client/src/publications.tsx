import { Disclosure } from "./disclosure";
import { Field, jsonError } from "./field";
import { PageList } from "./page-list";
import { DetailPane, SidebarPane } from "./panes";
import { at, SourceLink } from "./github-observations";
import { StatusBadge } from "./status-badge";
import {
  NavigationLink,
  ScreenContent,
  useNavigation,
  useScreenActive,
} from "./navigation";
import { useEffect, useRef, useState } from "react";
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
type Draft = {
  name: string;
  connection: string;
  title: string;
  body: string;
  sources: Ref[];
};
type Entry = Ref & { owner_id: string; kind: string; value: Draft | Request };
type Request = {
  draft: Ref;
  input: Draft;
  state: string;
  reason: string;
  external_body: string;
  run_id: string;
  connection: { repository: string; provider?: string; instance?: string };
  actor: { kind: string; user_id: string; control?: Ref };
  observed_at?: string;
  external?: {
    number: number;
    html_url: string;
    title: string;
    body: string;
    state: string;
    updated_at: string;
  };
};
type Page = {
  items: (Ref & { value: { name?: string; title?: string; state?: string } })[];
  next: string;
  through?: string;
};
type Mutation = { operation_id: string; expected_version: string };
type Editor = {
  id: string;
  version: string;
  value: Draft;
  pending?: Mutation & { value_utf8: string };
};
type Proposal = {
  id: string;
  draft: Ref;
  preview: Request;
  mutation: Mutation;
};
const empty = (): Draft => ({
  name: "",
  connection: "",
  title: "",
  body: "",
  sources: [],
});
const format = (v: unknown) => JSON.stringify(v, null, 2);
const sameDraft = (a: Draft, b: Draft) =>
  a.name === b.name &&
  a.connection === b.connection &&
  a.title === b.title &&
  a.body === b.body &&
  format(a.sources) === format(b.sources);
// Hopper creates the Issue and publication with the same suffix. Only link
// the fixed Issue actually recorded in the accepted publication's sources.
export function publicationIssue(id: string, value: Request): Ref | undefined {
  if (
    value.actor.kind !== "automation" ||
    value.actor.control?.id !== "control:hopper" ||
    !id.startsWith("publication:hopper-")
  )
    return;
  return value.input.sources.find(
    (ref) =>
      ref.id === id.replace(/^publication:/, "scope:") &&
      !!ref.version &&
      ref.version !== "0" &&
      ref.version !== "head",
  );
}
const count = (p: Page, empty: string) =>
  p.items.length
    ? `${p.items.length}件を表示${p.next ? "（続きあり）" : ""}`
    : empty;
const states: Record<string, string> = {
  preview: "送信前プレビュー",
  accepted: "共有受付・外部未確認",
  sending: "送信中・外部未確認",
  unknown: "結果不明・再作成停止",
  created: "外部作成を確認",
  failed: "作成されなかったことを確認",
  held: "現在の内容・権限の確認待ち",
};

export function Publications({
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
  const navigation = useNavigation();
  const navigationRef = useRef(navigation);
  navigationRef.current = navigation;
  const active = useScreenActive();
  const targetOwner = useRef(JSON.stringify(navigation?.route.target));
  if (active) targetOwner.current = JSON.stringify(navigation?.route.target);
  const [incoming, setIncoming] = useState<{
    run: Ref;
    output: Ref;
    value: unknown;
  }>();
  const namespace = { api, user, workspace },
    base = `/publications/workspaces/${encodeURIComponent(workspace)}`,
    canEdit = role !== "viewer";
  const [id, setID] = useState(""),
    [version, setVersion] = useState("head"),
    [kind, setKind] = useState("draft");
  const [editor, setEditor] = useState<Editor>(),
    [entry, setEntry] = useState<Entry>(),
    [page, setPage] = useState<Page>(),
    [history, setHistory] = useState<Page>();
  const [proposal, setProposal] = useState<Proposal>(),
    [request, setRequest] = useState<Entry>(),
    [sourcesText, setSourcesText] = useState<string>();
  const [busy, setBusy] = useState(false),
    [notice, setNotice] = useState("");
  const epoch = useRef(answerScopeEpoch(namespace));
  useEffect(() => {
    let active = true;
    void Promise.all([
      readLocalDraft<Editor>(native, namespace, "active", "publication-editor"),
      readLocalDraft<Proposal>(
        native,
        namespace,
        "active",
        "publication-proposal",
      ),
    ]).then(([saved, pending]) => {
      if (!active || !allowAnswerScope(namespace, epoch.current)) return;
      if (saved) {
        setEditor(saved);
        setID(saved.id);
        setNotice("端末の下書きを復元しました。");
      }
      if (pending) setProposal(pending);
    });
    return () => {
      active = false;
    };
  }, [native, api, user, workspace]);
  const call = async (path: string, body?: unknown) => {
    const before = answerScopeEpoch(namespace);
    const value = await syncRequest(base + path, native, {
      method: body === undefined ? "GET" : "POST",
      body,
    });
    if (!allowAnswerScope(namespace, before))
      throw new SyncHTTPError(403, undefined);
    return value;
  };
  const persist = (value: Editor) => {
    setEditor(value);
    void saveLocalDraft(
      native,
      namespace,
      "active",
      value,
      "publication-editor",
    ).catch(() => setNotice("下書きを端末に保存できませんでした。"));
  };
  const forgetProposal = () => {
    setProposal(undefined);
    void saveLocalDraft<Proposal>(
      native,
      namespace,
      "active",
      undefined,
      "publication-proposal",
    ).catch(() => setNotice("送信案を端末に保存できませんでした。"));
  };
  const edit = (value: Draft) => {
    if (editor) persist({ ...editor, value, pending: undefined });
    forgetProposal();
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
        setEntry(undefined);
        setRequest(undefined);
        setPage(undefined);
        setHistory(undefined);
        if (await workspaceAccessLost(native, workspace, error)) {
          setEditor(undefined);
          setEntry(undefined);
          setRequest(undefined);
          setProposal(undefined);
          setPage(undefined);
          setHistory(undefined);
          await syncStorage(native).remove(namespace);
        } else if (editor?.version !== "0") {
          try {
            await call(`/records/${encodeURIComponent(editor?.id ?? id)}`);
          } catch {
            setEditor(undefined);
            setEntry(undefined);
            setRequest(undefined);
            setProposal(undefined);
            await saveLocalDraft<Editor>(
              native,
              namespace,
              "active",
              undefined,
              "publication-editor",
            );
            await saveLocalDraft<Proposal>(
              native,
              namespace,
              "active",
              undefined,
              "publication-proposal",
            );
          }
        }
        setNotice("閲覧・編集・実行権限または起票接続が不足しています。");
      } else setNotice("操作を完了できませんでした。");
    } finally {
      setBusy(false);
    }
  };
  const load = async (
    target = id,
    requested = version,
    current = () => true,
  ) => {
    const value = (await call(
      `/records/${encodeURIComponent(target)}?version=${encodeURIComponent(requested)}`,
    )) as Entry;
    if (!current()) return;
    setID(target);
    setEntry(value);
    setHistory(undefined);
    if (value.kind === "draft") {
      if (!editor || editor.id !== value.id)
        persist({
          id: value.id,
          version: value.version,
          value: value.value as Draft,
        });
      else if (editor.version !== value.version)
        setNotice(
          "下書きの前提版と共有版が異なります。共有内容と比較してください。",
        );
    } else {
      setRequest(value);
      if (proposal?.id === value.id) forgetProposal();
    }
  };
  const showRequest = (value: Entry, owner: string) => {
    const current = navigationRef.current;
    if (targetOwner.current !== owner) return;
    setRequest(value);
    if (proposal?.id === value.id) forgetProposal();
    if (current?.route.screen === "publications")
      current.navigate(
        {
          ...current.route,
          target: { id: value.id, version: value.version, kind: "publication" },
        },
        { replace: true },
      );
  };
  const readRequest = async (target: string) => {
    const owner = targetOwner.current;
    const value = (await call(
      `/records/${encodeURIComponent(target)}`,
    )) as Entry;
    if (value.kind !== "request") throw new Error("not_request");
    showRequest(value, owner);
  };
  const list = async (after = "") => {
    const value = (await call("/query", {
      kind: "heads",
      type: kind,
      after,
      limit: 50,
    })) as { data: Page };
    setPage(value.data);
  };
  const save = async () => {
    if (!editor) return;
    const source = format(editor.value);
    const pending =
      editor.pending?.value_utf8 === source
        ? editor.pending
        : {
            operation_id: crypto.randomUUID(),
            expected_version: editor.version,
            value_utf8: source,
          };
    const planned = { ...editor, pending };
    persist(planned);
    await saveLocalDraft(
      native,
      namespace,
      "active",
      planned,
      "publication-editor",
    );
    const result = (await call(
      `/records/${encodeURIComponent(editor.id)}`,
      pending,
    )) as { data: Ref };
    const loaded = (await call(
      `/records/${encodeURIComponent(result.data.id)}?version=${encodeURIComponent(result.data.version)}`,
    )) as Entry;
    setEntry(loaded);
    persist({
      id: loaded.id,
      version: loaded.version,
      value: loaded.value as Draft,
    });
    setProposal(undefined);
    await saveLocalDraft<Proposal>(
      native,
      namespace,
      "active",
      undefined,
      "publication-proposal",
    );
    setNotice("共有下書きを保存しました。");
  };
  const preview = async () => {
    if (!editor) return;
    const current = (await call(
      `/records/${encodeURIComponent(editor.id)}`,
    )) as Entry;
    if (
      current.version !== editor.version ||
      !sameDraft(current.value as Draft, editor.value)
    ) {
      setEntry(current);
      throw new Error("draft_changed");
    }
    const target = `publication:${crypto.randomUUID()}`;
    const fixed = { id: current.id, version: current.version };
    const value = (await call(`/preview/${encodeURIComponent(target)}`, {
      draft: fixed,
    })) as Request;
    const pending = {
      id: target,
      draft: fixed,
      preview: value,
      mutation: { operation_id: crypto.randomUUID(), expected_version: "0" },
    };
    setProposal(pending);
    await saveLocalDraft(
      native,
      namespace,
      "active",
      pending,
      "publication-proposal",
    );
  };
  const submit = async () => {
    if (!proposal) return;
    const current = (await call(
      `/records/${encodeURIComponent(proposal.draft.id)}`,
    )) as Entry;
    if (current.version !== proposal.draft.version) {
      setEntry(current);
      throw new Error("draft_changed");
    }
    const existing = (await call(
      `/records/${encodeURIComponent(proposal.id)}`,
    ).catch((e: unknown) => {
      if (e instanceof SyncHTTPError && e.status === 403) return undefined;
      throw e;
    })) as Entry | undefined;
    if (existing) {
      setRequest(existing);
      forgetProposal();
      setNotice("既存の起票要求を取得しました。");
      return;
    }
    await call(`/submit/${encodeURIComponent(proposal.id)}`, {
      ...proposal.mutation,
      draft: proposal.draft,
    });
    await readRequest(proposal.id);
    setNotice("起票要求を共有で受け付けました。");
  };
  const historyPage = async (after = "") => {
    const root = request ?? entry;
    if (!root) return;
    const result = (await call("/query", {
      kind: "history",
      id: root.owner_id,
      after,
      through: after ? history?.through : undefined,
      limit: 25,
    })) as { data: Page };
    setHistory(result.data);
  };
  const fresh = () => {
    const target = `publication-draft:${crypto.randomUUID()}`;
    setID(target);
    setEntry(undefined);
    setProposal(undefined);
    setRequest(undefined);
    setHistory(undefined);
    persist({ id: target, version: "0", value: empty() });
    void saveLocalDraft<Proposal>(
      native,
      namespace,
      "active",
      undefined,
      "publication-proposal",
    );
    setNotice("新しい下書きを端末に作成しました。");
  };
  useEffect(() => {
    const route = navigation?.route;
    if (
      !active ||
      route?.screen !== "publications" ||
      route.target?.kind !== "output" ||
      route.origin?.kind !== "run"
    )
      return;
    let current = true;
    setIncoming(undefined);
    const run = { id: route.origin.id, version: route.origin.version },
      output = { id: route.target.id, version: route.target.version };
    void act(async () => {
      const record = (await syncRequest(
        `/runs/workspaces/${encodeURIComponent(workspace)}/records/${encodeURIComponent(output.id)}?version=${encodeURIComponent(output.version)}`,
        native,
        { method: "GET" },
      )) as { run_id: string; kind: string; value: unknown };
      const owner = (await syncRequest(
        `/runs/workspaces/${encodeURIComponent(workspace)}/records/${encodeURIComponent(run.id)}?version=${encodeURIComponent(run.version)}`,
        native,
        { method: "GET" },
      )) as Ref;
      if (record.kind !== "output" || record.run_id !== owner.id)
        throw Error("output_source_mismatch");
      if (current) setIncoming({ run, output, value: record.value });
    });
    return () => {
      current = false;
    };
  }, [
    active,
    navigation?.route.target?.id,
    navigation?.route.target?.version,
    navigation?.route.origin?.id,
    navigation?.route.origin?.version,
  ]);
  useEffect(() => {
    const target = navigation?.route.target;
    if (!active || target?.kind !== "publication") return;
    let current = true;
    void act(() => load(target.id, target.version, () => current));
    return () => {
      current = false;
    };
  }, [active, navigation?.route.target?.id, navigation?.route.target?.version]);
  useEffect(() => setSourcesText(undefined), [editor?.id, editor?.version]);
  const shared = entry?.kind === "draft" ? entry : undefined;
  const selectedTarget = navigation?.route.target;
  const requestMatches =
    !selectedTarget ||
    (selectedTarget.kind === "publication" &&
      request?.id === selectedTarget.id &&
      (selectedTarget.version === "head" ||
        selectedTarget.version === request.version));
  const result = requestMatches
    ? (request?.value as Request | undefined)
    : undefined;
  const issue =
    result && request ? publicationIssue(request.id, result) : undefined;
  const historyView = (entry || request) && (
    <>
      <button
        data-variant="secondary"
        disabled={busy}
        aria-busy={busy}
        onClick={() => void act(() => historyPage())}
      >
        {busy ? "起票操作の履歴を取得中…" : "起票操作の履歴を取得"}
      </button>
      {history && (
        <>
          <p>{count(history, "起票操作の履歴はありません。")}</p>
          <ul>
            {history.items.map((value) => (
              <li key={`${value.id}@${value.version}`}>
                {value.id}@{value.version}
                <Disclosure title="起票履歴の内容">
                  {() => <pre>{format(value.value)}</pre>}
                </Disclosure>
              </li>
            ))}
          </ul>
        </>
      )}
      {history?.next && (
        <button
          data-variant="secondary"
          disabled={busy}
          onClick={() => void act(() => historyPage(history.next))}
        >
          起票履歴の次のページ
        </button>
      )}
    </>
  );
  return (
    <ScreenContent>
      <section className="workflow-editor" aria-label="外部Issueの起票">
        <SidebarPane>
          <section aria-label="起票要求一覧">
            <label>
              一覧の種類
              <select
                value={kind}
                onChange={(e) => {
                  setKind(e.target.value);
                  setPage(undefined);
                }}
              >
                <option value="draft">共有下書き</option>
                <option value="request">起票要求</option>
              </select>
            </label>
            <button
              data-variant="secondary"
              disabled={busy}
              aria-busy={busy}
              onClick={() => void act(() => list())}
            >
              {busy ? "起票一覧を取得中…" : "起票一覧を取得"}
            </button>
            {page && (
              <>
                <p>
                  {count(
                    page,
                    kind === "draft"
                      ? "共有下書きはありません。"
                      : "起票要求はありません。",
                  )}
                </p>
                <ul>
                  {page.items.map((value) => (
                    <li key={value.id}>
                      <button
                        data-variant="secondary"
                        disabled={busy}
                        aria-current={value.id === id ? "true" : undefined}
                        onClick={() => void act(() => load(value.id, "head"))}
                      >
                        {value.value.name || value.value.title || value.id} ·{" "}
                        {states[value.value.state ?? ""] ?? value.version}
                      </button>
                    </li>
                  ))}
                </ul>
              </>
            )}
            {page?.next && (
              <button
                data-variant="secondary"
                disabled={busy}
                onClick={() => void act(() => list(page.next))}
              >
                起票一覧の次のページ
              </button>
            )}
            {canEdit && (
              <button disabled={busy} onClick={fresh}>
                起票下書きを新規作成
              </button>
            )}
            <details>
              <summary>IDと版を指定して取得</summary>
              <Field label="共有下書き・要求ID" value={id} onChange={setID} />
              <Field
                label="取得する起票記録の版"
                value={version}
                onChange={setVersion}
              />
              <button
                data-variant="secondary"
                disabled={busy || !id}
                aria-busy={busy}
                onClick={() => void act(() => load())}
              >
                {busy ? "起票の固定記録を取得中…" : "起票の固定記録を取得"}
              </button>
            </details>
          </section>
        </SidebarPane>
        {incoming &&
          navigation?.route.target?.id === incoming.output.id &&
          navigation.route.target.version === incoming.output.version && (
            <section aria-label="起票元の固定出力">
              <h4>起票元の固定出力</h4>
              <p>
                {incoming.run.id} · {incoming.run.version}
              </p>
              <p>
                {incoming.output.id} · {incoming.output.version}
              </p>
              <Disclosure title="原出力">
                {() => <pre>{format(incoming.value)}</pre>}
              </Disclosure>
              <button
                disabled={!canEdit || busy}
                onClick={() => {
                  forgetProposal();
                  setRequest(undefined);
                  setEntry(undefined);
                  const id = `publication-draft:${crypto.randomUUID()}`;
                  setID(id);
                  persist({
                    id,
                    version: "0",
                    value: {
                      ...empty(),
                      name: "固定出力からの起票",
                      body: format(incoming.value),
                      sources: [incoming.run, incoming.output],
                    },
                  });
                  setNotice("固定出力を含む下書きを端末に作成しました。");
                }}
              >
                固定出力から下書きを作成
              </button>
            </section>
          )}
        {editor && (
          <fieldset disabled={!canEdit || busy}>
            <legend>端末の下書き · {editor.id}</legend>
            <p>
              前提版：{editor.version} / 取得した共有版：
              {shared?.version ?? "未取得"}
            </p>
            {(
              [
                ["name", "下書きの名前"],
                ["connection", "起票接続ID"],
                ["title", "外部Issueのタイトル"],
              ] as const
            ).map(([key, label]) => (
              <Field
                key={key}
                label={label}
                value={editor.value[key]}
                onChange={(v) => edit({ ...editor.value, [key]: v })}
              />
            ))}
            <Field
              label="外部Issueの本文"
              rows={6}
              value={editor.value.body}
              onChange={(body) => edit({ ...editor.value, body })}
            />
            <PageList label="この下書きの参照元" items={editor.value.sources}>
              {(ref, index) => (
                <li key={index}>
                  {ref.id} · {ref.version}
                </li>
              )}
            </PageList>
            {incoming &&
              !editor.value.sources.some(
                (ref) =>
                  ref.id === incoming.output.id &&
                  ref.version === incoming.output.version,
              ) && (
                <p role="status" className="context-warning">
                  参照元に起票元の固定出力が含まれていません。
                </p>
              )}
            <Disclosure title="参照元の固定記録">
              {() => (
                <Field
                  label="起票の参照元JSON"
                  rows={4}
                  value={sourcesText ?? format(editor.value.sources)}
                  error={jsonError(sourcesText ?? "[]")}
                  onChange={(v) => {
                    setSourcesText(v);
                    try {
                      edit({
                        ...editor.value,
                        sources: JSON.parse(v) as Ref[],
                      });
                    } catch {
                      /* the field shows the JSON error until it parses */
                    }
                  }}
                />
              )}
            </Disclosure>
            <button onClick={() => void act(save)}>下書きを共有へ保存</button>
            {shared && (
              <button
                data-variant="secondary"
                onClick={() => {
                  persist({
                    id: shared.id,
                    version: shared.version,
                    value: shared.value as Draft,
                  });
                  setSourcesText(undefined);
                  forgetProposal();
                }}
              >
                共有版から編集し直す
              </button>
            )}
          </fieldset>
        )}
        {shared && (
          <Disclosure title="取得した共有下書き">
            {() => <pre>{format(shared.value)}</pre>}
          </Disclosure>
        )}
        <button
          disabled={busy || !canEdit || !editor || editor.version === "0"}
          onClick={() => void act(preview)}
        >
          送信内容をプレビュー
        </button>
        {proposal && (
          <section aria-label="起票の送信前プレビュー">
            <h4>{states.preview}</h4>
            <p>
              対象：
              {proposal.preview.connection.provider === "gitea"
                ? "Gitea"
                : "GitHub"}{" "}
              · {proposal.preview.connection.instance ?? "https://github.com"} /{" "}
              {proposal.preview.connection.repository} / 要求ID：{proposal.id}
            </p>
            <p>{proposal.preview.input.title}</p>
            <pre>{proposal.preview.external_body}</pre>
            <button
              disabled={busy || !canEdit || !canExecute}
              onClick={() => void act(submit)}
            >
              起票を依頼
            </button>
          </section>
        )}
        {!result && historyView}
        <p role="status">{notice}</p>
        {request && result && (
          <DetailPane>
            <section aria-label="選択した起票要求の詳細">
              <h4>{result.input.title || request.id}</h4>
              <p>
                <StatusBadge
                  label="起票"
                  value={states[result.state] ?? result.state}
                />
              </p>
              <p>
                要求：{request.id}@{request.version} / 共通Run：{result.run_id}
              </p>
              <p>
                主体：{result.actor.kind} · {result.actor.user_id}
              </p>
              <p>
                確認時刻：{at(result.observed_at)} / 理由：
                {result.reason || "なし"}
              </p>
              {issue && (
                <NavigationLink
                  to={{
                    workspace,
                    screen: "priority",
                    target: { ...issue, kind: "scope" },
                  }}
                  related
                >
                  この起票で作成したIssueの優先度を確認
                </NavigationLink>
              )}
              <button
                data-variant="secondary"
                disabled={busy}
                onClick={() =>
                  void act(async () => {
                    const owner = targetOwner.current;
                    const updated = (await call(
                      `/refresh/${encodeURIComponent(request.id)}`,
                      { version: request.version },
                    )) as Entry;
                    showRequest(updated, owner);
                  })
                }
              >
                外部Issueを再照合
              </button>
              {result.external && (
                <>
                  <p>
                    外部Issue：{result.external.html_url} ·{" "}
                    {result.external.state}
                  </p>
                  <SourceLink
                    href={result.external.html_url}
                    native={native}
                    instance={result.connection.instance}
                  />
                  <h4>{result.external.title}</h4>
                  <pre>{result.external.body}</pre>
                  <p>外部更新日時：{at(result.external.updated_at)}</p>
                </>
              )}
              <Disclosure title="受付した固定内容と原記録">
                {() => <pre>{format(result)}</pre>}
              </Disclosure>
              {historyView}
            </section>
          </DetailPane>
        )}
      </section>
    </ScreenContent>
  );
}
