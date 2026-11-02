import { useState } from "react";
import {
  costLabel,
  type CostCondition,
  type CostRef,
} from "./usage-comparison-model";
export function ComparisonConditions({
  name,
  value,
  onChange,
  call,
  workspace,
  disabled,
}: {
  name: string;
  value: CostCondition;
  onChange: (v: CostCondition) => void;
  call: <T>(path: string, body?: unknown) => Promise<T>;
  workspace: string;
  disabled: boolean;
}) {
  const [scopePage, setScopePage] = useState<{
    data: {
      items: { name: string; ref: { id: string }; scope_kind?: string }[];
    };
    meta: { next_cursor?: string | null };
  }>();
  const [notice, setNotice] = useState("");
  const set = (key: keyof CostCondition, v: unknown) =>
    onChange({ ...value, [key]: v, cursor: undefined });
  const refs = value.basis.map((r) => r.id + "@" + r.version).join("\n");
  async function scopes(after = "") {
    try {
      setScopePage(
        await call(
          `/contexts/workspaces/${encodeURIComponent(workspace)}/catalog`,
          { kind: "scope", after },
        ),
      );
      setNotice("");
    } catch {
      setNotice("Scope一覧を取得できませんでした。");
    }
  }
  return (
    <fieldset disabled={disabled}>
      <legend>{name}の比較条件</legend>
      <label>
        {name} 開始日（UTC）
        <input
          type="date"
          value={value.from.slice(0, 10)}
          onChange={(e) => set("from", e.target.value + "T00:00:00Z")}
        />
      </label>
      <label>
        {name} 終了日（UTC・含まない）
        <input
          type="date"
          value={value.until.slice(0, 10)}
          onChange={(e) => set("until", e.target.value + "T00:00:00Z")}
        />
      </label>
      <label>
        {name} 集計のタイムゾーン
        <input
          value={value.timezone}
          onChange={(e) => set("timezone", e.target.value)}
        />
      </label>
      <label>
        {name} 実際に使ったモデル
        <input
          value={value.model}
          onChange={(e) => set("model", e.target.value)}
        />
      </label>
      <label>
        {name} 費用の評価種別
        <select
          value={value.kind}
          onChange={(e) => set("kind", e.target.value)}
        >
          {[
            "usage",
            "reported",
            "estimated",
            "api_equivalent",
            "allocated",
          ].map((k) => (
            <option key={k} value={k}>
              {costLabel(k)}
            </option>
          ))}
        </select>
      </label>
      <label>
        {name} 通貨
        <input
          value={value.currency}
          onChange={(e) => set("currency", e.target.value)}
        />
      </label>
      <p>
        対象Scope：
        {value.scopes.length
          ? value.scopes.join("、")
          : "Workspace内の閲覧可能な範囲すべて"}
      </p>
      <button data-variant="secondary" onClick={() => void scopes()}>
        {name} 対象Scopeを取得
      </button>
      {scopePage && (
        <>
          <p>
            {scopePage.data.items.length
              ? `${scopePage.data.items.length}件を表示${scopePage.meta.next_cursor ? "（続きあり）" : ""}`
              : "Scopeはありません。"}
          </p>
          <ul>
            {scopePage.data.items.map((s) => (
              <li key={s.ref.id}>
                <button
                  aria-pressed={value.scopes.includes(s.ref.id)}
                  onClick={() =>
                    set(
                      "scopes",
                      value.scopes.includes(s.ref.id)
                        ? value.scopes.filter((id) => id !== s.ref.id)
                        : [...value.scopes, s.ref.id],
                    )
                  }
                >
                  {value.scopes.includes(s.ref.id)
                    ? "対象から外す："
                    : "対象にする："}
                  {s.name || s.ref.id}
                </button>
              </li>
            ))}
          </ul>
          {scopePage.meta.next_cursor && (
            <button
              data-variant="secondary"
              onClick={() => void scopes(scopePage.meta.next_cursor!)}
            >
              {name} Scopeの次のページ
            </button>
          )}
          <button data-variant="secondary" onClick={() => set("scopes", [])}>
            {name} Scopeの絞り込みを解除
          </button>
        </>
      )}
      <details>
        <summary>{name} 工程・固定版・帰属で絞る</summary>
        <label>
          {name} 工程の実行ID
          <input
            value={value.workflow}
            onChange={(e) => set("workflow", e.target.value)}
          />
        </label>
        <label>
          {name} 帰属
          <select
            value={value.attribution}
            onChange={(e) => set("attribution", e.target.value)}
          >
            <option value="">Run・操作・評価・未帰属すべて</option>
            {["run", "operation", "hopper", "triage", "unattributed"].map(
              (k) => (
                <option key={k} value={k}>
                  {costLabel(k)}
                </option>
              ),
            )}
          </select>
        </label>
        <label>
          {name} 帰属先ID
          <input
            value={value.attribution_id}
            onChange={(e) => set("attribution_id", e.target.value)}
          />
        </label>
        <label>
          {name} Context・Manifest・工程定義の固定版（1行に ID@版）
          <textarea
            value={refs}
            onChange={(e) =>
              set(
                "basis",
                e.target.value
                  .split("\n")
                  .filter(Boolean)
                  .map((text) => {
                    const at = text.lastIndexOf("@");
                    return {
                      id: at < 0 ? text : text.slice(0, at),
                      version: at < 0 ? "" : text.slice(at + 1),
                    } as CostRef;
                  }),
              )
            }
          />
        </label>
      </details>
      <p role="status">{notice}</p>
    </fieldset>
  );
}
