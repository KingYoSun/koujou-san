import type { JSONPath } from "jsonc-parser";
import type { ReactNode } from "react";
import { JSONField } from "./json-field";

export function HopperRuleFields({
  text,
  fields,
  onChange,
  children,
}: {
  text: string;
  fields: Record<string, string | null>;
  onChange: (path: string, raw: string | null) => void;
  children?: ReactNode;
}) {
  const field = (
    label: string,
    path: JSONPath,
    format: "text" | "json" | "number" | "boolean" = "text",
    options?: readonly { value: string; label: string }[],
  ) => (
    <JSONField
      label={label}
      path={path}
      format={format}
      text={text}
      fields={fields}
      onChange={onChange}
      options={options}
    />
  );
  return (
    <div className="rule-groups">
      <fieldset>
        <legend>1. 対象結果</legend>
        {field("ルールの名前", ["name"])}
        {field(
          "監視対象（kind: run / github / gitea、id、JSON）",
          ["targets"],
          "json",
        )}
        <p>
          Run・外部CIのどの結果を評価するかを指定します。Context全件の巡回ではありません。
        </p>
      </fieldset>
      <fieldset>
        <legend>2. 条件</legend>
        {field("型付き条件（JSON）", ["condition"], "json")}
        <p>
          all / any /
          notと、run.state・run.exit_code・inspection.verdict・ci.conclusionなどの条件を使います。判定の合否はサーバーが確認します。
        </p>
      </fieldset>
      <fieldset>
        <legend>3. 読む情報・モデル</legend>
        {field("必須の固定成果物（JSON）", ["required"], "json")}
        {children}
      </fieldset>
      <fieldset>
        <legend>4. 発行する仕事</legend>
        {field("発行する仕事", ["action", "kind"], "text", [
          { value: "issue", label: "Issueを作成" },
          { value: "run", label: "Runを作成" },
        ])}
        {field("テンプレートID", ["action", "template", "id"])}
        {field("テンプレートの固定版", ["action", "template", "version"])}
        {field("通常の割当・開始へ進める", ["action", "start"], "boolean")}
        {field(
          "選ぶ結果のキーと入力先（JSON）",
          ["action", "mappings"],
          "json",
        )}
        <details>
          <summary>発行先の所属・リポジトリ</summary>
          {field("親Context（省略可、JSON）", ["action", "parent"], "json")}
          {field(
            "リポジトリと固定commit（省略可、JSON）",
            ["action", "repository"],
            "json",
          )}
        </details>
        <p>発行・Run作成・実行開始の許可は別々に確認します。</p>
      </fieldset>
      <fieldset>
        <legend>5. 反映と連鎖の制御</legend>
        {field("反映方式", ["mode"], "text", [
          { value: "auto", label: "条件を満たせば自動発行" },
          { value: "proposal", label: "提案を確認して発行" },
        ])}
        {field("派生結果も対象にする", ["include_derived"], "boolean")}
        {(
          [
            ["depth", "連鎖の深度"],
            ["publications", "起点ごとの累積発行数"],
            ["evaluations", "起点ごとの評価数"],
            ["seconds", "連鎖の期限（秒）"],
            ["per_evaluation", "評価1回の発行数"],
          ] as const
        ).map(([key, label]) => (
          <div key={key}>{field(label, ["bounds", key], "number")}</div>
        ))}
        {field("連鎖の費用上限（省略可、JSON）", ["bounds", "cost"], "json")}
        {field("同一問題の照合と再発方針（省略可、JSON）", ["problem"], "json")}
        <p>
          ルールの保存、有効化、過去結果の評価は別の操作です。共有の主体・処理枠は自動化の「共通処理制御」で設定します。
        </p>
      </fieldset>
    </div>
  );
}
