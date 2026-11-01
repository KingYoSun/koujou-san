import { ScreenContent } from "./navigation";
import { useState } from "react";
import { invoke } from "@tauri-apps/api/core";

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
      setNotice(
        "この端末のRunner接続を更新しました。commandは起動していません。",
      );
    } catch {
      setNotice(
        "端末登録・入力取得/報告の許可と設定を確認してください。接続の更新を確認できませんでした。",
      );
    } finally {
      setBusy(false);
    }
  }
  return (
    <ScreenContent>
      {
        <details className="team-details">
          <summary>この端末のRunner設定</summary>
          <fieldset disabled={busy}>
            <label>
              接続の種類
              <input value={kind} onChange={(e) => setKind(e.target.value)} />
            </label>
            <label>
              接続先の論理名
              <input
                value={target}
                onChange={(e) => setTarget(e.target.value)}
              />
            </label>
            <label>
              確認したCLI・接続の版
              <input
                value={version}
                onChange={(e) => setVersion(e.target.value)}
              />
            </label>
            <label>
              実行環境
              <input
                value={environment}
                onChange={(e) => setEnvironment(e.target.value)}
              />
            </label>
            <label>
              CLI認証主体への参照
              <input
                value={subject}
                onChange={(e) => setSubject(e.target.value)}
              />
            </label>
            <label>
              依頼を許可するメンバーID（空白区切り）
              <textarea
                value={users}
                onChange={(e) => setUsers(e.target.value)}
              />
            </label>
            <label>
              確認した入力方式（stdin、file等、空白区切り）
              <input
                value={bindings}
                onChange={(e) => setBindings(e.target.value)}
              />
            </label>
            <label>
              確認した能力（pause、stop等、空白区切り）
              <input
                value={capabilities}
                onChange={(e) => setCapabilities(e.target.value)}
              />
            </label>
            <label>
              <input
                type="checkbox"
                checked={confirmed}
                onChange={(e) => setConfirmed(e.target.checked)}
              />
              この版・環境で接続能力を確認済み
            </label>
            <label>
              repositoryの論理ID（使う場合）
              <input
                value={repository}
                onChange={(e) => setRepository(e.target.value)}
              />
            </label>
            {repository && (
              <label>
                この端末内のrepositoryのpath
                <input value={path} onChange={(e) => setPath(e.target.value)} />
              </label>
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
              接続設定を保存・更新
            </button>
            <button onClick={() => void apply(false)}>
              保存済みのRunner接続を更新
            </button>
          </fieldset>
          {notice && <p role="status">{notice}</p>}
        </details>
      }
    </ScreenContent>
  );
}
