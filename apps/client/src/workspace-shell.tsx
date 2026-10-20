import { useEffect, useState, type ReactNode } from "react";
import {
  destinations,
  NavigationLink,
  Screen,
  ScreenContent,
  useNavigation,
  type Screen as ScreenName,
} from "./navigation";
import { Workbench } from "./workbench";
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
import type { ModelProfile } from "./model-connections";
import { ServerModelConnections } from "./server-model";

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
  const [profile, setProfile] = useState<ModelProfile>();
  const [sync, setSync] = useState("接続確認中");
  const [group, title] = destinations[route.screen];
  const groups = [...new Set(Object.values(destinations).map(([g]) => g))];
  useEffect(() => onScreen(route.screen), [route.screen, onScreen]);
  const to = (screen: ScreenName) => navigation.destination(screen);
  const common = { native, api, user, workspace, role, canExecute };
  return (
    <div className="workspace-shell">
      <header className="workspace-header">
        <div>{selector}</div>
        <p className="connection-summary">
          API: {health} / 同期: {sync} / {native ? "Windowsアプリ" : "Web"}
        </p>
      </header>
      <a className="skip-link" href="#screen-title">
        作業画面へ移動
      </a>
      <nav className="primary-navigation" aria-label="主な画面">
        {groups.map((g) => {
          const first = Object.entries(destinations).find(
            ([, [category]]) => category === g,
          )![0] as ScreenName;
          return (
            <NavigationLink
              key={g}
              to={to(first)}
              related={g === "設定"}
              aria-current={group === g ? "page" : undefined}
            >
              {g}
            </NavigationLink>
          );
        })}
      </nav>
      <nav className="section-navigation" aria-label={`${group}の画面`}>
        {Object.entries(destinations)
          .filter(([, [g]]) => g === group)
          .map(([screen, [, label]]) => (
            <NavigationLink
              key={screen}
              to={to(screen as ScreenName)}
              aria-current={route.screen === screen ? "page" : undefined}
            >
              {label}
            </NavigationLink>
          ))}
      </nav>
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
      <section className="screen" aria-labelledby="screen-title">
        <p className="breadcrumbs">
          {name} / {group} / {title}
        </p>
        <h2 id="screen-title" tabIndex={-1}>
          {title}
        </h2>
        {route.target && (
          <p className="target-reference">
            対象: {route.target.name ?? route.target.id} / 種別:{" "}
            {route.target.kind} / 版: {route.target.version}
          </p>
        )}
        {navigation.returnTo && (
          <NavigationLink to={navigation.returnTo}>
            元の対象へ戻る
          </NavigationLink>
        )}
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
        <Screen screens={["context"]} background>
          <ContextWorkspace
            {...common}
            source={source}
            onRevoked={onRevoked}
            onStatus={setSync}
          />
        </Screen>
        <Screen screens={["workflows"]}>
          <Workflows {...common} />
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
                  : "CLI接続と独立RunnerはWindowsアプリで設定します。Webにローカル実行能力はありません。"}
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
      </section>
    </div>
  );
}
