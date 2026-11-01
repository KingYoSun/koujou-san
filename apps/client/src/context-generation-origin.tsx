import { useEffect, useState } from "react";
import { syncRequest } from "./auth";
import { allowAnswerScope, answerScopeEpoch } from "./sync-store";
import { contextStates } from "./context-generation-proposal";
import type { OperationAddress } from "./client-operation-port";
import type { VersionRef } from "./model-connections";
import type { Ref } from "./context-ui";
import { NavigationLink } from "./navigation";
import { PageList } from "./page-list";

type Origin = {
  operation: VersionRef;
  source: { run: VersionRef; output: VersionRef; path: string[] };
  item: { state: string; reason: string; ref: Ref; references: Ref[] };
  edited: boolean;
  manual: boolean;
};
export function ContextGenerationOrigin({
  ui,
  refValue,
  operation,
}: {
  ui: OperationAddress;
  refValue: Ref;
  operation: string;
}) {
  const [origin, setOrigin] = useState<Origin>(),
    [notice, setNotice] = useState(""),
    [excerpt, setExcerpt] = useState("");
  const namespace = { api: ui.api, user: ui.user, workspace: ui.workspace };
  const base = `/client-operations/workspaces/${encodeURIComponent(ui.workspace)}`;
  useEffect(() => {
    let active = true;
    const epoch = answerScopeEpoch(namespace);
    setOrigin(undefined);
    setExcerpt("");
    void syncRequest(
      `${base}/origins/${encodeURIComponent(refValue.id)}?version=${encodeURIComponent(refValue.revision)}&operation=${encodeURIComponent(operation)}`,
      ui.native,
      { method: "GET" },
    )
      .then((v) => {
        if (active && allowAnswerScope(namespace, epoch)) {
          setOrigin(v as Origin | undefined);
          setNotice("");
        }
      })
      .catch(() => {
        if (active) setNotice("由来の詳細は現在の権限で取得できません。");
      });
    return () => {
      active = false;
    };
  }, [
    ui.api,
    ui.user,
    ui.workspace,
    refValue.id,
    refValue.revision,
    operation,
  ]);
  async function preview() {
    if (!origin) return;
    const epoch = answerScopeEpoch(namespace);
    try {
      const v = (await syncRequest(base + "/generation/preview", ui.native, {
        method: "POST",
        body: origin.source,
      })) as { output: { selected_utf8: string } };
      if (allowAnswerScope(namespace, epoch))
        setExcerpt(v.output.selected_utf8);
    } catch {
      setNotice("元出力の現在の取得権限を確認してください。");
    }
  }
  return (
    <section aria-label="生成Contextの状態と由来">
      <h4>記述の状態と由来</h4>
      {origin && (
        <>
          <p>
            {contextStates[origin.item.state] ?? origin.item.state} ·{" "}
            {origin.manual
              ? "依頼者が明示的に編集・採用"
              : "生成方針に従って採用"}
          </p>
          {origin.edited && (
            <p>
              この版は生成後に手動で変更されています。生成時の状態と現在の記述を区別してください。
            </p>
          )}
          <p>{origin.item.reason}</p>
          <p>
            元Run: {origin.source.run.id} · {origin.source.run.version} ·
            出力版: {origin.source.output.version} · 箇所: /
            {origin.source.path.join("/")}
          </p>
          <p>生成操作: {origin.operation.id}</p>
          {origin.item.references.length ? (
            <PageList label="参照したContext" items={origin.item.references}>
              {(ref, index) => (
                <li key={index}>
                  {ref.kind}:{ref.id}@{ref.revision}
                </li>
              )}
            </PageList>
          ) : (
            <p>参照したContext：元出力のみ</p>
          )}
          <NavigationLink
            related
            to={{
              workspace: ui.workspace,
              screen: "work",
              target: { ...origin.source.run, kind: "run" },
              view: "evidence",
            }}
          >
            元Runの成果・検品へ戻る
          </NavigationLink>
          <button onClick={() => void preview()}>元出力の固定箇所を確認</button>
          {excerpt && <pre>{excerpt}</pre>}
        </>
      )}
      <p role="status">{notice}</p>
    </section>
  );
}
