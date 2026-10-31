import { ScreenContent } from "./navigation";
import { useEffect, useState } from "react";
import { editorStore, type Ref } from "./context-ui";
import { syncRequest } from "./auth";
import { ClientSelection, type DefinitionTarget } from "./client-selection";
import type { VersionRef } from "./model-connections";
import { fieldSource } from "./workflow-json";

export function DefinitionGeneration({
  native,
  api,
  user,
  workspace,
  canEdit,
  target,
  text,
  onApplied,
  onDraft,
  manualPaths,
}: {
  native: boolean;
  api: string;
  user: string;
  workspace: string;
  canEdit: boolean;
  target: VersionRef;
  text: string;
  onApplied: DefinitionTarget["onApplied"];
  onDraft: DefinitionTarget["onDraft"];
  manualPaths?: string[];
}) {
  const [store] = useState(() => editorStore());
  const selection = fieldSource(text, ["selection"]);
  useEffect(() => {
    try {
      const value = JSON.parse(selection || '{"mode":"select"}') as {
        mode: "select" | "none";
        include?: Ref[];
        exclude?: Ref[];
      };
      store.setState({
        mode: value.mode,
        include: value.include ?? [],
        exclude: value.exclude ?? [],
      });
    } catch {
      // The definition editor owns invalid drafts; do not replace its contents.
    }
  }, [target.id, selection, store]);
  const [devices, setDevices] = useState<{ id: string; name: string }[]>([]);
  const [next, setNext] = useState("");
  const [sources, setSources] = useState<VersionRef[]>([]);
  const [notice, setNotice] = useState("");
  const [busy, setBusy] = useState(false);
  const [connection, setConnection] = useState("");
  const [deviceName, setDeviceName] = useState("");
  const [capabilities, setCapabilities] = useState<{
    id: string;
    version: string;
    connections: {
      kind: string;
      target: string;
      version: string;
      confirmed: boolean;
    }[];
  }>();
  async function devicesPage(after = "") {
    setBusy(true);
    setNotice("");
    try {
      const response = (await syncRequest(
        `/access/workspaces/${encodeURIComponent(workspace)}/devices?after=${encodeURIComponent(after)}`,
        native,
        { method: "GET" },
      )) as { data: { items: typeof devices; next_cursor?: string } };
      setDevices(response.data.items);
      setNext(response.data.next_cursor ?? "");
    } catch {
      setNotice(
        "端末一覧を取得できません。現在の権限と接続を確認してください。",
      );
    } finally {
      setBusy(false);
    }
  }
  async function chooseDevice(id: string) {
    if (!id) {
      setCapabilities(undefined);
      setSources([]);
      setConnection("");
      return;
    }
    setBusy(true);
    setNotice("");
    try {
      const current = (await syncRequest(
        `/workflows/workspaces/${encodeURIComponent(workspace)}/capabilities/${encodeURIComponent(id)}?version=head`,
        native,
        { method: "GET" },
      )) as NonNullable<typeof capabilities>;
      setCapabilities(current);
      setDeviceName(devices.find((d) => d.id === id)?.name ?? id);
      setSources([{ id: current.id, version: current.version }]);
      setConnection("");
    } catch {
      setNotice(
        "端末の確認済み接続を取得できません。取得できない能力を補って生成しません。",
      );
    } finally {
      setBusy(false);
    }
  }
  return (
    <>
      <ScreenContent>
        {
          <section className="team" aria-label="Run定義の生成設定">
            <h3>利用する端末・接続（未設定でも質問として残せます）</h3>
            <button disabled={busy} onClick={() => void devicesPage()}>
              端末を50件取得
            </button>
            <select
              aria-label="生成で参照する端末"
              disabled={busy}
              value={capabilities?.id.replace(/^runner:/, "") ?? ""}
              onChange={(e) => void chooseDevice(e.target.value)}
            >
              <option value="">未指定</option>
              {capabilities &&
                !devices.some(
                  (d) => d.id === capabilities.id.replace(/^runner:/, ""),
                ) && (
                  <option value={capabilities.id.replace(/^runner:/, "")}>
                    {deviceName}
                  </option>
                )}
              {devices.map((d) => (
                <option key={d.id} value={d.id}>
                  {d.name}
                </option>
              ))}
            </select>
            {next && (
              <button disabled={busy} onClick={() => void devicesPage(next)}>
                端末の次の50件
              </button>
            )}
            {capabilities && (
              <label>
                利用する接続
                <select
                  value={connection}
                  onChange={(e) => setConnection(e.target.value)}
                >
                  <option value="">依頼に応じて選ぶ・不足時は質問</option>
                  {capabilities.connections.map((c) => (
                    <option
                      key={c.kind + ":" + c.target}
                      disabled={!c.confirmed}
                      value={c.kind + ":" + c.target}
                    >
                      {c.target}・{c.version}
                      {c.confirmed ? "" : "（未確認）"}
                    </option>
                  ))}
                </select>
              </label>
            )}
            <p role="status">{notice}</p>
          </section>
        }
      </ScreenContent>
      <ClientSelection
        ui={{ native, api, user, workspace, canCreate: canEdit, store }}
        definition={{
          target,
          manualPaths,
          text,
          sources,
          onDraft,
          onApplied,
          request: connection ? `利用者が指定した接続: ${connection}` : "",
        }}
      />
    </>
  );
}
