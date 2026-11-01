import { useEffect, useState } from "react";
import { syncRequest } from "./auth";
import { contextStates } from "./context-generation-proposal";
import type { OperationAddress } from "./client-operation-port";
import type { VersionRef } from "./model-connections";
import type { Ref } from "./context-ui";
import { allowAnswerScope, answerScopeEpoch } from "./sync-store";
import { NavigationLink } from "./navigation";
import {
  sourceKey,
  sourceRoute,
  type GenerationSource,
} from "./context-generation-source";

type ResultPage = {
  items: {
    key: string;
    name: string;
    ref: Ref;
    state: string;
    reason: string;
    references: Ref[];
  }[];
  memberships: number;
  next: string;
  source: { run: VersionRef; output: VersionRef; path: string[] };
};
export function ContextGenerationResults({
  ui,
  result,
  onOpen,
  source,
}: {
  ui: OperationAddress;
  result: VersionRef;
  onOpen: (ref: Ref) => void;
  source?: GenerationSource;
}) {
  const [page, setPage] = useState<ResultPage>(),
    [notice, setNotice] = useState("");
  async function load(after = "") {
    const namespace = { api: ui.api, user: ui.user, workspace: ui.workspace },
      epoch = answerScopeEpoch(namespace);
    try {
      const response = (await syncRequest(
        `/client-operations/workspaces/${encodeURIComponent(ui.workspace)}/generation/results/${encodeURIComponent(result.id)}?version=${encodeURIComponent(result.version)}&after=${encodeURIComponent(after)}`,
        ui.native,
        { method: "GET" },
      )) as { data: ResultPage };
      if (allowAnswerScope(namespace, epoch)) {
        setPage(response.data);
        setNotice("");
      }
    } catch {
      setNotice("現在の権限で生成結果を取得できません。");
    }
  }
  useEffect(() => {
    void load();
  }, [result.id, result.version, ui.api, ui.user, ui.workspace]);
  return (
    <section aria-label="保存したContextと由来">
      <h3>保存したContextと由来</h3>
      {page && source && sourceKey(page.source) !== sourceKey(source) && (
        <p role="status">
          この結果は別の固定出力から作られています。
          <NavigationLink related to={sourceRoute(ui.workspace, page.source)}>
            この結果の元出力を開く
          </NavigationLink>
        </p>
      )}
      {page && (!source || sourceKey(page.source) === sourceKey(source)) && (
        <>
          <p>
            元Run: {page.source.run.id} · {page.source.run.version} · 出力:{" "}
            {page.source.output.id} · {page.source.output.version} · 箇所: /
            {page.source.path.join("/")}
          </p>
          <ul>
            {page.items.map((item) => (
              <li key={item.key}>
                <button onClick={() => onOpen(item.ref)}>
                  {item.name || item.key}
                </button>
                <p>
                  {contextStates[item.state] ?? item.state} · {item.reason}
                </p>
                <small>
                  {item.ref.kind} · {item.ref.revision}
                </small>
              </li>
            ))}
          </ul>
          <p>所属の変更: {page.memberships}件</p>
          {page.next && (
            <button onClick={() => void load(page.next)}>結果の次の50件</button>
          )}
        </>
      )}
      <p role="status">{notice}</p>
    </section>
  );
}
