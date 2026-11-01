import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import {
  destinations,
  NavigationLink,
  Screen,
  ScreenContent,
  useNavigation,
  type Screen as ScreenName,
} from "./navigation";
import { PaneProvider, type Panes } from "./panes";
import { Workbench } from "./workbench";
import { ProjectChat } from "./project-chat";
import { ContextWorkspace } from "./context-workspace";
import type { OutputContextSource } from "./context-generation";
import { Workflows } from "./workflows";
import { Inspections } from "./inspections";
import { Publications } from "./publications";
import { Automation } from "./automation";
import { Hopper } from "./hopper";
import { UsagePricing } from "./usage-pricing";
import { GithubObservations } from "./github-observations";
import { ExecutionSettings } from "./execution-settings";
import { RunnerSettings } from "./runner-settings";
import { ModelConnectionSettings } from "./model-connection-settings";
import type { ModelProfile, VersionRef } from "./model-connections";
import { ServerModelConnections } from "./server-model";
import type { DefinitionContextInput } from "./context-preview";

export function WorkspaceShell({
  native,
  api,
  user,
  workspace,
  name,
  role,
  canExecute,
  members,
  membersNext,
  selector,
  management,
  account,
  connection,
  health,
  onScreen,
  onRevoked,
}: {
  native: boolean;
  api: string;
  user: string;
  workspace: string;
  name: string;
  role: string;
  canExecute: boolean;
  members: { id: string; name: string; role: string; active: boolean }[];
  membersNext?: string;
  selector: ReactNode;
  management: ReactNode;
  account: ReactNode;
  connection: ReactNode;
  health: string;
  onScreen: (screen: ScreenName) => void;
  onRevoked: () => void;
}) {
  const navigation = useNavigation()!;
  const { route } = navigation;
  const [source, setSource] = useState<OutputContextSource>();
  const [definitionRequest, setDefinitionRequest] =
    useState<DefinitionContextInput>();
  const [definitionResult, setDefinitionResult] =
    useState<DefinitionContextInput>();
  const [profile, setProfile] = useState<ModelProfile>();
  const [sync, setSync] = useState("確認中");
  const [sidebar, setSidebar] = useState<HTMLElement | null>(null);
  const [detail, setDetail] = useState<HTMLElement | null>(null);
  const panes = useMemo<Panes>(() => ({ sidebar, detail }), [sidebar, detail]);
  const [chatOpen, setChatOpen] = useState(false);
  const [chatThread, setChatThread] = useState<VersionRef>();
  const launcher = useRef<HTMLButtonElement>(null);
  const [group, title] = destinations[route.screen];
  const groups = [...new Set(Object.values(destinations).map(([g]) => g))];
  useEffect(() => onScreen(route.screen), [route.screen, onScreen]);
  const chatWasOpen = useRef(false);
  useEffect(() => {
    if (chatOpen) document.getElementById("chat-popover-title")?.focus();
    else if (chatWasOpen.current)
      launcher.current?.focus({ preventScroll: true });
    chatWasOpen.current = chatOpen;
  }, [chatOpen]);
  const to = (screen: ScreenName) => navigation.destination(screen);
  const common = { native, api, user, workspace, role, canExecute };
  return (
    <div className="workspace-shell">
      <a className="skip-link" href="#screen-title">
        作業画面へ移動
      </a>
      <aside className="workspace-sidebar">
        <div className="workspace-header">
          {selector}
          <p className="connection-summary">
            API: {health} / 同期: {sync} / {native ? "Windowsアプリ" : "Web"}
          </p>
        </div>
        <nav className="primary-navigation" aria-label="主な画面">
          {groups.map((g) => (
            <section key={g}>
              <p className="navigation-group">{g}</p>
              <ul>
                {Object.entries(destinations)
                  .filter(([, [category]]) => category === g)
                  .map(([screen, [, label]]) => (
                    <li key={screen}>
                      <NavigationLink
                        to={to(screen as ScreenName)}
                        related={g === "設定" && group !== "設定"}
                        aria-current={
                          route.screen === screen ? "page" : undefined
                        }
                      >
                        {label}
                      </NavigationLink>
                    </li>
                  ))}
              </ul>
            </section>
          ))}
        </nav>
        <section
          className="sidebar-pane"
          aria-label="画面内の一覧"
          ref={setSidebar}
        />
        <label className="compact-navigation">
          画面
          <select
            value={route.screen}
            onChange={(e) =>
              navigation.navigate(to(e.target.value as ScreenName))
            }
          >
            {groups.map((g) => (
              <optgroup label={g} key={g}>
                {Object.entries(destinations)
                  .filter(([, [category]]) => category === g)
                  .map(([screen, [, label]]) => (
                    <option key={screen} value={screen}>
                      {label}
                    </option>
                  ))}
              </optgroup>
            ))}
          </select>
        </label>
      </aside>
      <section className="screen" aria-labelledby="screen-title">
        <p className="breadcrumbs">
          {name} / {group} / {title}
          {route.target &&
            ` / ${route.target.name ?? route.target.id}（${route.target.kind}）`}
        </p>
        <h2 id="screen-title" tabIndex={-1}>
          {title}
        </h2>
        {route.target && (
          <details className="target-reference">
            <summary>対象の固定版を確認</summary>
            <p>版：{route.target.version}</p>
          </details>
        )}
        {navigation.returnTo && (
          <NavigationLink to={navigation.returnTo}>
            元の対象へ戻る
          </NavigationLink>
        )}
        <PaneProvider value={panes}>
          <Screen screens={["work"]}>
            <Workbench
              {...common}
              members={members}
              membersNext={membersNext}
              onGenerate={(next) => {
                setSource(next);
                navigation.navigate(
                  {
                    ...to("context"),
                    target: { ...next.output, kind: "output" },
                    origin: { ...next.run, kind: "run" },
                    view: "generation",
                  },
                  { related: true },
                );
              }}
            />
          </Screen>
          <Screen screens={["chat"]}>
            <ProjectChat {...common} />
          </Screen>
          <Screen screens={["context"]} background>
            <ContextWorkspace
              {...common}
              source={source}
              onRevoked={onRevoked}
              onStatus={setSync}
              definitionInput={definitionRequest}
              onDefinitionInput={(value) => {
                setDefinitionRequest(undefined);
                setDefinitionResult(value);
                navigation.navigate(
                  navigation.returnTo?.screen === "workflows" &&
                    navigation.returnTo.target?.id === value.definition.id
                    ? navigation.returnTo
                    : {
                        workspace,
                        screen: "workflows",
                        target: {
                          ...value.definition,
                          kind: "run-definition",
                        },
                        view: "edit",
                      },
                );
              }}
            />
          </Screen>
          <Screen screens={["workflows"]}>
            <Workflows
              {...common}
              definitionInput={definitionResult}
              onDefinitionInputUsed={() => setDefinitionResult(undefined)}
              onContextSelection={(value) => {
                setDefinitionRequest(value);
                setDefinitionResult(undefined);
                navigation.navigate(
                  {
                    workspace,
                    screen: "context",
                    origin: { ...value.definition, kind: "run-definition" },
                    view: "input",
                  },
                  { related: true },
                );
              }}
            />
          </Screen>
          <Screen screens={["inspections"]}>
            <Inspections {...common} />
          </Screen>
          <Screen screens={["publications"]}>
            <Publications {...common} />
          </Screen>
          <Screen screens={["priority"]}>
            <Automation
              native={native}
              workspace={workspace}
              role={role}
              surface="priorities"
            />
          </Screen>
          <Screen screens={["triage"]}>
            <Automation
              native={native}
              workspace={workspace}
              role={role}
              surface="rules"
            />
          </Screen>
          <Screen screens={["operations"]}>
            <Automation
              native={native}
              workspace={workspace}
              role={role}
              surface="operations"
            />
          </Screen>
          <Screen screens={["hopper"]}>
            <Hopper native={native} workspace={workspace} role={role} />
          </Screen>
          <Screen screens={["usage"]}>
            <UsagePricing {...common} />
          </Screen>
          <Screen screens={["pricing"]}>
            <UsagePricing {...common} settings />
          </Screen>
          <Screen screens={["github"]}>
            <GithubObservations {...common} />
          </Screen>
          <ScreenContent screens={["workspace", "devices"]}>
            {management}
          </ScreenContent>
          <ScreenContent screens={["account"]}>{account}</ScreenContent>
          <ScreenContent screens={["connection"]}>{connection}</ScreenContent>
          <Screen screens={["execution"]}>
            {native && role !== "viewer" ? (
              <>
                <ExecutionSettings workspace={workspace} />
                <RunnerSettings workspace={workspace} user={user} />
              </>
            ) : (
              <ScreenContent>
                <p>
                  {native
                    ? "CLI接続の設定には編集権限が必要です。"
                    : "CLI接続と独立RunnerはWindowsアプリで設定します。"}
                </p>
              </ScreenContent>
            )}
          </Screen>
          <Screen screens={["models"]}>
            <ModelConnectionSettings
              native={native}
              actor={api + "\0" + user}
              workspace={workspace}
              selected={profile}
              onSelected={setProfile}
            />
          </Screen>
          <Screen screens={["serverModels"]}>
            <ServerModelConnections native={native} workspace={workspace} />
          </Screen>
        </PaneProvider>
      </section>
      <aside
        className="detail-pane"
        aria-label="選択した対象の詳細"
        ref={setDetail}
      />
      {route.screen !== "chat" &&
        (chatOpen ? (
          <section
            className="chat-popover"
            role="dialog"
            aria-labelledby="chat-popover-title"
            onKeyDown={(e) => {
              if (e.key === "Escape") setChatOpen(false);
            }}
          >
            <header>
              <h3 id="chat-popover-title" tabIndex={-1}>
                Projectチャット
              </h3>
              <NavigationLink
                to={{
                  ...to("chat"),
                  ...(chatThread
                    ? { target: { ...chatThread, kind: "chat-thread" } }
                    : {}),
                }}
              >
                チャット画面で開く
              </NavigationLink>
              <button
                data-variant="secondary"
                onClick={() => setChatOpen(false)}
              >
                閉じる
              </button>
            </header>
            <ProjectChat
              {...common}
              embedded
              initialThread={chatThread}
              onThreadOpened={setChatThread}
            />
          </section>
        ) : (
          <button
            ref={launcher}
            className="chat-launcher"
            aria-haspopup="dialog"
            onClick={() => setChatOpen(true)}
          >
            Projectチャット
          </button>
        ))}
    </div>
  );
}
