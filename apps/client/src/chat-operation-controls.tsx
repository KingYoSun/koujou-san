import type { ClientOperation } from "./client-selection-host";

export function ChatOperationControls({
  operation,
  user,
  client,
  busy,
  modelMatches,
  onAction,
}: {
  operation: ClientOperation;
  user: string;
  client: string;
  busy: boolean;
  modelMatches: boolean;
  onAction: (action: "takeover" | "resume" | "continue" | "cancel") => void;
}) {
  if (
    operation.user !== user ||
    ["applied", "cancelled"].includes(operation.state)
  )
    return null;
  const owns = operation.client === client;
  return (
    <div className="context-actions">
      <button
        data-variant="secondary"
        disabled={busy}
        onClick={() => onAction("takeover")}
      >
        このクライアントへ引き継ぐ
      </button>
      {["failed", "incomplete"].includes(operation.state) && (
        <button
          disabled={busy || !owns || !modelMatches}
          onClick={() => onAction("resume")}
        >
          相談を再開
        </button>
      )}
      {operation.state === "ready" && (
        <button
          disabled={busy || !owns || !modelMatches}
          onClick={() => onAction("continue")}
        >
          相談を続ける
        </button>
      )}
      <button data-variant="secondary" onClick={() => onAction("cancel")}>
        この相談操作を終了
      </button>
    </div>
  );
}
