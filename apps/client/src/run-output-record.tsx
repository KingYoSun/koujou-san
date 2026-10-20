import type { OutputContextSource } from "./context-generation";
import type { VersionRef } from "./model-connections";
import { NavigationLink } from "./navigation";
import { Disclosure } from "./disclosure";
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
    <section aria-label={record.kind === "output" ? "固定出力" : "Runの原記録"}>
      <h4>
        {record.kind === "output"
          ? `原出力：${(record.value as { channel?: string }).channel ?? "チャンネル未指定"}`
          : record.kind}
      </h4>
      <p className="target-reference">
        {record.id} · {record.version}
      </p>
      <Disclosure title="内容・原JSONを確認">
        {() => <pre>{JSON.stringify(record.value, null, 2)}</pre>}
      </Disclosure>
      {editable && record.kind === "output" && (
        <div className="team-controls">
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
          <NavigationLink
            related
            to={{
              workspace,
              screen: "publications",
              target: {
                kind: "output",
                id: record.id,
                version: record.version,
              },
              origin: { kind: "run", id: current.id, version: current.version },
              view: "source",
            }}
          >
            この固定出力から外部起票を準備
          </NavigationLink>
        </div>
      )}
    </section>
  );
}
