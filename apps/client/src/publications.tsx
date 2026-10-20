import { Field } from "./field";
import { PageList } from "./page-list";
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
  connection: { repository: string };
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
    [requestID, setRequestID] = useState(""),
    [request, setRequest] = useState<Entry>();
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
        setNotice(
          "端末の編集内容を復元しました。共有の現在版と起票要求を確認してください。自動送信はしません。",
        );
      }
      if (pending) {
        setProposal(pending);
        setRequestID(pending.id);
      }
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
    ).catch(() => setNotice("下書きの端末保存を確認できません。"));
  };
  const forgetProposal = () => {
    setProposal(undefined);
    void saveLocalDraft<Proposal>(
      native,
      namespace,
      "active",
      undefined,
      "publication-proposal",
    ).catch(() => setNotice("送信案の端末保存を確認できません。"));
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
        setNotice(
          "現在の閲覧・編集・実行権限と、許可された起票接続を確認してください。",
        );
      } else
        setNotice(
          "操作を完了できませんでした。端末の下書きは自動送信しません。接続・現在版・要求の状態を確認してください。",
        );
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
      setRequestID(value.id);
      if (proposal?.id === value.id) forgetProposal();
    }
  };
  const showRequest = (value: Entry, owner: string) => {
    const current = navigationRef.current;
    if (
      current?.route.screen === "publications" &&
      JSON.stringify(current.route.target) !== owner
    )
      return;
    setRequest(value);
    setRequestID(value.id);
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
  const readRequest = async (target = requestID) => {
    const owner = JSON.stringify(navigation?.route.target);
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
    setNotice("共有下書きを保存しました。外部起票は依頼していません。");
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
    setRequestID(target);
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
      setNotice("既存の共有要求を取得しました。新しい作成は依頼していません。");
      return;
    }
    await call(`/submit/${encodeURIComponent(proposal.id)}`, {
      ...proposal.mutation,
      draft: proposal.draft,
    });
    await readRequest(proposal.id);
    setNotice("共有で受け付けました。外部作成の確認とは別の状態です。");
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
    setNotice("端末内の新しい下書きです。自動送信しません。");
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
  const shared = entry?.kind === "draft" ? entry : undefined;
  const result = request?.value as Request | undefined;
  const issue =
    result && request ? publicationIssue(request.id, result) : undefined;
  return (
    <ScreenContent>
      {
        <section className="panel workflow-editor" aria-label="外部Issueの起票">
          <h3>外部Issueの起票</h3>
          <p>
            下書き、共有受付、GitHubでの作成確認を分けて記録します。応答を失った要求は再作成せず、本文末尾の照合コメントで探します。
          </p>
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
                <details>
                  <summary>原出力を確認</summary>
                  <pre>{format(incoming.value)}</pre>
                </details>
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
                    setNotice(
                      "固定出力を含む端末の下書きを準備しました。共有保存・外部送信は行っていません。",
                    );
                  }}
                >
                  この固定出力を含む新規下書きを準備
                </button>
              </section>
            )}
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
          <button disabled={busy} onClick={() => void act(() => list())}>
            起票の一覧を取得
          </button>
          <ul>
            {page?.items.map((value) => (
              <li key={value.id}>
                <button
                  disabled={busy}
                  onClick={() => void act(() => load(value.id, "head"))}
                >
                  {value.value.name || value.value.title || value.id} ·{" "}
                  {states[value.value.state ?? ""] ?? value.version}
                </button>
              </li>
            ))}
          </ul>
          {page?.next && (
            <button
              disabled={busy}
              onClick={() => void act(() => list(page.next))}
            >
              次の起票一覧ページ
            </button>
          )}
          <Field label="共有下書き・要求ID" value={id} onChange={setID} />
          <Field
            label="取得する起票記録の版"
            value={version}
            onChange={setVersion}
          />
          <button disabled={busy || !id} onClick={() => void act(() => load())}>
            起票の固定記録を取得
          </button>
          {canEdit && (
            <button disabled={busy} onClick={fresh}>
              起票下書きを新規作成
            </button>
          )}
          {editor && (
            <fieldset disabled={!canEdit || busy}>
              <legend>端末の下書き · {editor.id}</legend>
              <p>
                前提版: {editor.version} / 取得した共有版:{" "}
                {shared?.version ?? "未取得"}
              </p>
              {(
                [
                  ["name", "下書きの名前"],
                  ["connection", "起票接続ID"],
                  ["title", "GitHub Issueのタイトル"],
                ] as const
              ).map(([key, label]) => (
                <label key={key}>
                  {label}
                  <input
                    value={editor.value[key]}
                    onChange={(e) => {
                      edit({ ...editor.value, [key]: e.target.value });
                    }}
                  />
                </label>
              ))}
              <label>
                GitHub Issueの本文
                <textarea
                  value={editor.value.body}
                  onChange={(e) => {
                    edit({ ...editor.value, body: e.target.value });
                  }}
                />
              </label>
              <p className="note">
                本文末尾に通常表示では見えない照合用HTMLコメントを追加します。認証情報は入力しないでください。
              </p>
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
                  <p className="context-warning">
                    この下書きは、上に表示した出力とは別の参照元を持っています。出力の切替だけでは下書きの内容・参照元を変更しません。
                  </p>
                )}
              <details>
                <summary>参照元の固定記録</summary>
                <label>
                  起票の参照元JSON
                  <textarea
                    key={`${editor.id}@${editor.version}`}
                    defaultValue={format(editor.value.sources)}
                    onBlur={(e) => {
                      try {
                        const sources = JSON.parse(e.target.value) as Ref[];
                        edit({ ...editor.value, sources });
                      } catch {
                        setNotice(
                          "参照元をJSONの固定ID・版の配列で指定してください。",
                        );
                      }
                    }}
                  />
                </label>
              </details>
              <button onClick={() => void act(save)}>
                下書きを確認して共有へ保存
              </button>
              {shared && (
                <button
                  onClick={() => {
                    persist({
                      id: shared.id,
                      version: shared.version,
                      value: shared.value as Draft,
                    });
                    forgetProposal();
                  }}
                >
                  取得した共有内容から編集し直す
                </button>
              )}
            </fieldset>
          )}
          {shared && (
            <details>
              <summary>取得した共有下書き</summary>
              <pre>{format(shared.value)}</pre>
            </details>
          )}
          <button
            disabled={busy || !canEdit || !editor || editor.version === "0"}
            onClick={() => void act(preview)}
          >
            固定下書きから送信内容をプレビュー
          </button>
          {proposal && (
            <section aria-label="起票の送信前プレビュー">
              <h4>送信前プレビュー · 外部未送信</h4>
              <p>
                対象: {proposal.preview.connection.repository} / 要求ID:{" "}
                {proposal.id}
              </p>
              <p>{proposal.preview.input.title}</p>
              <pre>{proposal.preview.external_body}</pre>
              <button
                disabled={busy || !canEdit || !canExecute}
                onClick={() => void act(submit)}
              >
                この固定内容で起票を依頼
              </button>
            </section>
          )}
          <Field
            label="確認する起票要求ID"
            value={requestID}
            onChange={setRequestID}
          />
          <button
            disabled={busy || !requestID}
            onClick={() => void act(() => readRequest())}
          >
            起票要求の状態を取得
          </button>
          {request && result && (
            <section aria-label="外部起票の確認">
              <h4>{states[result.state] ?? result.state}</h4>
              <p>
                要求: {request.id}@{request.version} / 共通Run: {result.run_id}
              </p>
              <p>
                主体: {result.actor.kind} · {result.actor.user_id}
              </p>
              <p>
                確認時刻: {result.observed_at ?? "未確認"} / 理由:{" "}
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
                disabled={busy}
                onClick={() =>
                  void act(async () => {
                    const owner = JSON.stringify(navigation?.route.target);
                    const updated = (await call(
                      `/refresh/${encodeURIComponent(request.id)}`,
                      { version: request.version },
                    )) as Entry;
                    showRequest(updated, owner);
                  })
                }
              >
                既存Issueと照合・外部内容を再取得
              </button>
              {result.external && (
                <>
                  <p>
                    外部Issue: {result.external.html_url} ·{" "}
                    {result.external.state}
                  </p>
                  <h4>{result.external.title}</h4>
                  <pre>{result.external.body}</pre>
                  <p>外部更新日時: {result.external.updated_at}</p>
                </>
              )}
              <details>
                <summary>受付した固定内容と原記録</summary>
                <pre>{format(result)}</pre>
              </details>
            </section>
          )}
          {(entry || request) && (
            <>
              <button
                disabled={busy}
                onClick={() => void act(() => historyPage())}
              >
                起票操作の履歴を取得
              </button>
              <ul>
                {history?.items.map((value) => (
                  <li key={`${value.id}@${value.version}`}>
                    {value.id}@{value.version}
                    <details>
                      <summary>起票履歴の内容</summary>
                      <pre>{format(value.value)}</pre>
                    </details>
                  </li>
                ))}
              </ul>
              {history?.next && (
                <button
                  disabled={busy}
                  onClick={() => void act(() => historyPage(history.next))}
                >
                  次の起票履歴ページ
                </button>
              )}
            </>
          )}
          <p role="status" aria-live="polite">
            {notice}
          </p>
        </section>
      }
    </ScreenContent>
  );
}
