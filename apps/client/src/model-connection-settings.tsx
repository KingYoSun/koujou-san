import { NavigationLink, ScreenContent } from "./navigation";
import { useEffect, useState } from "react";
import {
  loadProfiles,
  saveProfile,
  modelReady,
  signInModel,
  signOutModel,
  openModelAuthorization,
  type ModelAuthPrompt,
  type ModelProfile,
} from "./model-connections";
import { Field } from "./field";
import { Disclosure } from "./disclosure";
import { StatusBadge } from "./status-badge";
const fresh = (): ModelProfile => ({
  id: crypto.randomUUID(),
  name: "",
  endpoint: "",
  model: "",
  allowed_models: [],
  auth: "bearer",
  provider: "compatible",
  revision: "",
  contract: "",
  prices: {},
  valuation: "estimated",
  cost: null,
});
export function ModelConnectionSettings({
  native,
  actor,
  workspace,
  selected,
  onSelected,
  selectionOnly = false,
}: {
  native: boolean;
  actor: string;
  workspace: string;
  selected?: ModelProfile;
  onSelected: (p: ModelProfile) => void;
  selectionOnly?: boolean;
}) {
  const [profiles, setProfiles] = useState<ModelProfile[]>([]),
    [draft, setDraft] = useState<ModelProfile>(fresh),
    [secret, setSecret] = useState(""),
    [allowed, setAllowed] = useState(""),
    [notice, setNotice] = useState(""),
    [busy, setBusy] = useState(false);
  const [prompt, setPrompt] = useState<ModelAuthPrompt>();
  const [priceModel, setPriceModel] = useState(""),
    [priceID, setPriceID] = useState(""),
    [priceVersion, setPriceVersion] = useState("");
  useEffect(() => {
    void loadProfiles(native, actor)
      .then(setProfiles)
      .catch(() => setNotice("接続設定を取得できませんでした。"));
  }, [native, actor]);
  function edit(p: ModelProfile) {
    setPrompt(undefined);
    setDraft(p);
    setAllowed(p.allowed_models.join("\n"));
    setSecret("");
    setPriceModel(p.allowed_models[0] ?? "");
    const r = p.prices?.[workspace + "\0" + (p.allowed_models[0] ?? "")];
    setPriceID(r?.id ?? "");
    setPriceVersion(r?.version ?? "");
  }
  async function save() {
    setBusy(true);
    setNotice("");
    try {
      const names = allowed
        .split(/\r?\n/)
        .map((v) => v.trim())
        .filter(Boolean);
      const prices = { ...(draft.prices ?? {}) };
      if (priceModel && priceID && priceVersion)
        prices[workspace + "\0" + priceModel] = {
          id: priceID,
          version: priceVersion,
        };
      const result = await saveProfile(
        native,
        actor,
        { ...draft, allowed_models: names, prices },
        secret,
      );
      setSecret("");
      setDraft(result);
      setProfiles(await loadProfiles(native, actor));
      onSelected(result);
      setNotice("接続設定を保存しました。");
    } catch {
      setNotice(
        "接続設定を保存できませんでした。URL・モデル名・認証方法・キーを確認してください。",
      );
    } finally {
      setBusy(false);
    }
  }
  async function authentication(login: boolean) {
    if (!selected) return;
    setBusy(true);
    setNotice("");
    setPrompt(undefined);
    try {
      if (login) {
        const result = await signInModel(native, selected, setPrompt);
        setDraft(result);
        onSelected(result);
        setNotice("認証情報を保存しました。");
      } else {
        const result = await signOutModel(native, selected);
        const profile = { ...selected, credentials_saved: false };
        setDraft(profile);
        onSelected(profile);
        setNotice(
          result.remote_revoked
            ? "この接続の認証を失効し、端末の認証情報を削除しました。"
            : "端末の認証情報を削除しました。提供元側の失効は確認できていません。",
        );
      }
      setProfiles(await loadProfiles(native, actor));
      setPrompt(undefined);
    } catch {
      setNotice(
        "モデル提供元の認証を完了できませんでした。ブラウザと接続設定を確認してください。",
      );
    } finally {
      setBusy(false);
    }
  }
  const selector = (
    <label>
      利用する接続
      <select
        disabled={busy}
        value={selected?.id ?? ""}
        onChange={(e) => {
          const p = profiles.find((v) => v.id === e.target.value);
          if (p) {
            onSelected(p);
            edit(p);
          }
        }}
      >
        <option value="">未選択</option>
        {profiles.map((p) => (
          <option key={p.id} value={p.id}>
            {p.name} · {p.model}
            {modelReady(native, actor, p)
              ? ""
              : p.auth === "oauth"
                ? "（ログイン待ち）"
                : "（キーの入力待ち）"}
          </option>
        ))}
      </select>
    </label>
  );
  if (selectionOnly)
    return (
      <>
        <div className="model-selector">
          {selector}
          <NavigationLink to={{ workspace, screen: "models" }} related>
            クライアントLLM接続を設定
          </NavigationLink>
        </div>
        {notice && <p role="status">{notice}</p>}
      </>
    );
  return (
    <ScreenContent>
      <section className="workspace-management">
        {selector}
        <button
          data-variant="secondary"
          disabled={busy}
          onClick={() => {
            edit(fresh());
            setNotice("");
          }}
        >
          新しいLLM接続を設定
        </button>
        <fieldset disabled={busy}>
          <legend>接続の設定</legend>
          {native && (
            <label>
              接続先の種類
              <select
                value={draft.provider ?? "compatible"}
                onChange={(e) => {
                  const provider = e.target.value as NonNullable<
                    ModelProfile["provider"]
                  >;
                  setDraft({
                    ...draft,
                    provider,
                    auth: provider === "compatible" ? "bearer" : "oauth",
                    endpoint:
                      provider === "chatgpt"
                        ? "https://chatgpt.com/backend-api/codex"
                        : provider === "copilot"
                          ? "https://api.githubcopilot.com"
                          : "https://api.openai.com/v1",
                    model: "",
                    credentials_saved: false,
                  });
                  setAllowed("");
                  setSecret("");
                  setPrompt(undefined);
                }}
              >
                <option value="compatible">
                  APIキー・互換API・ローカルAPI
                </option>
                <option value="chatgpt">ChatGPTログイン</option>
                <option value="copilot">GitHub Copilotログイン</option>
              </select>
            </label>
          )}
          <Field
            label="接続名"
            value={draft.name}
            onChange={(name) => setDraft({ ...draft, name })}
            required
          />
          <label>
            接続先URL（必須）
            <input
              value={draft.endpoint}
              required
              readOnly={
                native &&
                draft.provider !== undefined &&
                draft.provider !== "compatible"
              }
              onChange={(e) => setDraft({ ...draft, endpoint: e.target.value })}
            />
          </label>
          <Field
            label="要求するモデル名"
            value={draft.model}
            onChange={(model) => setDraft({ ...draft, model })}
            required
          />
          <Field
            label="応答で確認する実モデル名"
            value={allowed}
            onChange={setAllowed}
            rows={3}
            required
          />
          {draft.auth !== "oauth" && (
            <label>
              認証方法
              <select
                value={draft.auth}
                onChange={(e) =>
                  setDraft({
                    ...draft,
                    auth: e.target.value as ModelProfile["auth"],
                  })
                }
              >
                <option value="bearer">接続用キー</option>
                <option value="none">認証なしのローカル接続</option>
              </select>
            </label>
          )}
          {draft.auth === "bearer" && (
            <label>
              接続用キー
              <input
                type="password"
                autoComplete="off"
                value={secret}
                onChange={(e) => setSecret(e.target.value)}
              />
            </label>
          )}
          <Disclosure title="個別契約単価と費用上限">
            {() => (
              <>
                <Field
                  label="契約の識別名"
                  value={draft.contract}
                  onChange={(contract) => setDraft({ ...draft, contract })}
                />
                <Field
                  label="単価を適用する実モデル名"
                  value={priceModel}
                  onChange={setPriceModel}
                />
                <Field label="単価ID" value={priceID} onChange={setPriceID} />
                <Field
                  label="単価の版"
                  value={priceVersion}
                  onChange={setPriceVersion}
                />
                <label>
                  評価種別
                  <select
                    value={draft.valuation}
                    onChange={(e) =>
                      setDraft({ ...draft, valuation: e.target.value })
                    }
                  >
                    <option value="estimated">個別契約単価による見積り</option>
                    <option value="api_equivalent">API相当の参考額</option>
                  </select>
                </label>
                <Field
                  label="操作1回の費用上限"
                  value={draft.cost?.amount ?? ""}
                  onChange={(amount) =>
                    setDraft({
                      ...draft,
                      cost: amount
                        ? {
                            amount,
                            currency: draft.cost?.currency ?? "USD",
                            valuation: draft.valuation,
                          }
                        : null,
                    })
                  }
                />
                {draft.cost && (
                  <Field
                    label="通貨"
                    value={draft.cost.currency}
                    onChange={(currency) =>
                      setDraft({
                        ...draft,
                        cost: { ...draft.cost!, currency },
                      })
                    }
                  />
                )}
                <small>接続ID：{draft.id}</small>
              </>
            )}
          </Disclosure>
          <button
            disabled={
              !draft.name || !draft.endpoint || !draft.model || !allowed.trim()
            }
            onClick={() => void save()}
          >
            LLM接続を保存
          </button>
        </fieldset>
        {native && draft.auth === "oauth" && (
          <section aria-label="モデル提供元の認証">
            <p>
              <StatusBadge
                label="認証情報"
                value={draft.credentials_saved ? "保存済み" : "未保存"}
                tone={draft.credentials_saved ? "positive" : "neutral"}
              />
            </p>
            <button
              disabled={
                busy ||
                selected?.id !== draft.id ||
                selected?.revision !== draft.revision ||
                selected?.provider !== draft.provider
              }
              onClick={() => void authentication(true)}
            >
              モデル提供元へログイン
            </button>
            <button
              data-variant="secondary"
              disabled={
                busy || !draft.credentials_saved || selected?.id !== draft.id
              }
              onClick={() => void authentication(false)}
            >
              このLLM接続を切断
            </button>
            {prompt?.url && (
              <>
                <code>{prompt.url}</code>
                <button
                  onClick={() =>
                    void openModelAuthorization(native, prompt.url!)
                  }
                >
                  認証画面を開く
                </button>
                {prompt.code && (
                  <p>
                    認証コード：<strong>{prompt.code}</strong>
                  </p>
                )}
              </>
            )}
          </section>
        )}
        {notice && <p role="status">{notice}</p>}
      </section>
    </ScreenContent>
  );
}
