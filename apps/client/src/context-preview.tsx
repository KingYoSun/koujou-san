import { PageList } from "./page-list";
import { Disclosure } from "./disclosure";
import { useEffect, useRef, useState } from "react";
import { useNavigation, useScreenActive } from "./navigation";
import { useStore } from "zustand";
import type { CandidateEvidence, ContextResolution } from "./context-contract";
import type { UIState } from "./context-ui";
type PreviewRequest = {
  run_id: string;
  selection: Pick<UIState, "mode" | "include" | "exclude">;
  evidence: CandidateEvidence[];
};
import type { UI, Ref, Envelope, CandidatePage } from "./context-ui";
import { refKey, refLabel, resultData } from "./context-ui";
import { Completeness, reasonText } from "./context-status";
export type DefinitionContextInput = {
  definition: { id: string; version: string };
  selection: Pick<UIState, "mode" | "include" | "exclude">;
};
export function ContextPreview({
  ui,
  active = true,
  definitionInput,
  onDefinitionInput,
}: {
  ui: UI;
  active?: boolean;
  definitionInput?: DefinitionContextInput;
  onDefinitionInput?: (value: DefinitionContextInput) => void;
}) {
  const visible = useScreenActive() && active;
  const navigation = useNavigation();
  const preparation =
    navigation?.route.origin?.kind === "run-preparation"
      ? navigation.route.origin
      : undefined;
  const definition =
    navigation?.route.origin?.kind === "run-definition"
      ? navigation.route.origin
      : undefined;
  const initialized = useRef<DefinitionContextInput | undefined>(undefined);
  useEffect(() => {
    if (
      visible &&
      definition &&
      definitionInput &&
      initialized.current !== definitionInput &&
      definition.id === definitionInput.definition.id &&
      definition.version === definitionInput.definition.version
    ) {
      ui.store.setState(definitionInput.selection);
      setResult(undefined);
      setError("");
      initialized.current = definitionInput;
    }
  }, [visible, definitionInput, definition?.id, definition?.version]);
  const [resolvedRequest, setResolvedRequest] = useState<PreviewRequest>();
  const pending = useRef<
    | {
        signature: string;
        id: string;
        operation_id: string;
      }
    | undefined
  >(undefined);
  const state = useStore(ui.store);
  const [result, setResult] = useState<Envelope<ContextResolution>>();
  const [basis, setBasis] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  useEffect(() => {
    setResult(undefined);
    setError("");
  }, [state.permissionEpoch]);
  const signature = JSON.stringify([
    state.permissionEpoch,
    state.mode,
    state.include,
    state.exclude,
  ]);
  function update(field: "include" | "exclude", values: Ref[]) {
    ui.store.setState({ [field]: values });
  }
  async function preview() {
    setBusy(true);
    setError("");
    setResult(undefined);
    setBasis(signature);
    try {
      if (state.mode === "select" && !state.include.length) {
        setResult({
          meta: {
            position: null,
            completeness: { state: "pending", reason: "empty_selection" },
          },
          data: { ready: false, missing: [{ code: "empty_selection" }] },
        });
        return;
      }
      const refs = [
        ...new Map(
          [...state.include, ...state.exclude].map((ref) => [refKey(ref), ref]),
        ).values(),
      ];
      const evidence: CandidateEvidence[] = [];
      // The user explicitly confirms the fixed references, not every candidate in
      // the original broad search. Every bounded batch is rechecked at the source.
      if (state.mode !== "none")
        for (let i = 0; i < refs.length; i += 100) {
          const chunk = refs.slice(i, i + 100);
          const found = await ui.request<CandidatePage>(
            `/search/workspaces/${encodeURIComponent(ui.workspace)}/candidates`,
            {
              refs: chunk,
              limit: 100,
            },
          );
          if (
            found.error ||
            !found.data ||
            found.meta.completeness.state !== "complete"
          ) {
            setResult({
              meta: found.meta,
              data: {
                ready: false,
                missing: [
                  {
                    code: "incomplete_candidates",
                    source: found.meta.completeness.reason ?? found.error?.code,
                  },
                ],
              },
            });
            return;
          }
          const available = new Set(
            found.data.items.map((candidate) => refKey(candidate.ref)),
          );
          if (chunk.some((ref) => !available.has(refKey(ref)))) {
            setResult({
              meta: found.meta,
              data: {
                ready: false,
                missing: chunk
                  .filter((ref) => !available.has(refKey(ref)))
                  .map((ref) => ({ code: "missing_reference", ref })),
              },
            });
            return;
          }
          evidence.push(found.data.evidence);
        }
      const request: PreviewRequest = {
        run_id: "",
        selection: {
          mode: state.mode,
          include: state.include,
          exclude: state.exclude,
        },
        evidence,
      };
      const value = await ui.request<ContextResolution>(
        `/contexts/workspaces/${encodeURIComponent(ui.workspace)}/resolve`,
        request,
      );
      if (
        value.data?.manifest?.state === "ready" &&
        !value.data.manifest.placements.length
      )
        value.data = { ready: false, missing: [{ code: "empty_selection" }] };
      if (ui.store.getState().permissionEpoch !== state.permissionEpoch) return;
      setResult(value);
      setResolvedRequest(request);
    } catch (error) {
      setError(String(error));
      ui.fail(error);
    } finally {
      setBusy(false);
    }
  }
  const current = basis === signature;
  const manifest = current ? result?.data?.manifest : undefined;
  async function useInput() {
    if (
      !preparation ||
      !resolvedRequest ||
      !manifest ||
      !result?.data?.ready ||
      !navigation?.returnTo
    )
      return;
    setBusy(true);
    setError("");
    const value = { ...resolvedRequest, run_id: preparation.id };
    const signature = JSON.stringify(value);
    if (pending.current?.signature !== signature)
      pending.current = {
        signature,
        id: crypto.randomUUID(),
        operation_id: crypto.randomUUID(),
      };
    try {
      const fixed = resultData(
        await ui.request<{ id: string; version: string }>(
          `/contexts/workspaces/${encodeURIComponent(ui.workspace)}/manifest/${pending.current.id}`,
          {
            operation_id: pending.current.operation_id,
            expected_version: "0",
            value,
          },
        ),
      );
      navigation.navigate({
        ...navigation.returnTo,
        origin: preparation,
        input: {
          kind: "manifest",
          id: pending.current.id,
          version: fixed.version,
        },
      });
    } catch (error) {
      setError(String(error));
      ui.fail(error);
    } finally {
      setBusy(false);
    }
  }
  if (!visible) return null;
  return (
    <aside className="context-preview" aria-label="注入プレビュー">
      <h3>注入プレビュー</h3>
      <label>
        注入方法
        <select
          aria-label="注入方法"
          value={state.mode}
          onChange={(event) =>
            ui.store.setState({ mode: event.target.value as "none" | "select" })
          }
        >
          <option value="select">Contextを選ぶ</option>
          <option value="none">注入なし（明示）</option>
        </select>
      </label>
      {(["include", "exclude"] as const).map((field) => (
        <section
          key={field}
          aria-label={field === "include" ? "注入する順序" : "除外する固定版"}
        >
          <h4>
            {field === "include" ? "注入する順序" : "除外する固定版"} (
            {state[field].length})
          </h4>
          {!state[field].length && <p className="context-meta">未選択</p>}
          <PageList
            label={field === "include" ? "注入する順序" : "除外する固定版"}
            items={state[field]}
            listClassName="context-ref-list"
          >
            {(ref, index) => (
              <li key={`${refKey(ref)}:${index}`}>
                <span>{refLabel(ref)}</span>
                <div className="context-actions">
                  <button
                    disabled={index === 0}
                    aria-label={`${field} ${index + 1}を上へ`}
                    onClick={() => {
                      const values = [...state[field]];
                      [values[index - 1], values[index]] = [
                        values[index]!,
                        values[index - 1]!,
                      ];
                      update(field, values);
                    }}
                  >
                    ↑
                  </button>
                  <button
                    disabled={index === state[field].length - 1}
                    aria-label={`${field} ${index + 1}を下へ`}
                    onClick={() => {
                      const values = [...state[field]];
                      [values[index + 1], values[index]] = [
                        values[index]!,
                        values[index + 1]!,
                      ];
                      update(field, values);
                    }}
                  >
                    ↓
                  </button>
                  <button
                    onClick={() =>
                      update(
                        field,
                        state[field].filter((_, i) => i !== index),
                      )
                    }
                  >
                    選択を外す
                  </button>
                </div>
              </li>
            )}
          </PageList>
        </section>
      ))}
      <button onClick={() => ui.store.setState({ include: [], exclude: [] })}>
        選択をクリア
      </button>
      <button
        className="primary"
        disabled={busy}
        onClick={() => void preview()}
      >
        {busy ? "確認中…" : "選択した固定版を確認してプレビュー"}
      </button>
      {!current && result && (
        <p className="context-warning">
          選択または閲覧権限の確認状態が変わりました。もう一度プレビューしてください。
        </p>
      )}
      {error && <p role="alert">{error}</p>}
      {current && result && (
        <>
          <Completeness meta={result.meta} />
          {!result.data?.ready && (
            <div role="alert">
              <strong>入力は未確定です</strong>
              <ul>
                {result.data?.missing.map((gap, index) => (
                  <li key={index}>
                    {reasonText(gap.code)}{" "}
                    {gap.source && reasonText(gap.source)}{" "}
                    {gap.ref && refLabel(gap.ref)}
                  </li>
                ))}
              </ul>
            </div>
          )}
          {manifest && (
            <>
              <p className="context-success">
                {manifest.state === "none"
                  ? "注入なしを確認しました。"
                  : "選択した固定版の入力を確認しました。"}{" "}
                読取状態: {manifest.read_status}
              </p>
              {preparation && navigation?.returnTo && (
                <button
                  disabled={busy || !result.data?.ready}
                  onClick={() => void useInput()}
                >
                  この固定入力をRunの準備へ戻す（起動しない）
                </button>
              )}
              {definition && onDefinitionInput && ui.canCreate && (
                <>
                  <button
                    disabled={busy || !result.data?.ready || !resolvedRequest}
                    onClick={() =>
                      resolvedRequest &&
                      onDefinitionInput({
                        definition: {
                          id: definition.id,
                          version: definition.version,
                        },
                        selection: resolvedRequest.selection,
                      })
                    }
                  >
                    この選択を定義の下書きへ戻す（共有保存しない）
                  </button>
                </>
              )}
              <h4>構造・順序・抜粋</h4>
              <PageList
                key={manifest.digest + ":placements"}
                label="この入力の配置"
                items={manifest.placements}
              >
                {(placement, index) => (
                  <li key={index}>
                    {placement.path.join(" / ")} · {refLabel(placement.ref)}{" "}
                    {placement.excerpt ? "（抜粋）" : "（元の構造）"}
                  </li>
                )}
              </PageList>
              <h4>由来・固定参照・取得状態</h4>
              {result.data?.sources?.length ? (
                <PageList
                  key={manifest.digest + ":sources"}
                  label="この入力の由来"
                  items={result.data.sources}
                >
                  {(source, index) => (
                    <li key={index}>
                      {refLabel(source.ref)}
                      <Disclosure title="由来と取得状態の詳細">
                        {() => <pre>{JSON.stringify(source, null, 2)}</pre>}
                      </Disclosure>
                    </li>
                  )}
                </PageList>
              ) : (
                <p>外部参照・添付・由来の指定はありません。</p>
              )}
              <h4>まとめた重複</h4>
              {manifest.duplicates.length ? (
                <PageList
                  key={manifest.digest + ":duplicates"}
                  label="まとめた重複"
                  items={manifest.duplicates}
                >
                  {(ref, index) => <li key={index}>{refLabel(ref)}</li>}
                </PageList>
              ) : (
                <p>なし</p>
              )}
              <h4>除外した項目</h4>
              {manifest.excluded.length ? (
                <PageList
                  key={manifest.digest + ":excluded"}
                  label="除外した項目"
                  items={manifest.excluded}
                >
                  {(ref, index) => <li key={index}>{refLabel(ref)}</li>}
                </PageList>
              ) : (
                <p>なし</p>
              )}
              <details open>
                <summary>実入力・添付（固定した内容）</summary>
                <pre>{manifest.input_utf8 ?? "入力なし"}</pre>
              </details>
              <Disclosure title="版・由来の記録">
                {() => (
                  <pre>
                    {JSON.stringify(resultData(result).manifest, null, 2)}
                  </pre>
                )}
              </Disclosure>
            </>
          )}
        </>
      )}
    </aside>
  );
}
