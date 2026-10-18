import { useEffect, useRef, useState } from "react";
import { syncRequest } from "./auth";
import { allowAnswerScope, answerScopeEpoch } from "./sync-store";
import {
  operationClientID,
  operationPort,
  type OperationAddress,
} from "./client-operation-port";
import { loadProfiles, modelReady, type VersionRef } from "./model-connections";
import {
  runSelection,
  type ClientOperation,
  type Saved,
  type SelectionPolicy,
} from "./client-selection-host";

export function useContextGenerationWorker(
  ui: OperationAddress & { canCreate: boolean },
  onApplied: (ref: VersionRef) => void,
) {
  const [notice, setNotice] = useState("");
  const current = useRef({ ui, onApplied });
  current.current = { ui, onApplied };
  useEffect(() => {
    if (!ui.canCreate) return;
    let alive = true,
      busy = false;
    const namespace = { api: ui.api, user: ui.user, workspace: ui.workspace };
    const epoch = answerScopeEpoch(namespace);
    const active = () => alive && allowAnswerScope(namespace, epoch);
    const tick = async () => {
      if (busy || !active()) return;
      busy = true;
      try {
        const ref = (await syncRequest(
          `/client-operations/workspaces/${encodeURIComponent(ui.workspace)}/generation/next`,
          ui.native,
          { method: "POST", body: { client: operationClientID } },
        )) as VersionRef | null;
        if (!ref || !active()) return;
        const base = `/client-operations/workspaces/${encodeURIComponent(ui.workspace)}`;
        const op = (await syncRequest(
          `${base}/records/${encodeURIComponent(ref.id)}?version=head`,
          ui.native,
          { method: "GET" },
        )) as Saved<ClientOperation>;
        if (!active() || op.value.state !== "ready") {
          if (active())
            setNotice(
              "自動生成の質問・提案・未確認要求があります。操作履歴で確認してください。",
            );
          return;
        }
        const profiles = await loadProfiles(ui.native, ui.api + "\0" + ui.user);
        const profile = profiles.find(
          (p) =>
            p.id === op.value.model.connection &&
            p.revision === op.value.model.revision,
        );
        if (
          !profile ||
          !modelReady(ui.native, ui.api + "\0" + ui.user, profile)
        ) {
          if (active())
            setNotice(
              "自動生成は接続設定・キーの入力待ちです。新しいモデル要求は送っていません。",
            );
          return;
        }
        const port = operationPort(ui, profile, ref, active);
        const policy = await port.read<SelectionPolicy>(op.value.policy);
        const result = await runSelection(
          op,
          policy.value,
          profile,
          operationClientID,
          port,
          () => {},
        );
        if (!active()) return;
        if (result.value.applied) {
          current.current.onApplied(result.value.applied);
          setNotice(
            "有効化した方針でContextを保存しました。原出力とRunの入力は保持しています。",
          );
        } else
          setNotice(
            "自動生成の質問・提案・未確認要求があります。操作履歴で確認してください。",
          );
      } catch {
        if (active())
          setNotice(
            "自動生成の接続・権限・操作状態を確認してください。結果未確認の要求は再送しません。",
          );
      } finally {
        busy = false;
      }
    };
    const timer = setInterval(() => void tick(), 2500);
    void tick();
    return () => {
      alive = false;
      clearInterval(timer);
    };
  }, [ui.native, ui.api, ui.user, ui.workspace, ui.canCreate]);
  return notice;
}
