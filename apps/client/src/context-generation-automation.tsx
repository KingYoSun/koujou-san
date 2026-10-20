import { useState } from "react";
import { syncRequest } from "./auth";
import {
  operationClientID,
  type OperationAddress,
} from "./client-operation-port";
import {
  modelReady,
  modelSnapshot,
  type ModelProfile,
  type VersionRef,
} from "./model-connections";
import type {
  GenerationPolicy,
  Saved,
  SelectionPolicy,
} from "./client-selection-host";

type Rule = {
  name: string;
  goal: string;
  channel: string;
  targets: string[];
  template: SelectionPolicy;
};
type Activation = VersionRef & {
  value: { rule: VersionRef; enabled: boolean; user: string; client: string };
};
export function ContextGenerationAutomation({
  ui,
  source,
  fixed,
  groupScopes,
  profile,
  channel,
  onOpen,
}: {
  ui: OperationAddress & { canCreate: boolean };
  source?: GenerationPolicy["source"];
  fixed: GenerationPolicy["fixed"];
  groupScopes: GenerationPolicy["group_scopes"];
  profile?: ModelProfile;
  channel: string;
  onOpen: (ref: VersionRef) => void;
}) {
  const [name, setName] = useState("出力から再利用するContext"),
    [goal, setGoal] = useState(
      "固定した出力から観測と推測を区別し、再利用するContextを作ってください。",
    ),
    [target, setTarget] = useState("*"),
    [selectedChannel, setChannel] = useState(channel),
    [mode, setMode] = useState<"auto" | "proposal">("auto");
  const [models, setModels] = useState(32),
    [mcp, setMCP] = useState(256),
    [seconds, setSeconds] = useState(600),
    [scopeText, setScopes] = useState("");
  const [rule, setRule] = useState<VersionRef>(),
    [loaded, setLoaded] = useState<SelectionPolicy>(),
    [activation, setActivation] = useState<Activation>();
  const [rules, setRules] = useState<Saved<Rule>[]>([]),
    [ruleAfter, setRuleAfter] = useState({ after: 0, after_id: "" }),
    [queue, setQueue] = useState<Saved[]>([]),
    [queueAfter, setQueueAfter] = useState("");
  const [from, setFrom] = useState(""),
    [until, setUntil] = useState(""),
    [notice, setNotice] = useState(""),
    [busy, setBusy] = useState(false);
  const base = `/client-operations/workspaces/${encodeURIComponent(ui.workspace)}`;
  const call = async <T,>(path: string, body?: unknown) =>
    syncRequest(base + path, ui.native, {
      method: body === undefined ? "GET" : "POST",
      body,
    }) as Promise<T>;
  async function work(action: () => Promise<void>) {
    setBusy(true);
    setNotice("");
    try {
      await action();
    } catch {
      setNotice("方針の版・現在の権限・接続設定を確認してください。");
    } finally {
      setBusy(false);
    }
  }
  async function save() {
    const template: SelectionPolicy = loaded ?? {
      mode,
      fixed: { mode: "select", include: [], exclude: [] },
      scopes: scopeText.split(/[,\s]+/).filter(Boolean),
      limits: { model_requests: models, mcp_requests: mcp, seconds },
      generation: {
        source: {
          run: { id: "", version: "" },
          output: { id: "", version: "" },
          path: source?.path ?? [],
          extras: source?.extras,
        },
        fixed,
        group_scopes: groupScopes,
      },
    };
    template.mode = mode;
    template.scopes = scopeText.split(/[,\s]+/).filter(Boolean);
    template.limits = {
      ...template.limits,
      model_requests: models,
      mcp_requests: mcp,
      seconds,
    };
    const id = rule?.id ?? `generation-rule:${crypto.randomUUID()}`;
    const result = await call<{ data: VersionRef }>(
      `/generation/rules/${encodeURIComponent(id)}`,
      {
        operation_id: crypto.randomUUID(),
        expected_version: rule?.version ?? "0",
        value: {
          name,
          goal,
          channel: selectedChannel,
          targets: target.split(/[,\s]+/).filter(Boolean),
          template,
        },
      },
    );
    setRule({ id: result.data.id, version: result.data.version });
    setLoaded(template);
    setNotice(
      "生成方針を保存しました。有効化は別の操作です。過去の出力は処理していません。",
    );
  }
  async function activate(enabled: boolean) {
    if (!rule || (enabled && !profile)) return;
    const result = await call<{ data: VersionRef }>("/generation/activate", {
      operation_id: crypto.randomUUID(),
      expected_version: activation?.version ?? "0",
      rule: { id: rule.id, version: rule.version },
      enabled,
      client: operationClientID,
      model: profile ? modelSnapshot(profile, ui.workspace) : {},
    });
    setActivation({
      id: result.data.id,
      version: result.data.version,
      value: { rule, enabled, user: ui.user, client: operationClientID },
    });
    setNotice(
      enabled
        ? "このクライアントを担当にして有効化しました。新しく終了したRunの指定チャンネルの固定出力を一件ずつ生成します。"
        : "今後の自動生成を無効化しました。開始済みの要求と原応答は履歴に保持します。",
    );
  }
  async function listRules(next = false) {
    const page = await call<{
      items: Saved<Rule>[];
      after: number;
      after_id: string;
    }>("/query", {
      kind: "generation-rule",
      limit: 50,
      metadata: true,
      ...(next ? ruleAfter : { after: 0, after_id: "" }),
    });
    setRules(page.items);
    setRuleAfter({ after: page.after, after_id: page.after_id });
  }
  async function load(ref: VersionRef) {
    const r = await call<Saved<Rule>>(
      `/records/${encodeURIComponent(ref.id)}?version=${encodeURIComponent(ref.version)}`,
    );
    setRule(ref);
    setName(r.value.name);
    setGoal(r.value.goal);
    setChannel(r.value.channel);
    setTarget(r.value.targets.join("\n"));
    setMode(r.value.template.mode === "auto" ? "auto" : "proposal");
    setLoaded(r.value.template);
    setModels(r.value.template.limits.model_requests);
    setMCP(r.value.template.limits.mcp_requests);
    setSeconds(r.value.template.limits.seconds);
    setScopes(r.value.template.scopes.join("\n"));
    const a = await call<Activation>(
      `/records/${encodeURIComponent(ref.id.replace(/^generation-rule:/, "generation-active:"))}?version=head`,
    ).catch(() => undefined);
    setActivation(a);
  }
  async function listQueue(next = false) {
    const page = await call<{ items: Saved[]; after_id: string }>(
      `/generation/queue?after=${encodeURIComponent(next ? queueAfter : "")}`,
    );
    setQueue(page.items);
    setQueueAfter(page.after_id);
  }
  async function backfill() {
    if (!activation) return;
    await call(
      `/generation/backfills/generation-backfill:${crypto.randomUUID()}`,
      {
        operation_id: crypto.randomUUID(),
        expected_version: "0",
        activation: { id: activation.id, version: activation.version },
        client: operationClientID,
        from: new Date(from).toISOString(),
        until: new Date(until).toISOString(),
      },
    );
    setNotice(
      "指定期間の出力を索引付きのページで照合します。同じ方針版で処理済みの出力は重ねて生成しません。",
    );
  }
  return (
    <details aria-label="出力からの自動生成方針">
      <summary>出力からの自動生成方針・担当・履歴</summary>
      <p>
        保存と有効化を分けます。Contextの更新だけでは次の生成を始めません。画面の切替中も有効化した処理は継続し、アプリを終了すると新しいモデル要求を止めます。未確認要求は履歴へ残します。
      </p>
      <fieldset disabled={!ui.canCreate || busy}>
        <label>
          方針名
          <input value={name} onChange={(e) => setName(e.target.value)} />
        </label>
        <label>
          自動生成の目的
          <textarea value={goal} onChange={(e) => setGoal(e.target.value)} />
        </label>
        <label>
          対象チャンネル
          <input
            value={selectedChannel}
            onChange={(e) => setChannel(e.target.value)}
          />
        </label>
        <label>
          対象Run・Scope・Run定義（* はWorkspaceの許可範囲）
          <textarea
            value={target}
            onChange={(e) => setTarget(e.target.value)}
          />
        </label>
        <label>
          反映方式
          <select
            value={mode}
            onChange={(e) => setMode(e.target.value as "auto" | "proposal")}
          >
            <option value="auto">検証済み変更を自動反映</option>
            <option value="proposal">案を確認して採用</option>
          </select>
        </label>
        <details>
          <summary>候補のScope・操作上限</summary>
          <label>
            候補を絞るScope ID
            <input
              value={scopeText}
              onChange={(e) => setScopes(e.target.value)}
            />
          </label>
          <label>
            モデル要求の上限
            <input
              type="number"
              min={1}
              value={models}
              onChange={(e) => setModels(Number(e.target.value))}
            />
          </label>
          <label>
            MCP要求の上限
            <input
              type="number"
              min={1}
              value={mcp}
              onChange={(e) => setMCP(Number(e.target.value))}
            />
          </label>
          <label>
            操作の上限秒
            <input
              type="number"
              min={1}
              value={seconds}
              onChange={(e) => setSeconds(Number(e.target.value))}
            />
          </label>
        </details>
        <button
          onClick={() => {
            setLoaded(undefined);
            setNotice(
              "次回の保存に現在の固定項目・所属先・対象箇所を使います。",
            );
          }}
        >
          現在の生成設定を方針へコピー
        </button>
        <button
          disabled={!name || !goal || !selectedChannel || !target}
          onClick={() => void work(save)}
        >
          生成方針を保存（有効化しない）
        </button>
        <button
          disabled={
            !rule ||
            !profile ||
            !modelReady(ui.native, ui.api + "\0" + ui.user, profile)
          }
          onClick={() => void work(() => activate(true))}
        >
          このクライアントを担当にして有効化
        </button>
        <button
          disabled={!activation?.value.enabled}
          onClick={() => void work(() => activate(false))}
        >
          自動生成を無効化
        </button>
      </fieldset>
      {rule && (
        <p>
          方針版: {rule.version} · 有効化:{" "}
          {activation?.value.enabled ? "有効" : "無効"}
        </p>
      )}
      <button disabled={busy} onClick={() => void work(() => listRules())}>
        生成方針を50件取得
      </button>
      <ul>
        {rules.map((r) => (
          <li key={r.id}>
            <button onClick={() => void work(() => load(r))}>
              {r.value.name || r.id} · {r.version}
            </button>
          </li>
        ))}
      </ul>
      {rules.length === 50 && (
        <button onClick={() => void work(() => listRules(true))}>
          方針の次の50件
        </button>
      )}
      <button onClick={() => void work(() => listQueue())}>
        自分の生成待ち・保留を50件取得
      </button>
      <ul>
        {queue.map((r) => {
          const v = r.value as {
            state: string;
            operation: string;
            client: string;
          };
          return (
            <li key={r.id}>
              {v.state} ·{" "}
              {v.client === operationClientID
                ? "このクライアント"
                : "別クライアント"}
              {v.operation && (
                <button
                  onClick={() => onOpen({ id: v.operation, version: "head" })}
                >
                  生成操作を開く
                </button>
              )}
            </li>
          );
        })}
      </ul>
      {queue.length === 50 && (
        <button onClick={() => void work(() => listQueue(true))}>
          待機の次の50件
        </button>
      )}
      <details>
        <summary>過去の出力を指定期間で照合</summary>
        <label>
          開始日時
          <input
            type="datetime-local"
            value={from}
            onChange={(e) => setFrom(e.target.value)}
          />
        </label>
        <label>
          終了日時
          <input
            type="datetime-local"
            value={until}
            onChange={(e) => setUntil(e.target.value)}
          />
        </label>
        <button
          disabled={!activation?.value.enabled || !from || !until}
          onClick={() => void work(backfill)}
        >
          指定期間を明示的に照合
        </button>
      </details>
      <p role="status">{notice}</p>
    </details>
  );
}
