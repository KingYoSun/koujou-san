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
      .catch(() =>
        setNotice("接続設定を読み込めません。手動の選択は利用できます。"),
      );
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
      setNotice("接続設定を保存しました。モデルへの要求はまだ送っていません。");
    } catch {
      setNotice(
        "URL・モデル名・認証方法・キーを確認してください。接続先を変える場合は、その接続先用のキーを入力します。",
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
        setNotice(
          "認証情報を暗号化して保存しました。モデルへの要求はまだ送っていません。",
        );
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
    <>
      {" "}
      <label>
        利用する接続
        <select
          aria-label="利用するモデル接続"
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
    </>
  );
  if (selectionOnly)
    return (
      <>
        <div className="model-selector">
          {selector}
          <NavigationLink to={{ workspace, screen: "models" }} related>
            モデル接続を設定
          </NavigationLink>
        </div>
        {notice && <p role="status">{notice}</p>}
      </>
    );
  return (
    <ScreenContent>
      {
        <section className="workspace-management">
          <h3>選択に使うモデル接続</h3>
          <p>
            {native
              ? "Runで使うCLIとは独立し、モデル提供元へ直接接続します。既存CLIの認証情報は使いません。"
              : "Runで使うCLIとは独立したHTTP接続です。接続先のキーをこの画面で扱います。"}
          </p>
          <p>
            {native
              ? "キーはWindowsの暗号化保存を使います。"
              : "キーはこの画面を開いている間だけ保持します。開き直したら再入力してください。"}
          </p>
          {selector}
          <button
            disabled={busy}
            onClick={() => {
              edit(fresh());
              setNotice("");
            }}
          >
            新しいモデル接続を設定
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
            <label>
              接続名
              <input
                value={draft.name}
                onChange={(e) => setDraft({ ...draft, name: e.target.value })}
              />
            </label>
            <label>
              接続先URL
              <input
                placeholder="https://gateway.example/v1"
                value={draft.endpoint}
                readOnly={
                  native &&
                  draft.provider !== undefined &&
                  draft.provider !== "compatible"
                }
                onChange={(e) =>
                  setDraft({ ...draft, endpoint: e.target.value })
                }
              />
            </label>
            <label>
              要求するモデル名
              <input
                value={draft.model}
                onChange={(e) => setDraft({ ...draft, model: e.target.value })}
              />
            </label>
            <label>
              応答で確認する実モデル名（1行に1件）
              <textarea
                value={allowed}
                onChange={(e) => setAllowed(e.target.value)}
              />
            </label>
            <p>
              tool要求と構造化出力に対応した接続を使います。実モデル名が一致しない応答や不完全な結果は自動反映しません。
            </p>
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
            <details>
              <summary>個別契約単価と費用上限（任意）</summary>
              <p>
                単価はWorkspaceごとの固定版を指定できます。未設定・欠測を0として扱いません。
              </p>
              <label>
                契約の識別名
                <input
                  value={draft.contract}
                  onChange={(e) =>
                    setDraft({ ...draft, contract: e.target.value })
                  }
                />
              </label>
              <label>
                単価を適用する実モデル名
                <input
                  value={priceModel}
                  onChange={(e) => setPriceModel(e.target.value)}
                />
              </label>
              <label>
                単価ID
                <input
                  value={priceID}
                  onChange={(e) => setPriceID(e.target.value)}
                />
              </label>
              <label>
                単価の版
                <input
                  value={priceVersion}
                  onChange={(e) => setPriceVersion(e.target.value)}
                />
              </label>
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
              <label>
                操作1回の費用上限
                <input
                  value={draft.cost?.amount ?? ""}
                  onChange={(e) =>
                    setDraft({
                      ...draft,
                      cost: e.target.value
                        ? {
                            amount: e.target.value,
                            currency: draft.cost?.currency ?? "USD",
                            valuation: draft.valuation,
                          }
                        : null,
                    })
                  }
                />
              </label>
              {draft.cost && (
                <label>
                  通貨
                  <input
                    value={draft.cost.currency}
                    onChange={(e) =>
                      setDraft({
                        ...draft,
                        cost: { ...draft.cost!, currency: e.target.value },
                      })
                    }
                  />
                </label>
              )}
              <small>
                接続ID: {draft.id}
                。単価の接続欄にもこのIDを指定します。上限は記録済み費用で追加要求を止め、実請求額の上限は保証しません。
              </small>
            </details>
            <button
              disabled={
                !draft.name ||
                !draft.endpoint ||
                !draft.model ||
                !allowed.trim()
              }
              onClick={() => void save()}
            >
              モデル接続を保存
            </button>
          </fieldset>
          {native && draft.auth === "oauth" && (
            <section aria-label="モデル提供元の認証">
              <p>
                {draft.credentials_saved
                  ? "認証情報を保存済みです。利用できるモデルは提供元の契約に従います。"
                  : "接続設定を保存してから、提供元へログインしてください。"}
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
                disabled={
                  busy || !draft.credentials_saved || selected?.id !== draft.id
                }
                onClick={() => void authentication(false)}
              >
                このモデル接続を切断
              </button>
              {prompt?.url && (
                <>
                  <p>ブラウザで認証を完了してください。</p>
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
                      認証コード: <strong>{prompt.code}</strong>
                    </p>
                  )}
                </>
              )}
            </section>
          )}
          <p role="status">{notice}</p>
        </section>
      }
    </ScreenContent>
  );
}
