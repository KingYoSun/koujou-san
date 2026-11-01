import { useEffect, useState } from "react";
import { syncRequest } from "./auth";
import {
  allowAnswerScope,
  answerScopeEpoch,
  saveLocalDraft,
} from "./sync-store";
import { NavigationLink, useNavigation, useScreenActive } from "./navigation";
import { WorkState } from "./workbench";
import {
  PauseAnswer,
  PauseResponse,
  QuestionView,
  type Pause,
} from "./run-pause";
import { WorkflowExecution } from "./workflow-execution";
import { DefinitionPicker } from "./definition-picker";
import {
  linkChatTarget,
  pin,
  type ChatAdoption,
  type ChatLink,
} from "./chat-api";
import type { VersionRef } from "./model-connections";

type Common = {
  native: boolean;
  api: string;
  user: string;
  workspace: string;
  role: string;
  canExecute: boolean;
};
type Work = VersionRef & {
  kind: string;
  value: { name?: string; state: string };
};

export function ChatWork(
  props: Common & {
    thread: VersionRef;
    adoption?: ChatAdoption;
    link?: ChatLink;
    onLinked?: () => Promise<void>;
  },
) {
  const {
    native,
    api,
    user,
    workspace,
    role,
    thread,
    adoption,
    link,
    onLinked,
  } = props;
  const navigation = useNavigation();
  const [notice, setNotice] = useState("");
  const [busy, setBusy] = useState(false);
  const canEdit = role !== "viewer",
    namespace = { api, user, workspace };
  const call = async <T,>(path: string, body?: unknown) => {
    const epoch = answerScopeEpoch(namespace);
    const result = await syncRequest(path, native, {
      method: body === undefined ? "GET" : "POST",
      body,
    });
    if (!allowAnswerScope(namespace, epoch)) throw Error("chat_scope_changed");
    return result as T;
  };
  async function prepare() {
    if (!adoption || !navigation) return;
    setBusy(true);
    try {
      const id = `run-definition:${crypto.randomUUID()}`;
      await saveLocalDraft(
        native,
        namespace,
        id,
        {
          version: "0",
          text: JSON.stringify(
            {
              schema: "run-definition-v1",
              name: "採用した仕様の作業",
              selection: {
                mode: "select",
                include: [adoption.context],
                exclude: [],
              },
              spec: {
                input: { command: { kind: "cli", target: "", parameters: [] } },
              },
            },
            null,
            2,
          ),
          fields: {},
          manual_paths: ["/selection"],
        },
        "workflow-definition",
      );
      navigation.navigate(
        {
          workspace,
          screen: "workflows",
          target: { id, version: "0", kind: "run-definition" },
          origin: { ...pin(thread), kind: "chat-thread" },
          view: "edit",
        },
        { related: true },
      );
    } catch {
      setNotice(
        "作業の下書きを保存できません。会話と採用したContextは再取得できます。",
      );
    } finally {
      setBusy(false);
    }
  }
  async function attach(kind: string, ref: VersionRef) {
    setBusy(true);
    setNotice("");
    try {
      await linkChatTarget(native, workspace, thread, kind, ref);
      await onLinked?.();
    } catch {
      setNotice(
        "関連付けを確認できません。現在の対象版と権限を確認してください。",
      );
    } finally {
      setBusy(false);
    }
  }
  if (adoption)
    return (
      <div>
        {canEdit && (
          <button disabled={busy} onClick={() => void prepare()}>
            この仕様でRun定義を準備
          </button>
        )}
        <p role="status">{notice}</p>
      </div>
    );
  if (link) {
    if (link.kind === "run") return <ChatRun {...props} target={link.target} />;
    if (link.kind === "execution" || link.kind === "workflow-definition")
      return (
        <section aria-label="関連工程">
          <h4>実行記録 · {link.name || "関連する工程"}</h4>
          <WorkflowExecution
            {...props}
            canEdit={canEdit}
            definition={
              link.kind === "workflow-definition" ? link.target : undefined
            }
            selected={link.kind === "execution" ? link.target : undefined}
            onSelect={(ref) => void attach("execution", ref)}
          />
        </section>
      );
    return (
      <NavigationLink
        related
        to={{
          workspace,
          screen: link.kind === "issue" ? "context" : "workflows",
          target: {
            ...link.target,
            kind: link.kind === "issue" ? "scope" : link.kind,
          },
          origin: { ...thread, kind: "chat-thread" },
        }}
      >
        {link.name || (link.kind === "issue" ? "関連Issue" : "関連する定義")}
        を開く
      </NavigationLink>
    );
  }
  if (!canEdit) return null;
  return (
    <details>
      <summary>既存のIssue・Run・工程を関連付ける</summary>
      <fieldset disabled={busy}>
        <DefinitionPicker
          label="関連Run・工程"
          load={async (after) =>
            (
              await call<{ data: { items: Work[]; next: string } }>(
                `/workbench/workspaces/${encodeURIComponent(workspace)}/query`,
                { kind: "work", after, limit: 50 },
              )
            ).data
          }
          onChoose={(ref) =>
            void attach(ref.id.startsWith("run:") ? "run" : "execution", ref)
          }
        />
        <DefinitionPicker
          label="関連Issue"
          load={async (after) => {
            const page = await call<{
              data: {
                items: {
                  ref: { id: string; revision: string };
                  name: string;
                  scope_kind: string;
                }[];
              };
              meta: { next_cursor?: string };
            }>(
              `/contexts/workspaces/${encodeURIComponent(workspace)}/catalog`,
              { kind: "scope", after },
            );
            return {
              items: page.data.items
                .filter((r) => r.scope_kind === "issue")
                .map((r) => ({
                  id: "scope:" + r.ref.id,
                  version: r.ref.revision,
                  value: { name: r.name },
                })),
              next: page.meta.next_cursor ?? "",
            };
          }}
          onChoose={(ref) => void attach("issue", ref)}
        />
      </fieldset>
      <p role="status">{notice}</p>
    </details>
  );
}

function ChatRun(props: Common & { target: VersionRef }) {
  const { native, api, user, workspace, role, target } = props;
  const visible = useScreenActive(),
    namespace = { api, user, workspace };
  const [record, setRecord] = useState<
    VersionRef & {
      value: Parameters<typeof WorkState>[0]["value"] & {
        active_pause?: VersionRef;
      };
    }
  >();
  const [pause, setPause] = useState<VersionRef & { value: Pause }>();
  const [notice, setNotice] = useState("");
  async function load() {
    const epoch = answerScopeEpoch(namespace);
    const base = `/runs/workspaces/${encodeURIComponent(workspace)}/records/`;
    try {
      const nextRecord = (await syncRequest(
        `${base}${encodeURIComponent(target.id)}?version=head`,
        native,
        { method: "GET" },
      )) as NonNullable<typeof record>;
      const question =
        nextRecord.value.active_pause ??
        (pause ? { id: pause.id, version: "head" } : undefined);
      const nextPause = question
        ? ((await syncRequest(
            `${base}${encodeURIComponent(question.id)}?version=${encodeURIComponent(question.version)}`,
            native,
            { method: "GET" },
          )) as NonNullable<typeof pause>)
        : undefined;
      if (!allowAnswerScope(namespace, epoch)) return;
      setRecord(nextRecord);
      setPause(nextPause);
      setNotice("");
    } catch {
      setRecord(undefined);
      setPause(undefined);
      setNotice(
        "現在のRun・質問を取得できません。権限と接続を確認してください。",
      );
    }
  }
  useEffect(() => {
    if (visible) void load();
  }, [visible, target.id, target.version]);
  return (
    <section aria-label="関連Runの現在の状態">
      <h4>実行記録 · {record?.value.name || "関連Run"}</h4>
      <button onClick={() => void load()}>Run・質問・結果を更新</button>
      {record && (
        <>
          <WorkState value={record.value} />
          <NavigationLink
            related
            to={{
              workspace,
              screen: "work",
              target: { ...pin(record), kind: "run" },
            }}
          >
            取得したRun・出力・検品の原記録を開く
          </NavigationLink>
          {pause && (
            <section>
              <p>
                質問への回答:{" "}
                {{
                  waiting: "未回答",
                  received: "API受付済み",
                  delivered: "配送確認済み",
                  resolved: "再開確認済み",
                }[pause.value.state] ?? pause.value.state}
              </p>
              <QuestionView value={pause.value.request} />
              {pause.value.state === "waiting" && role !== "viewer" ? (
                <PauseAnswer
                  native={native}
                  namespace={namespace}
                  runID={target.id}
                  record={pause}
                  onAccepted={load}
                  onDenied={async () => {
                    setRecord(undefined);
                    setPause(undefined);
                  }}
                />
              ) : (
                pause.value.response !== undefined && (
                  <PauseResponse
                    value={pause.value.response}
                    request={pause.value.request}
                  />
                )
              )}
            </section>
          )}
          <details>
            <summary>関連付けた固定版</summary>
            <p>
              {target.id} / {target.version}
            </p>
          </details>
        </>
      )}
      <p role="status">{notice}</p>
    </section>
  );
}
