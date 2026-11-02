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
import { operationStateLabels } from "./client-selection";
import { Disclosure } from "./disclosure";
import { Field } from "./field";
import { StatusBadge } from "./status-badge";

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
  const [rules, setRules] = useState<Saved<Rule>[]>(),
    [ruleAfter, setRuleAfter] = useState({ after: 0, after_id: "" }),
    [queue, setQueue] = useState<Saved[]>(),
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
      setNotice(
        "操作を実行できませんでした。方針の版・権限・接続を確認してください。",
      );
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
    setNotice("生成方針を保存しました。");
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
        ? "このクライアントを担当にして有効化しました。"
        : "自動生成を無効化しました。",
    );
  }
  async function listRules(next = false) {
    const page = await call<{
      items: Saved<Rule>[];
      after: number;
      after_id: string;
    }>("/query", {
      kind: "generation-rule",
      limit: 100,
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
    setNotice("指定期間の照合を受け付けました。");
  }
  return (
    <Disclosure title="出力からの自動生成方針・担当・履歴">
      {() => (
        <>
          <fieldset disabled={!ui.canCreate || busy}>
            <Field label="方針名" value={name} onChange={setName} />
            <Field
              label="自動生成の目的"
              value={goal}
              onChange={setGoal}
              rows={3}
            />
            <Field
              label="対象チャンネル"
              value={selectedChannel}
              onChange={setChannel}
            />
            <Field
              label="対象Run・Scope・Run定義"
              value={target}
              onChange={setTarget}
              rows={3}
            />
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
            <Disclosure title="候補のScope・操作上限">
              {() => (
                <>
                  <Field
                    label="候補を絞るScope ID"
                    value={scopeText}
                    onChange={setScopes}
                  />
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
                    操作の上限（秒）
                    <input
                      type="number"
                      min={1}
                      value={seconds}
                      onChange={(e) => setSeconds(Number(e.target.value))}
                    />
                  </label>
                </>
              )}
            </Disclosure>
            <button
              data-variant="secondary"
              onClick={() => {
                setLoaded(undefined);
                setNotice("保存時に現在の固定・所属先・対象箇所を使います。");
              }}
            >
              現在の生成設定を方針へコピー
            </button>
            <button
              disabled={!name || !goal || !selectedChannel || !target}
              onClick={() => void work(save)}
            >
              生成方針を保存
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
              data-variant="secondary"
              disabled={!activation?.value.enabled}
              onClick={() => void work(() => activate(false))}
            >
              自動生成を無効化
            </button>
          </fieldset>
          {!ui.canCreate && (
            <p role="status">編集権限がないため方針を保存できません。</p>
          )}
          {rule && (
            <p>
              <StatusBadge label="方針版" value={rule.version} />{" "}
              <StatusBadge
                label="自動生成"
                value={activation?.value.enabled ? "有効" : "無効"}
              />
            </p>
          )}
          <button
            data-variant="secondary"
            disabled={busy}
            aria-busy={busy}
            onClick={() => void work(() => listRules())}
          >
            {busy ? "生成方針を取得中…" : "生成方針を取得"}
          </button>
          {rules && (
            <p>
              {rules.length
                ? `${rules.length}件を表示${rules.length === 100 ? "（続きあり）" : ""}`
                : "該当する生成方針はありません。"}
            </p>
          )}
          <ul>
            {rules?.map((r) => (
              <li key={r.id}>
                <button
                  data-variant="secondary"
                  onClick={() => void work(() => load(r))}
                >
                  {r.value.name || r.id} · {r.version}
                </button>
              </li>
            ))}
          </ul>
          {rules?.length === 100 && (
            <button
              data-variant="secondary"
              onClick={() => void work(() => listRules(true))}
            >
              生成方針の次のページ
            </button>
          )}
          <button
            data-variant="secondary"
            disabled={busy}
            aria-busy={busy}
            onClick={() => void work(() => listQueue())}
          >
            {busy ? "生成待ち・保留を取得中…" : "自分の生成待ち・保留を取得"}
          </button>
          {queue && (
            <p>
              {queue.length
                ? `${queue.length}件を表示${queue.length === 50 ? "（続きあり）" : ""}`
                : "生成待ち・保留はありません。"}
            </p>
          )}
          <ul>
            {queue?.map((r) => {
              const v = r.value as {
                state: string;
                operation: string;
                client: string;
              };
              return (
                <li key={r.id}>
                  {operationStateLabels[v.state] ?? v.state} ·{" "}
                  {v.client === operationClientID
                    ? "このクライアント"
                    : "別クライアント"}
                  {v.operation && (
                    <button
                      data-variant="secondary"
                      onClick={() =>
                        onOpen({ id: v.operation, version: "head" })
                      }
                    >
                      生成操作を開く
                    </button>
                  )}
                </li>
              );
            })}
          </ul>
          {queue?.length === 50 && (
            <button
              data-variant="secondary"
              onClick={() => void work(() => listQueue(true))}
            >
              生成待ちの次のページ
            </button>
          )}
          <Disclosure title="過去の出力を指定期間で照合">
            {() => (
              <>
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
                  指定期間を照合
                </button>
              </>
            )}
          </Disclosure>
          <p role="status">{notice}</p>
        </>
      )}
    </Disclosure>
  );
}
