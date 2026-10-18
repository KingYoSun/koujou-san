import { useState } from "react";
import { syncRequest } from "./auth";
import { allowAnswerScope, answerScopeEpoch } from "./sync-store";
import { fieldSource } from "./workflow-json";
import type { VersionRef } from "./model-connections";

export function DefinitionRun({
  native,
  api,
  user,
  workspace,
  definition,
  canEdit,
}: {
  native: boolean;
  api: string;
  user: string;
  workspace: string;
  definition: VersionRef & { value_utf8: string };
  canEdit: boolean;
}) {
  const [inputs, setInputs] = useState("{}");
  const [pending, setPending] = useState<{
    id: string;
    operation_id: string;
    adoption: VersionRef;
    inputs_utf8: string;
  }>();
  const [created, setCreated] = useState<VersionRef>();
  const [busy, setBusy] = useState(false),
    [notice, setNotice] = useState("");
  const namespace = { api, user, workspace };
  const base = `/workflows/workspaces/${encodeURIComponent(workspace)}`;
  async function create() {
    setBusy(true);
    setNotice("");
    const epoch = answerScopeEpoch(namespace);
    try {
      const read = (await syncRequest(
        `${base}/records/${encodeURIComponent("adoption:" + definition.id)}?version=head`,
        native,
        { method: "GET" },
      )) as VersionRef & { value: { definition: VersionRef } };
      if (!allowAnswerScope(namespace, epoch)) return;
      if (
        read.value.definition.id !== definition.id ||
        read.value.definition.version !== definition.version
      ) {
        setNotice("表示中の定義版を採用してからRunを作成してください。");
        return;
      }
      const request = pending ?? {
        id: `run:${crypto.randomUUID()}`,
        operation_id: crypto.randomUUID(),
        adoption: { id: read.id, version: read.version },
        inputs_utf8: inputs,
      };
      setPending(request);
      const result = (await syncRequest(
        `${base}/runs/${encodeURIComponent(request.id)}`,
        native,
        {
          method: "POST",
          body: {
            operation_id: request.operation_id,
            expected_version: "0",
            adoption: request.adoption,
            inputs_utf8: request.inputs_utf8,
          },
        },
      )) as { data: VersionRef };
      if (allowAnswerScope(namespace, epoch)) {
        setCreated(result.data);
        setPending(undefined);
        setNotice(
          "未割当のRunを作成しました。「作業と判断」のRunで確認し、通常の割当・開始を行えます。",
        );
      }
    } catch {
      if (allowAnswerScope(namespace, epoch))
        setNotice(
          "作成を確認できません。採用版・入力・権限・接続を確認してください。同じ要求で再確認できます。",
        );
    } finally {
      setBusy(false);
    }
  }
  const guarded =
    fieldSource(definition.value_utf8, ["ci"]) ||
    fieldSource(definition.value_utf8, ["write"]);
  return (
    <section aria-label="採用した定義からRunを作成">
      <h3>採用した定義からRunを作成</h3>
      {guarded ? (
        <p>CI観測・条件付き書込みの定義は工程に組み込んで実行します。</p>
      ) : (
        <>
          <p>
            宣言された入力:{" "}
            {fieldSource(definition.value_utf8, ["inputs"]) || "なし"}
            。リポジトリ・commitはRun割当時に指定します。
          </p>
          <label>
            今回の入力（名前をキーにしたJSON）
            <textarea
              value={pending?.inputs_utf8 ?? inputs}
              disabled={busy || !!pending || !!created}
              onChange={(e) => setInputs(e.target.value)}
            />
          </label>
          <button
            disabled={!canEdit || busy || !!created}
            onClick={() => void create()}
          >
            {pending
              ? "同じRun作成要求を確認"
              : "未割当のRunを作成（開始しない）"}
          </button>
        </>
      )}
      {created && (
        <p>
          作成したRun: <code>{created.id}</code>
        </p>
      )}
      <p role="status">{notice}</p>
    </section>
  );
}
