import { useEffect, useState } from "react";
import {
  loadProfiles,
  saveProfile,
  modelReady,
  type ModelProfile,
} from "./model-connections";
const fresh = (): ModelProfile => ({
  id: crypto.randomUUID(),
  name: "",
  endpoint: "",
  model: "",
  allowed_models: [],
  auth: "bearer",
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
}: {
  native: boolean;
  actor: string;
  workspace: string;
  selected?: ModelProfile;
  onSelected: (p: ModelProfile) => void;
}) {
  const [profiles, setProfiles] = useState<ModelProfile[]>([]),
    [draft, setDraft] = useState<ModelProfile>(fresh),
    [secret, setSecret] = useState(""),
    [allowed, setAllowed] = useState(""),
    [notice, setNotice] = useState(""),
    [busy, setBusy] = useState(false);
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
  return (
    <details className="workspace-management">
      <summary>選択に使うモデル接続</summary>
      <p>
        Runで使うCLIとは独立した接続です。上流の認証はGateway側、ここでは接続用キーを扱います。
      </p>
      <p>
        {native
          ? "キーはWindowsの暗号化保存を使います。"
          : "キーはこの画面を開いている間だけ保持します。開き直したら再入力してください。"}
      </p>
      <label>
        利用する接続
        <select
          aria-label="利用するモデル接続"
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
              {modelReady(native, actor, p) ? "" : "（キーの入力待ち）"}
            </option>
          ))}
        </select>
      </label>
      <button
        onClick={() => {
          edit(fresh());
          setNotice("");
        }}
      >
        新しいモデル接続を設定
      </button>
      <fieldset disabled={busy}>
        <legend>接続の設定</legend>
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
            onChange={(e) => setDraft({ ...draft, endpoint: e.target.value })}
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
              onChange={(e) => setDraft({ ...draft, contract: e.target.value })}
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
            !draft.name || !draft.endpoint || !draft.model || !allowed.trim()
          }
          onClick={() => void save()}
        >
          モデル接続を保存
        </button>
      </fieldset>
      <p role="status">{notice}</p>
    </details>
  );
}
