import { fieldSources } from "./workflow-json";
import { useMemo } from "react";
import { Disclosure } from "./disclosure";
import { jsonError } from "./field";
export const definitionFields = [
  ["/name", "名称"],
  ["/device_id", "端末"],
  ["/spec/name", "作業名"],
  ["/spec/issue", "対象Issue"],
  ["/spec/input/command/kind", "道具の種類"],
  ["/spec/input/command/target", "接続先"],
  ["/spec/input/command/parameters", "引数"],
  ["/spec/input/command/binding", "入力の受渡し"],
  ["/spec/input/prompt", "指示文"],
  ["/spec/input/context", "手動の直接入力Context"],
  ["/spec/derivation", "出力の契約"],
  ["/spec/sources", "参照元"],
  ["/spec/previous", "以前のRun"],
  ["/spec/artifacts", "成果物"],
  ["/selection", "Contextの選択"],
  ["/inputs", "実行時の入力"],
  ["/required", "必要な接続能力"],
  ["/ci", "CIの観測"],
  ["/write", "外部書込みの条件"],
  ["/baseline", "基準版"],
] as const;
export function DefinitionProposal({
  before,
  after,
  paths,
  onPaths,
}: {
  before: string;
  after: string;
  paths: string[];
  onPaths: (v: string[]) => void;
}) {
  const sources = (text: string) => {
    try {
      return fieldSources(
        text,
        definitionFields.map(([path]) => path.slice(1).split("/")),
      ).map((value) => value || "未設定");
    } catch {
      return undefined;
    }
  };
  const previous = useMemo(() => sources(before), [before]);
  const proposed = useMemo(() => sources(after), [after]);
  return (
    <section aria-label="生成した定義の項目別比較">
      {(!previous || !proposed) && (
        <p role="alert">{jsonError(previous ? after : before)}</p>
      )}
      {definitionFields.map(([path, label], index) => (
        <details key={path}>
          <summary>{label}</summary>
          <label>
            <input
              type="checkbox"
              checked={paths.includes(path)}
              onChange={(e) =>
                onPaths(
                  e.target.checked
                    ? [...paths, path]
                    : paths.filter((p) => p !== path),
                )
              }
            />
            この項目を採用
          </label>
          <p>現在の下書き</p>
          <pre>{previous?.[index] ?? "未取得"}</pre>
          <p>生成案</p>
          <pre>{proposed?.[index] ?? "未取得"}</pre>
        </details>
      ))}
    </section>
  );
}

export function DefinitionFixedFields({
  paths,
  onPaths,
}: {
  paths: string[];
  onPaths: (paths: string[]) => void;
}) {
  return (
    <Disclosure title="変更させない定義の項目">
      {() =>
        definitionFields.map(([path, label]) => (
          <label key={path}>
            <input
              type="checkbox"
              checked={paths.includes(path)}
              onChange={(e) =>
                onPaths(
                  e.target.checked
                    ? [...paths, path]
                    : paths.filter((p) => p !== path),
                )
              }
            />
            {label}を固定
          </label>
        ))
      }
    </Disclosure>
  );
}
