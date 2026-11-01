import { ScreenContent, useScreenActive } from "./navigation";
import { useEffect, useRef, useState } from "react";
import { syncRequest } from "./auth";
import { allowAnswerScope, answerScopeEpoch } from "./sync-store";
import { SyncHTTPError } from "./sync-error";
import { Disclosure } from "./disclosure";
import { SidebarPane } from "./panes";
import { StatusBadge } from "./status-badge";
import { ComparisonConditions } from "./usage-comparison-conditions";
import { ComparisonResults } from "./usage-comparison-results";
import { UsageReevaluation } from "./usage-reevaluation";
import {
  newCondition,
  storedCondition,
  type CostComparison,
  type CostCondition,
  type CostPage,
  type CostRef,
  type CostRow,
} from "./usage-comparison-model";
type View = { condition: CostCondition; mode: string; page: CostPage };
type Saved = CostRef & { kind: string; value: CostComparison };
export function UsageComparison({
  native,
  workspace,
  role,
  api,
  user,
  onInspect,
}: {
  native: boolean;
  workspace: string;
  role: string;
  api: string;
  user: string;
  onInspect: (r: CostRef & { kind: string }) => Promise<void>;
}) {
  const visible = useScreenActive();
  const [comparison, setComparison] = useState<CostComparison>(() => ({
    name: "費用・品質・時間の比較",
    left: newCondition(),
    right: newCondition(),
  }));
  const [saved, setSaved] = useState<CostRef>(),
    [list, setList] = useState<Saved[]>(),
    [listCursor, setListCursor] = useState(""),
    [views, setViews] = useState<Partial<Record<"left" | "right", View>>>({});
  const [basisNames, setBasisNames] = useState<Record<string, string>>({});
  const namedRefs = [comparison.left, comparison.right].flatMap((condition) =>
    condition.basis.length === 1 &&
    /^(run-definition|workflow-definition):/.test(condition.basis[0].id)
      ? condition.basis
      : [],
  );
  const nameKey = JSON.stringify(namedRefs);
  useEffect(() => {
    if (!visible) return;
    let current = true;
    setBasisNames({});
    if (namedRefs.length)
      void syncRequest(
        `/workflows/workspaces/${encodeURIComponent(workspace)}/labels`,
        native,
        { method: "POST", body: namedRefs },
      )
        .then((value) => {
          if (current)
            setBasisNames(
              Object.fromEntries(
                (value as (CostRef & { name?: string })[]).map((label) => [
                  label.id + "@" + label.version,
                  label.name || "名称未設定",
                ]),
              ),
            );
        })
        .catch(() => {
          /* Fixed references remain available in the conditions. */
        });
    return () => {
      current = false;
    };
  }, [visible, nameKey, native, api, user, workspace]);
  const [selected, setSelected] = useState<CostRow>(),
    [notice, setNotice] = useState(""),
    [busy, setBusy] = useState(false);
  const pending = useRef<{ key: string; operation: string } | undefined>(
      undefined,
    ),
    active = useRef(true);
  useEffect(() => {
    active.current = true;
    return () => {
      active.current = false;
    };
  }, []);
  const namespace = { api, user, workspace },
    base = `/usage/workspaces/${encodeURIComponent(workspace)}`;
  const call = async <T,>(path: string, body?: unknown): Promise<T> => {
    const epoch = answerScopeEpoch(namespace);
    const result = await syncRequest(path, native, {
      method: body === undefined ? "GET" : "POST",
      body,
    });
    if (!active.current || !allowAnswerScope(namespace, epoch))
      throw new SyncHTTPError(403, undefined);
    return result as T;
  };
  async function work(action: () => Promise<void>, failed: string) {
    setBusy(true);
    setNotice("");
    try {
      await action();
    } catch (error) {
      if (active.current) {
        if (
          error instanceof SyncHTTPError &&
          [401, 403].includes(error.status)
        ) {
          setViews({});
          setSelected(undefined);
          setList(undefined);
          setSaved(undefined);
          setNotice("閲覧権限がありません。");
        } else setNotice(`${failed}できませんでした。`);
      }
    } finally {
      if (active.current) setBusy(false);
    }
  }
  const condition = (side: "left" | "right", value: CostCondition) => {
    setComparison((c) => ({ ...c, [side]: value }));
    setViews((v) => ({ ...v, [side]: undefined }));
  };
  async function query(side: "left" | "right", mode = "compare", next = false) {
    const old = views[side],
      request = next && old ? old.condition : comparison[side];
    const cursor = next ? (old?.page.meta.next_cursor ?? "") : "";
    const page = await call<CostPage>(base + "/query", {
      ...request,
      mode,
      cursor,
      limit: 50,
    });
    setViews((v) => ({ ...v, [side]: { condition: request, mode, page } }));
  }
  async function save() {
    const key = JSON.stringify({ saved, comparison });
    if (pending.current?.key !== key)
      pending.current = { key, operation: crypto.randomUUID() };
    const id = saved?.id ?? "comparison:" + pending.current.operation;
    const result = await call<{ data: CostRef }>(
      base + "/records/" + encodeURIComponent(id),
      {
        operation_id: pending.current.operation,
        expected_version: saved?.version ?? "0",
        value: comparison,
      },
    );
    setSaved({ id: result.data.id, version: result.data.version });
    setNotice(
      `比較条件 ${result.data.id} @ ${result.data.version} を保存しました。`,
    );
  }
  async function listSaved(next = false) {
    const page = await call<{
      data: { items: Saved[] };
      meta: { next_cursor: string | null };
    }>(base + "/query", {
      mode: "records",
      kind: "comparison",
      limit: 50,
      cursor: next ? listCursor : "",
    });
    setList(page.data.items);
    setListCursor(page.meta.next_cursor ?? "");
  }
  async function load(ref: CostRef) {
    const r = await call<Saved>(
      base +
        "/records/" +
        encodeURIComponent(ref.id) +
        "?version=" +
        encodeURIComponent(ref.version),
    );
    setComparison({
      ...r.value,
      left: storedCondition(r.value.left),
      right: storedCondition(r.value.right),
    });
    setSaved({ id: r.id, version: r.version });
    setViews({});
    setSelected(undefined);
  }
  const equalKinds = comparison.left.kind === comparison.right.kind,
    equalPeriods =
      comparison.left.from === comparison.right.from &&
      comparison.left.until === comparison.right.until;
  return (
    <ScreenContent screens={["usage"]}>
      {
        <section className="team-details" aria-label="費用・品質・時間の比較">
          <h3>費用・品質・時間を比較</h3>
          <SidebarPane>
            <section aria-label="保存した比較条件">
              <button
                disabled={busy}
                aria-busy={busy}
                data-variant="secondary"
                onClick={() => void work(() => listSaved(), "比較条件を取得")}
              >
                {busy ? "比較条件を取得中…" : "比較条件を取得"}
              </button>
              {list && (
                <p>
                  {list.length
                    ? `${list.length}件を表示${listCursor ? "（続きあり）" : ""}`
                    : "保存した比較条件はありません。"}
                </p>
              )}
              <ul aria-busy={busy}>
                {list?.map((r) => (
                  <li key={r.id + "@" + r.version}>
                    <button
                      data-variant="secondary"
                      disabled={busy}
                      aria-current={
                        saved?.id === r.id && saved.version === r.version
                          ? "true"
                          : undefined
                      }
                      onClick={() => void work(() => load(r), "比較条件を取得")}
                    >
                      {r.value.name}
                    </button>
                  </li>
                ))}
              </ul>
              {listCursor && (
                <button
                  disabled={busy}
                  data-variant="secondary"
                  onClick={() =>
                    void work(() => listSaved(true), "比較条件を取得")
                  }
                >
                  比較条件の次のページ
                </button>
              )}
            </section>
          </SidebarPane>
          <label>
            比較名
            <input
              value={comparison.name}
              onChange={(e) =>
                setComparison({ ...comparison, name: e.target.value })
              }
            />
          </label>
          <div className="cost-comparison-grid">
            {(["left", "right"] as const).map((side, i) => (
              <section aria-label={"比較" + (i === 0 ? "A" : "B")} key={side}>
                <h4>比較{i === 0 ? "A" : "B"}</h4>
                <p>
                  {comparison[side].basis.length === 1 &&
                  /^(run-definition|workflow-definition):/.test(
                    comparison[side].basis[0].id,
                  )
                    ? (basisNames[
                        comparison[side].basis[0].id +
                          "@" +
                          comparison[side].basis[0].version
                      ] ?? "定義名は未取得")
                    : `固定条件 ${comparison[side].basis.length}件`}
                </p>
                <p>
                  {comparison[side].model || "モデル指定なし"} /{" "}
                  {comparison[side].from} ～ {comparison[side].until}
                </p>
                <Disclosure title={"比較" + (i === 0 ? "A" : "B") + "の条件"}>
                  {() => (
                    <ComparisonConditions
                      name={"比較" + (i === 0 ? "A" : "B")}
                      value={comparison[side]}
                      onChange={(v) => condition(side, v)}
                      call={call}
                      workspace={workspace}
                      disabled={busy}
                    />
                  )}
                </Disclosure>
                <button
                  disabled={busy}
                  aria-busy={busy}
                  onClick={() => void work(() => query(side), "集計を取得")}
                >
                  比較{i === 0 ? "A" : "B"}の集計を取得
                </button>
                <button
                  disabled={busy}
                  aria-busy={busy}
                  onClick={() =>
                    void work(
                      () => query(side, "compare_quality"),
                      "検品条件を取得",
                    )
                  }
                >
                  比較{i === 0 ? "A" : "B"}の検品条件を取得
                </button>
                {views[side] && (
                  <ComparisonResults
                    page={views[side]!.page}
                    condition={views[side]!.condition}
                    mode={views[side]!.mode}
                    busy={busy}
                    editable={role !== "viewer"}
                    onNext={() =>
                      void work(
                        () => query(side, views[side]!.mode, true),
                        "比較結果を取得",
                      )
                    }
                    onDetails={() =>
                      void work(
                        () => query(side, "compare_details"),
                        "元計測・料金を取得",
                      )
                    }
                    onRead={(ref) =>
                      void work(
                        () => onInspect({ ...ref, kind: ref.id.split(":")[0] }),
                        "記録を取得",
                      )
                    }
                    onReevaluate={setSelected}
                  />
                )}
              </section>
            ))}
          </div>
          <button
            disabled={busy}
            aria-busy={busy}
            onClick={() =>
              void work(async () => {
                await Promise.all([query("left"), query("right")]);
              }, "集計を取得")
            }
          >
            二つの集計を取得
          </button>
          {!equalKinds && (
            <StatusBadge label="評価種別" value="A・Bで異なる" tone="warning" />
          )}
          {!equalPeriods && (
            <StatusBadge label="期間" value="A・Bで異なる" tone="warning" />
          )}
          {views.left &&
            views.right &&
            views.left.page.data.through !== views.right.page.data.through && (
              <StatusBadge
                label="反映位置"
                value="A・Bで異なる"
                tone="warning"
              />
            )}
          {role !== "viewer" && (
            <button
              disabled={busy || !comparison.name.trim()}
              onClick={() => void work(save, "比較条件を保存")}
            >
              比較条件を保存
            </button>
          )}
          <button
            disabled={busy}
            onClick={() => {
              setSaved(undefined);
              setViews({});
              setSelected(undefined);
              setComparison({
                name: "新しい比較",
                left: newCondition(),
                right: newCondition(),
              });
            }}
          >
            新しい比較条件
          </button>
          {saved && (
            <p>
              保存した条件版：{saved.id} @ {saved.version}
            </p>
          )}
          <a
            download="koujou-comparison-conditions.json"
            href={
              "data:application/json;charset=utf-8," +
              encodeURIComponent(JSON.stringify({ saved, comparison }, null, 2))
            }
          >
            二群の条件を書き出す
          </a>
          {selected && role !== "viewer" && (
            <UsageReevaluation
              row={selected}
              call={(path, body) => call(base + path, body)}
              onSaved={(ref) =>
                void work(
                  () => onInspect({ ...ref, kind: "evaluation" }),
                  "記録を取得",
                )
              }
            />
          )}
          <p role="status">{busy ? "集計・照合しています…" : notice}</p>
        </section>
      }
    </ScreenContent>
  );
}
