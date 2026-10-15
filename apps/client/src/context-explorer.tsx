import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useStore } from "zustand";
import type {
  UI,
  Ref,
  Candidate,
  CandidatePage,
  GraphPage,
  GraphRequest,
} from "./context-ui";
import {
  labels,
  kinds,
  scopeKinds,
  refKey,
  refLabel,
  identityKey,
  fixedRef,
  recordName,
} from "./context-ui";
import { EnvelopeStatus } from "./context-status";

type Search = {
  text?: string;
  kinds?: string[];
  tags?: string[];
  scopes?: { kind: string; id: string }[];
  ids?: string[];
  revisions?: string[];
  limit: number;
  cursor?: string;
};
export function ContextExplorer({ ui }: { ui: UI }) {
  const state = useStore(ui.store);
  const [tab, setTab] = useState("search");
  const [text, setText] = useState("");
  const [kind, setKind] = useState("");
  const [tag, setTag] = useState("");
  const [id, setID] = useState("");
  const [revision, setRevision] = useState("");
  const [scope, setScope] = useState<Candidate>();
  const [query, setQuery] = useState<Search>({ limit: 100 });
  const [scopeAfter, setScopeAfter] = useState("");
  const [scopeBack, setScopeBack] = useState<string[]>([]);
  const [back, setBack] = useState<Search[]>([]);
  const [graph, setGraph] = useState<GraphRequest>();
  const [graphSource, setGraphSource] = useState("");
  const [graphBack, setGraphBack] = useState<GraphRequest[]>([]);
  const [direction, setDirection] = useState("in");
  const [relation, setRelation] = useState("contains");
  const [depth, setDepth] = useState(1);
  const [history, setHistory] = useState(false);
  const [newKind, setNewKind] = useState("sentence");
  const [newScopeKind, setNewScopeKind] = useState("issue");
  const [draftAfter, setDraftAfter] = useState("");
  const [draftNext, setDraftNext] = useState<string | null>();
  const [draftKeys, setDraftKeys] = useState<string[]>([]);
  const client = useQueryClient();
  const search = useQuery({
    queryKey: [...ui.prefix, "candidates", query],
    enabled: tab === "search",
    retry: false,
    queryFn: () =>
      ui.request<CandidatePage>(
        `/search/workspaces/${encodeURIComponent(ui.workspace)}/candidates`,
        query,
      ),
  });
  const scopes = useQuery({
    queryKey: [...ui.prefix, "scopes", scopeAfter],
    enabled: tab === "scopes",
    retry: false,
    queryFn: () =>
      ui.request<{ items: Candidate[] }>(
        `/contexts/workspaces/${encodeURIComponent(ui.workspace)}/catalog`,
        { kind: "scope", after: scopeAfter },
      ),
  });
  const related = useQuery({
    queryKey: [...ui.prefix, "graph", graph],
    enabled: tab === "graph" && Boolean(graph),
    retry: false,
    queryFn: () =>
      ui.request<GraphPage>(
        `/graph/workspaces/${encodeURIComponent(ui.workspace)}/query`,
        graph,
      ),
  });
  async function create(type: string) {
    try {
      const value =
        type === "scope"
          ? { kind: newScopeKind, name: "新しいScope" }
          : type === "sentence"
            ? { kind: type, name: "新しい文", text: "" }
            : {
                kind: type,
                [type === "paragraph" ? "name" : "title"]:
                  `新しい${labels[type]}`,
                children: [],
              };
      const key = await ui.engine.create(type, value);
      state.choose({ kind: type, id: key.slice(key.indexOf(":") + 1) });
    } catch (error) {
      ui.fail(error);
    }
  }
  function candidate(candidate: Candidate) {
    return (
      <li key={refKey(candidate.ref)} className="context-candidate">
        <button
          className="link-button"
          onClick={() => {
            state.choose(candidate.ref);
            if (candidate.ref.kind === "scope") setScope(candidate);
          }}
        >
          <strong>{candidate.name || candidate.ref.id}</strong>
          <small>
            {candidate.scope_kind
              ? labels[candidate.scope_kind]
              : labels[candidate.ref.kind]}{" "}
            · {candidate.ref.id} · {candidate.ref.revision}
          </small>
        </button>
        <div className="context-actions">
          <button onClick={() => ui.store.setState({ reference: candidate })}>
            参照候補にする
          </button>
          {kinds.includes(candidate.ref.kind) && (
            <>
              <button onClick={() => state.add(candidate.ref)}>
                入力へ追加
              </button>
              <button onClick={() => state.add(candidate.ref, true)}>
                入力から除外
              </button>
            </>
          )}

          {candidate.ref.kind === "document" &&
            scope &&
            query.scopes?.length && (
              <button
                onClick={() => {
                  setGraph({
                    root: candidate.ref,
                    direction: "out",
                    kinds: ["contains"],
                    history: true,
                    depth: 4,
                    limit: 100,
                  });
                  setGraphBack([]);
                  setGraphSource(
                    `Scope「${scope.name}」へ直接所属する文書からの参照。子の直接所属とは別です。`,
                  );
                  setTab("graph");
                }}
              >
                所属文書からの参照を確認
              </button>
            )}
          {candidate.ref.kind === "scope" && (
            <button
              onClick={() => {
                setScope(candidate);
                setText("");
                setKind("");
                setTag("");
                setID("");
                setRevision("");
                setQuery({
                  limit: 100,
                  scopes: [
                    { kind: candidate.scope_kind!, id: candidate.ref.id },
                  ],
                });
                setBack([]);
                setTab("search");
              }}
            >
              直接所属を探す
            </button>
          )}
        </div>
      </li>
    );
  }
  const selected = state.selected;
  const current = selected
    ? ui.engine.state.items[identityKey(selected)]?.record
    : undefined;
  function explore() {
    if (!current) return;
    setGraphSource("");
    const request: GraphRequest = {
      root: fixedRef(state.historical ?? current.ref),
      direction,
      kinds: relation ? [relation] : undefined,
      history,
      depth,
      limit: 100,
    };
    if (JSON.stringify(request) === JSON.stringify(graph))
      void related.refetch();
    else setGraph(request);
    setGraphBack([]);
  }
  async function draftPage(after = "") {
    try {
      const page = await ui.engine.draftPage(after);
      setDraftKeys(page.entries.map((entry) => entry.key));
      setDraftAfter(after);
      setDraftNext(page.next);
    } catch (error) {
      ui.fail(error);
    }
  }
  return (
    <section className="context-explorer" aria-label="Contextを探す">
      <h3>Contextを探す</h3>
      <nav className="context-tabs" aria-label="Contextの操作">
        {[
          ["search", "検索"],
          ["scopes", "Scope"],
          ["graph", "関係・利用先"],
          ["drafts", "下書き"],
        ].map(([value, label]) => (
          <button
            key={value}
            aria-pressed={tab === value}
            onClick={() => {
              setTab(value!);
              if (value === "drafts") void draftPage();
            }}
          >
            {label}
          </button>
        ))}
      </nav>
      <div className="context-create">
        <label>
          作成する粒度
          <select
            value={newKind}
            onChange={(event) => setNewKind(event.target.value)}
          >
            {kinds.map((kind) => (
              <option key={kind} value={kind}>
                {labels[kind]}
              </option>
            ))}
          </select>
        </label>
        <button
          disabled={!ui.canCreate || ui.engine.status !== "current"}
          onClick={() => void create(newKind)}
        >
          Contextを新規作成
        </button>
      </div>
      {tab === "search" && (
        <>
          <form
            onSubmit={(event) => {
              event.preventDefault();
              setBack([]);
              const request: Search = {
                limit: 100,
                text,
                kinds: kind ? [kind] : undefined,
                tags: tag ? [tag] : undefined,
                scopes: scope
                  ? [{ kind: scope.scope_kind!, id: scope.ref.id }]
                  : undefined,
                ids: id ? [id] : undefined,
                revisions: revision ? [revision] : undefined,
              };
              if (JSON.stringify(request) === JSON.stringify(query))
                void search.refetch();
              else setQuery(request);
            }}
          >
            <label>
              検索語
              <input
                aria-label="Contextの検索語"
                value={text}
                onChange={(event) => setText(event.target.value)}
              />
            </label>
            <label>
              粒度
              <select
                aria-label="検索する粒度"
                value={kind}
                onChange={(event) => setKind(event.target.value)}
              >
                <option value="">すべて</option>
                {kinds.map((kind) => (
                  <option key={kind} value={kind}>
                    {labels[kind]}
                  </option>
                ))}
              </select>
            </label>
            <details>
              <summary>分類・ID・版で絞る</summary>
              <label>
                分類タグ
                <input
                  value={tag}
                  onChange={(event) => setTag(event.target.value)}
                />
              </label>
              <label>
                Context ID
                <input
                  value={id}
                  onChange={(event) => setID(event.target.value)}
                />
              </label>
              <label>
                固定版
                <input
                  value={revision}
                  onChange={(event) => setRevision(event.target.value)}
                />
              </label>
            </details>
            {scope && (
              <p>
                直接所属するScope: {scope.name}{" "}
                <button type="button" onClick={() => setScope(undefined)}>
                  範囲を外す
                </button>
              </p>
            )}
            <button>検索する</button>
          </form>
          <EnvelopeStatus value={search.data} error={search.error} />
          {search.data?.data?.items.length === 0 && (
            <p>
              {search.data.meta.completeness.state === "complete"
                ? "指定した範囲に候補はありません。"
                : "候補はまだ確認できていません。"}
            </p>
          )}
          <ul className="context-candidates">
            {search.data?.data?.items.map(candidate)}
          </ul>
          <div className="context-actions">
            <button
              disabled={!back.length}
              onClick={() => {
                setQuery(back.at(-1)!);
                setBack(back.slice(0, -1));
              }}
            >
              前のページ
            </button>
            <button
              disabled={!search.data?.meta.next_cursor}
              onClick={() => {
                setBack([...back, query]);
                setQuery({ ...query, cursor: search.data!.meta.next_cursor! });
              }}
            >
              次の候補
            </button>
            <button onClick={() => void search.refetch()}>
              検索結果を更新
            </button>
          </div>
        </>
      )}
      {tab === "scopes" && (
        <>
          <p className="context-meta">
            本文の構造とは別のGroupingです。Scopeの選択だけでは入力へ注入しません。
          </p>
          <label>
            Scopeの種類
            <select
              value={newScopeKind}
              onChange={(event) => setNewScopeKind(event.target.value)}
            >
              {scopeKinds.map((kind) => (
                <option value={kind} key={kind}>
                  {labels[kind]}
                </option>
              ))}
            </select>
          </label>
          <button
            disabled={!ui.canCreate || ui.engine.status !== "current"}
            onClick={() => void create("scope")}
          >
            Scopeを新規作成
          </button>
          <EnvelopeStatus value={scopes.data} error={scopes.error} />
          <ul className="context-candidates">
            {scopes.data?.data?.items.map(candidate)}
          </ul>
          <button
            disabled={!scopeBack.length}
            onClick={() => {
              setScopeAfter(scopeBack.at(-1)!);
              setScopeBack(scopeBack.slice(0, -1));
            }}
          >
            前のScope
          </button>
          <button
            disabled={!scopes.data?.meta.next_cursor}
            onClick={() => {
              setScopeBack([...scopeBack, scopeAfter]);
              setScopeAfter(scopes.data!.meta.next_cursor!);
            }}
          >
            次のScope
          </button>
        </>
      )}
      {tab === "graph" && (
        <>
          <p className="context-meta">
            開いたContextまたはScopeを起点に、範囲を指定して探索します。
          </p>
          <label>
            方向
            <select
              value={direction}
              onChange={(event) => setDirection(event.target.value)}
            >
              <option value="in">利用先・親（入力方向）</option>
              <option value="out">参照先・子（出力方向）</option>
              <option value="both">両方向</option>
            </select>
          </label>
          <label>
            関係
            <select
              value={relation}
              onChange={(event) => setRelation(event.target.value)}
            >
              {[
                ["contains", "文書の包含"],
                ["membership", "直接所属"],
                ["scope_parent", "Scopeの親子"],
                ["uses", "Runでの利用"],
                ["reference", "参照"],
                ["generated", "生成の由来"],
                ["version", "版"],
                ["", "すべての関係"],
              ].map(([value, label]) => (
                <option value={value} key={value}>
                  {label}
                </option>
              ))}
            </select>
          </label>
          <label>
            深さ
            <input
              type="number"
              min={1}
              value={depth}
              onChange={(event) => setDepth(Number(event.target.value))}
            />
          </label>
          <label>
            <input
              type="checkbox"
              checked={history}
              onChange={(event) => setHistory(event.target.checked)}
            />
            固定した旧版も辿る（文書が参照する版の確認）
          </label>
          <button disabled={!current} onClick={explore}>
            この範囲を探索
          </button>
          {graph && (
            <>
              <p className="context-meta">起点: {refLabel(graph.root)}</p>
              {graphSource && <p className="context-meta">{graphSource}</p>}
              <EnvelopeStatus value={related.data} error={related.error} />
              <ul className="context-candidates">
                {related.data?.data?.items.map((item, index) =>
                  item.node ? (
                    <li key={index}>
                      <button
                        className="link-button"
                        onClick={() => {
                          state.choose(item.node!);
                          if (item.node!.revision)
                            ui.store.setState({
                              historical: fixedRef(item.node!),
                            });
                        }}
                      >
                        {refLabel(item.node)}
                      </button>
                      {item.node.revision && kinds.includes(item.node.kind) && (
                        <div className="context-actions">
                          <button onClick={() => state.add(item.node!)}>
                            入力へ追加
                          </button>
                          <button onClick={() => state.add(item.node!, true)}>
                            入力から除外
                          </button>
                        </div>
                      )}
                    </li>
                  ) : (
                    <li key={index}>
                      {item.edge?.kind}: {item.edge && refLabel(item.edge.from)}{" "}
                      → {item.edge && refLabel(item.edge.to)}
                      <small>
                        由来の版: {item.edge && refLabel(item.edge.source)}
                      </small>
                    </li>
                  ),
                )}
              </ul>
              {!!related.data?.data?.unexplored.length && (
                <div className="context-warning">
                  未探索の範囲
                  <ul>
                    {related.data.data.unexplored.map((reason) => (
                      <li key={reason}>{reason}</li>
                    ))}
                  </ul>
                </div>
              )}
              <button
                disabled={!graphBack.length}
                onClick={() => {
                  setGraph(graphBack.at(-1));
                  setGraphBack(graphBack.slice(0, -1));
                }}
              >
                前の探索ページ
              </button>
              <button
                disabled={!related.data?.meta.next_cursor}
                onClick={() => {
                  setGraphBack([...graphBack, graph]);
                  setGraph({
                    ...graph,
                    cursor: related.data!.meta.next_cursor!,
                  });
                }}
              >
                次の探索ページ
              </button>
            </>
          )}
        </>
      )}
      {tab === "drafts" && (
        <>
          <p>端末内に保存した下書きです。再接続しても自動送信しません。</p>
          <ul>
            {draftKeys.map((key) => {
              const draft = ui.engine.state.drafts[key];
              return draft ? (
                <li key={key}>
                  <button onClick={() => state.choose(draft.base.record.ref)}>
                    {recordName(draft.base.record)} ·{" "}
                    {draft.latest
                      ? "競合"
                      : draft.pending
                        ? "保存結果未確認"
                        : "未送信"}
                  </button>
                </li>
              ) : null;
            })}
          </ul>
          <button disabled={!draftAfter} onClick={() => void draftPage()}>
            先頭の下書き
          </button>
          <button
            disabled={!draftNext}
            onClick={() => void draftPage(draftNext!)}
          >
            次の下書き
          </button>
        </>
      )}
      {scope && current && kinds.includes(current.ref.kind) && (
        <Grouping
          ui={ui}
          scope={scope}
          context={current.ref}
          changed={() => void client.invalidateQueries({ queryKey: ui.prefix })}
        />
      )}
    </section>
  );
}
function Grouping({
  ui,
  scope,
  context,
  changed,
}: {
  ui: UI;
  scope: Candidate;
  context: Ref;
  changed: () => void;
}) {
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState("");
  const edges = useQuery({
    queryKey: [...ui.prefix, "membership", scope.ref, context.kind, context.id],
    retry: false,
    queryFn: () =>
      ui.request<GraphPage>(
        `/graph/workspaces/${encodeURIComponent(ui.workspace)}/query`,
        {
          root: { kind: context.kind, id: context.id, revision: "" },
          direction: "out",
          kinds: ["membership"],
          depth: 1,
          limit: 100,
        },
      ),
  });
  const existing = edges.data?.data?.items.flatMap((item) =>
    item.edge &&
    item.edge.to.kind === "scope" &&
    item.edge.to.id === scope.ref.id
      ? [item.edge]
      : [],
  )[0];
  async function group(present: boolean) {
    setBusy(true);
    setNotice("");
    try {
      if (existing) {
        await ui.engine.open(existing.source, true);
        const key = identityKey(existing.source);
        const item = ui.engine.state.items[key];
        if (!item) throw new Error("所属の基準版を取得できません。");
        await ui.engine.edit(key, { ...item.record.value, present });
        await ui.engine.send(key);
        if (ui.engine.state.drafts[key]) {
          ui.store.getState().choose(existing.source);
          setNotice("所属の下書き・競合を編集画面で確認してください。");
          return;
        }
      } else {
        const key = await ui.engine.create("membership", {
          scope: { kind: scope.scope_kind, id: scope.ref.id },
          context: { kind: context.kind, id: context.id },
          present,
        });
        await ui.engine.send(key);
        if (ui.engine.state.drafts[key]) {
          ui.store
            .getState()
            .choose(ui.engine.state.drafts[key]!.base.record.ref);
          setNotice("所属は未確定です。下書きを確認してください。");
          return;
        }
      }
      changed();
      await edges.refetch();
      setNotice(
        present
          ? "直接所属を保存しました。子の所属・本文は変更していません。"
          : "直接所属の解除を保存しました。子の所属・本文は変更していません。",
      );
    } catch (error) {
      ui.fail(error);
      setNotice(String(error));
    } finally {
      setBusy(false);
    }
  }
  return (
    <section className="context-grouping">
      <h4>直接所属を変更</h4>
      <p>
        {scope.name} → {refLabel(context)}
      </p>
      <EnvelopeStatus value={edges.data} error={edges.error} />
      <button
        disabled={busy || edges.isFetching}
        onClick={() => void edges.refetch()}
      >
        所属を再確認
      </button>
      <button
        disabled={
          busy ||
          !ui.canCreate ||
          edges.data?.meta.completeness.state !== "complete"
        }
        onClick={() => void group(!existing)}
      >
        {existing
          ? "このScopeから直接所属を解除"
          : "このScopeにも直接所属させる"}
      </button>
      <p role="status">{notice}</p>
      <p className="context-meta">
        所属文書から参照される子は、文書を開き「包含・出力方向・旧版も辿る」で確認できます。直接所属には含めません。
      </p>
    </section>
  );
}
