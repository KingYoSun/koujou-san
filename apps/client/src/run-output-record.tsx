import type { OutputContextSource } from "./context-generation";
import type { VersionRef } from "./model-connections";
export type OutputRecord = VersionRef & {
  run_id: string;
  kind: string;
  value: unknown;
};
export function handoffOutput<T extends OutputRecord>(
  current: VersionRef,
  record: T,
  previous: VersionRef | null,
  handoff: boolean,
  outputs: T[],
) {
  const same = handoff && previous?.id === current.id;
  return {
    previous: { id: current.id, version: current.version },
    outputs: [
      ...(same
        ? outputs.filter(
            (r) => r.id !== record.id || r.version !== record.version,
          )
        : []),
      record,
    ],
  };
}
export function RunOutputRecord({
  record,
  current,
  workspace,
  editable,
  busy,
  onHandoff,
  onGenerate,
}: {
  record: OutputRecord;
  current: VersionRef;
  workspace: string;
  editable: boolean;
  busy: boolean;
  onHandoff: () => void;
  onGenerate?: (source: OutputContextSource) => void;
}) {
  return (
    <details>
      <summary>
        {record.kind} · {record.id} · {record.version}
      </summary>
      <pre>{JSON.stringify(record.value, null, 2)}</pre>
      {editable && record.kind === "output" && (
        <>
          <button disabled={busy} onClick={onHandoff}>
            この固定出力を新Runへ引き継ぐ
          </button>
          {onGenerate && (
            <button
              disabled={busy}
              onClick={() =>
                onGenerate({
                  workspace,
                  run: { id: current.id, version: current.version },
                  output: { id: record.id, version: record.version },
                  channel: (record.value as { channel: string }).channel,
                })
              }
            >
              この固定出力からContextを作成・更新
            </button>
          )}
        </>
      )}
    </details>
  );
}
