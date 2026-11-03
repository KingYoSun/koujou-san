import { useEffect, useRef, useState } from "react";
import { syncRequest } from "./auth";
import {
  kinds,
  labels,
  fixedRef,
  type Ref,
  type UI,
  type GraphEdge,
} from "./context-ui";

export function GraphRelationEditor({
  ui,
  selected,
  reference,
  editing,
  onSaved,
}: {
  ui: Pick<UI, "workspace" | "native" | "canCreate" | "fail">;
  selected?: Ref;
  reference?: Ref;
  editing?: GraphEdge;
  onSaved: () => void;
}) {
  const empty = (): Ref => ({ kind: "sentence", id: "", revision: "" });
  const [id, setID] = useState("");
  const [version, setVersion] = useState("0");
  const [from, setFrom] = useState<Ref>(empty),
    [to, setTo] = useState<Ref>(empty);
  const [kind, setKind] = useState("reference"),
    [order, setOrder] = useState(0);
  const [origin, setOrigin] = useState<GraphEdge["origin"]>();
  const [busy, setBusy] = useState(false),
    [notice, setNotice] = useState("");
  const pending = useRef(new Map<string, string>());
  useEffect(() => {
    if (editing?.source.kind !== "relation") return;
    setID(editing.source.id);
    setVersion(editing.source.revision);
    setFrom(fixedRef(editing.from));
    setTo(fixedRef(editing.to));
    setKind(editing.kind);
    setOrder(editing.order);
    setOrigin(editing.origin);
    setNotice("");
  }, [editing]);
  function create() {
    setID(crypto.randomUUID());
    setVersion("0");
    setFrom(selected ? fixedRef(selected) : empty());
    setTo(reference ? fixedRef(reference) : empty());
    setKind("reference");
    setOrder(0);
    setOrigin(undefined);
    setNotice("");
  }
  async function save() {
    setBusy(true);
    setNotice("");
    try {
      const value = {
          kind,
          from,
          to,
          order,
          present: true,
          ...(origin ? { origin } : {}),
        },
        key = JSON.stringify([id, version, value]);
      if (!pending.current.has(key))
        pending.current.set(key, crypto.randomUUID());
      const result = (await syncRequest(
        `/graph/workspaces/${encodeURIComponent(ui.workspace)}/relations/${encodeURIComponent(id)}`,
        ui.native,
        {
          method: "PUT",
          body: {
            operation_id: pending.current.get(key),
            expected_version: version,
            value,
          },
        },
      )) as { data: { version: string } };
      setVersion(result.data.version);
      setNotice("関係を保存しました。");
      pending.current.delete(key);
      onSaved();
    } catch (error) {
      ui.fail(error);
    } finally {
      setBusy(false);
    }
  }
  const valid = (ref: Ref) =>
    !!ref.id && !!ref.revision && ref.revision !== "head";
  return (
    <section aria-label="明示関係の編集">
      <h4>明示関係</h4>
      <button disabled={!ui.canCreate || busy} onClick={create}>
        関係を新規作成
      </button>
      {id && (
        <fieldset disabled={busy || !ui.canCreate}>
          <legend>{version === "0" ? "新しい関係" : "関係の編集"}</legend>
          <label>
            関係
            <select
              value={kind}
              onChange={(event) => setKind(event.target.value)}
            >
              {[
                ["reference", "参照"],
                ["uses", "Runでの利用"],
                ["generated", "生成の由来"],
                ["semantic", "意味の関係"],
              ].map(([key, label]) => (
                <option key={key} value={key}>
                  {label}
                </option>
              ))}
            </select>
          </label>
          {(
            [
              ["元", from, setFrom, selected],
              ["先", to, setTo, reference],
            ] as const
          ).map(([title, ref, setRef, candidate]) => (
            <fieldset key={title}>
              <legend>{title}</legend>
              {candidate && (
                <button onClick={() => setRef(fixedRef(candidate))}>
                  {title === "元" ? "選択中の対象を使用" : "参照候補を使用"}
                </button>
              )}
              <label>
                種類
                <select
                  value={ref.kind}
                  onChange={(event) =>
                    setRef({ ...ref, kind: event.target.value })
                  }
                >
                  {[
                    ...kinds,
                    "scope",
                    "run",
                    "hopper-result",
                    "publication",
                    "workflow-execution",
                  ].map((key) => (
                    <option key={key} value={key}>
                      {labels[key] ?? key}
                    </option>
                  ))}
                </select>
              </label>
              <label>
                ID
                <input
                  value={ref.id}
                  onChange={(event) =>
                    setRef({ ...ref, id: event.target.value })
                  }
                />
              </label>
              <label>
                固定版
                <input
                  value={ref.revision}
                  onChange={(event) =>
                    setRef({ ...ref, revision: event.target.value })
                  }
                />
              </label>
            </fieldset>
          ))}
          <label>
            順序
            <input
              type="number"
              min={0}
              value={order}
              onChange={(event) => setOrder(Number(event.target.value))}
            />
          </label>
          <button
            disabled={
              !valid(from) ||
              !valid(to) ||
              order < 0 ||
              (["uses", "generated"].includes(kind) && from.kind !== "run")
            }
            onClick={() => void save()}
          >
            関係を保存
          </button>
        </fieldset>
      )}
      {notice && <p role="status">{notice}</p>}
    </section>
  );
}
