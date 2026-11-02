import { ScreenContent } from "./navigation";
import { useState } from "react";
import { invoke } from "@tauri-apps/api/core";
import { Field } from "./field";

const words = (s: string) => s.split(/[\s,]+/).filter(Boolean);
export function RunnerSettings({
  workspace,
  user,
}: {
  workspace: string;
  user: string;
}) {
  const [kind, setKind] = useState("cli"),
    [target, setTarget] = useState("");
  const [version, setVersion] = useState(""),
    [environment, setEnvironment] = useState("Windows");
  const [subject, setSubject] = useState(""),
    [users, setUsers] = useState(user);
  const [bindings, setBindings] = useState(""),
    [capabilities, setCapabilities] = useState("");
  const [repository, setRepository] = useState(""),
    [path, setPath] = useState("");
  const [confirmed, setConfirmed] = useState(false),
    [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState("");
  async function apply(save: boolean) {
    const label = save ? "Runner接続を保存" : "保存済みのRunner接続を再送";
    setBusy(true);
    setNotice("");
    try {
      if (save)
        await invoke("runner_configure", {
          workspace,
          connections: [
            {
              kind,
              target,
              version,
              environment,
              subject,
              confirmed,
              users: words(users),
              bindings: words(bindings),
              capabilities: words(capabilities),
              repositories: repository ? [repository] : [],
            },
          ],
          repositories: repository ? { [repository]: path } : {},
        });
      else await invoke("runner_contact", { workspace });
      setNotice(`${label}しました。`);
    } catch {
      setNotice(
        `${label}できませんでした。端末登録・入力取得/報告の許可と設定を確認してください。`,
      );
    } finally {
      setBusy(false);
    }
  }
  return (
    <ScreenContent>
      <section className="team-details">
        <h3>この端末のRunner接続</h3>
        <fieldset disabled={busy}>
          <Field label="接続の種類" value={kind} onChange={setKind} required />
          <Field
            label="接続先の論理名"
            value={target}
            onChange={setTarget}
            required
          />
          <Field
            label="確認したCLI・接続の版"
            value={version}
            onChange={setVersion}
            required
          />
          <Field
            label="実行環境"
            value={environment}
            onChange={setEnvironment}
            required
          />
          <Field
            label="CLI認証主体への参照"
            value={subject}
            onChange={setSubject}
            required
          />
          <Field
            label="依頼を許可するメンバーID"
            value={users}
            onChange={setUsers}
            rows={2}
          />
          <Field
            label="確認した入力方式"
            value={bindings}
            onChange={setBindings}
            rows={2}
          />
          <Field
            label="確認した能力"
            value={capabilities}
            onChange={setCapabilities}
            rows={2}
          />
          <label>
            <input
              type="checkbox"
              checked={confirmed}
              onChange={(e) => setConfirmed(e.target.checked)}
            />
            この版・環境で接続能力を確認済み
          </label>
          <Field
            label="repositoryの論理ID"
            value={repository}
            onChange={setRepository}
          />
          {repository && (
            <Field
              label="この端末内のrepositoryのpath"
              value={path}
              onChange={setPath}
              required
            />
          )}
          <button
            disabled={
              !kind ||
              !target ||
              !version ||
              !environment ||
              !subject ||
              (!!repository && !path)
            }
            onClick={() => void apply(true)}
          >
            Runner接続を保存
          </button>
          <button data-variant="secondary" onClick={() => void apply(false)}>
            保存済みのRunner接続を再送
          </button>
        </fieldset>
        {notice && <p role="status">{notice}</p>}
      </section>
    </ScreenContent>
  );
}
