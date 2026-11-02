import {
  createContext,
  useContext,
  useEffect,
  useRef,
  useState,
  type MouseEvent,
  type ReactNode,
} from "react";

export const destinations = {
  work: ["作業", "作業と判断"],
  chat: ["作業", "Projectチャット"],
  priority: ["作業", "Issue優先度"],
  publications: ["作業", "外部起票"],
  context: ["Context", "Context編集"],
  workflows: ["工程", "Run・Workflow定義"],
  inspections: ["工程", "検品定義・証拠管理"],
  hopper: ["自動化", "Hopper"],
  triage: ["自動化", "Triage方針・評価"],
  operations: ["自動化", "共通の処理運用"],
  usage: ["分析", "費用・品質・利用量"],
  github: ["分析", "GitHub・Gitea観測"],
  account: ["設定", "アカウント"],
  workspace: ["設定", "Workspace・参加者・招待"],
  devices: ["設定", "端末・共有ポリシー"],
  execution: ["設定", "CLI・独立Runner"],
  models: ["設定", "クライアントLLM接続"],
  serverModels: ["設定", "サーバーLLM接続"],
  pricing: ["設定", "料金表・費用評価"],
  connection: ["設定", "API接続"],
} as const;
export type Screen = keyof typeof destinations;
export type Target = {
  kind: string;
  id: string;
  version: string;
  name?: string;
};
export type Route = {
  screen: Screen;
  workspace: string;
  target?: Target;
  origin?: Target;
  input?: Target;
  view?: string;
  query?: string;
  cursor?: string;
};
type Entry = {
  scope: string;
  route: Route;
  scroll: number;
  focus?: string;
  returnTo?: Route;
  returnScroll?: number;
  returnFocus?: string;
};

export function historyActorMatches(scope: string, api: string, user: string) {
  const actor = JSON.parse(scope) as string[];
  return actor[0] === api && actor[1] === user;
}

export function restoreEntry(
  saved: Entry | undefined,
  scope: string,
  search: string,
  workspace: string,
): Entry {
  if (saved?.scope === scope) return saved;
  return {
    scope,
    route: saved ? { workspace, screen: "work" } : readRoute(search, workspace),
    scroll: 0,
  };
}

export function readRoute(search: string, workspace: string): Route {
  const p = new URLSearchParams(search);
  if (p.get("workspace") !== workspace) return { workspace, screen: "work" };
  const screen = p.get("screen") ?? "work";
  return {
    workspace,
    screen: Object.hasOwn(destinations, screen) ? (screen as Screen) : "work",
    ...(p.get("kind") && p.get("id") && p.get("version")
      ? {
          target: {
            kind: p.get("kind")!,
            id: p.get("id")!,
            version: p.get("version")!,
          },
        }
      : {}),
    ...(p.has("view") ? { view: p.get("view")! } : {}),
    ...(p.has("q") ? { query: p.get("q")! } : {}),
    ...(p.has("cursor") ? { cursor: p.get("cursor")! } : {}),
    ...Object.fromEntries(
      ["origin", "input"].flatMap((prefix) =>
        p.get(prefix + "Id") &&
        p.get(prefix + "Kind") &&
        p.get(prefix + "Version")
          ? [
              [
                prefix,
                {
                  id: p.get(prefix + "Id")!,
                  kind: p.get(prefix + "Kind")!,
                  version: p.get(prefix + "Version")!,
                },
              ],
            ]
          : [],
      ),
    ),
  };
}

export function routeHref(route: Route): string {
  const p = new URLSearchParams();
  p.set("workspace", route.workspace);
  p.set("screen", route.screen);
  if (route.target) {
    p.set("kind", route.target.kind);
    p.set("id", route.target.id);
    p.set("version", route.target.version);
  }
  if (route.view) p.set("view", route.view);
  if (route.query) p.set("q", route.query);
  if (route.cursor) p.set("cursor", route.cursor);
  for (const field of ["origin", "input"] as const) {
    const ref = route[field];
    if (ref) {
      p.set(field + "Kind", ref.kind);
      p.set(field + "Id", ref.id);
      p.set(field + "Version", ref.version);
    }
  }
  return `?${p}`;
}

type Navigation = {
  route: Route;
  returnTo?: Route;
  destination: (screen: Screen) => Route;
  navigate: (
    route: Route,
    options?: { replace?: boolean; related?: boolean; focus?: string | false },
  ) => void;
};
const NavigationContext = createContext<Navigation | undefined>(undefined);
export const useNavigation = () => useContext(NavigationContext);
const ScreenVisibility = createContext(true);
export const useScreenActive = () => useContext(ScreenVisibility);

// Keep the finite screen controllers, not hidden forms or their read effects.
export function Screen({
  screens,
  background = false,
  enabled = true,
  children,
}: {
  screens: Screen[];
  background?: boolean;
  enabled?: boolean;
  children: ReactNode;
}) {
  const navigation = useNavigation();
  const active =
    useScreenActive() &&
    enabled &&
    (!navigation || screens.includes(navigation.route.screen));
  const visited = useRef(false);
  if (active) visited.current = true;
  if (!background && !visited.current) return null;
  return (
    <ScreenVisibility.Provider value={active}>
      {children}
    </ScreenVisibility.Provider>
  );
}

export function ScreenContent({
  children,
  screens,
}: {
  children: ReactNode;
  screens?: Screen[];
}) {
  const active = useScreenActive();
  const navigation = useNavigation();
  return active &&
    (!screens || !navigation || screens.includes(navigation.route.screen))
    ? children
    : null;
}

export function NavigationProvider({
  api,
  user,
  workspace,
  children,
}: {
  api: string;
  user: string;
  workspace: string;
  children: ReactNode;
}) {
  const scope = JSON.stringify([api, user, workspace]);
  const initial = (): Entry => {
    const saved =
      typeof history === "undefined"
        ? undefined
        : (history.state?.koujou as Entry | undefined);
    return restoreEntry(
      saved,
      scope,
      typeof location === "undefined" ? "" : location.search,
      workspace,
    );
  };
  const [entry, setEntry] = useState(initial);
  const current = useRef(entry);
  const visited = useRef(new Map<Screen, Route>());
  current.current = entry;
  visited.current.set(entry.route.screen, entry.route);
  useEffect(() => {
    const previous = history.scrollRestoration;
    history.scrollRestoration = "manual";
    const restore = () => {
      const stored = history.state?.koujou as Entry | undefined;
      // Team switches Workspace first; its new provider owns this entry.
      if (
        stored &&
        stored.scope !== scope &&
        historyActorMatches(stored.scope, api, user)
      )
        return;
      const next = stored?.scope === scope ? stored : initial();
      history.replaceState(
        { ...history.state, koujou: next },
        "",
        routeHref(next.route) + location.hash,
      );
      setEntry(next);
      requestAnimationFrame(() => {
        document.getElementById(next.focus ?? "screen-title")?.focus();
        window.scrollTo(0, next.scroll);
      });
    };
    history.replaceState(
      { ...history.state, koujou: current.current },
      "",
      routeHref(current.current.route) + location.hash,
    );
    window.addEventListener("popstate", restore);
    return () => {
      history.scrollRestoration = previous;
      window.removeEventListener("popstate", restore);
    };
  }, [scope]);
  const navigate: Navigation["navigate"] = (route, options = {}) => {
    if (route.workspace !== workspace) return;
    if (
      !options.replace &&
      routeHref(route) === routeHref(current.current.route)
    ) {
      if (options.focus !== false)
        document.getElementById("screen-title")?.focus();
      return;
    }
    const old = {
      ...current.current,
      scroll: window.scrollY,
      focus: document.activeElement?.id || undefined,
    };
    history.replaceState({ ...history.state, koujou: old }, "");
    const returning =
      old.returnTo && routeHref(route) === routeHref(old.returnTo);
    const next: Entry = {
      scope,
      route,
      scroll: options.replace
        ? old.scroll
        : returning
          ? (old.returnScroll ?? 0)
          : 0,
      returnTo: options.related
        ? old.route
        : returning
          ? undefined
          : old.returnTo,
      returnScroll: options.related ? old.scroll : old.returnScroll,
      returnFocus: options.related ? old.focus : old.returnFocus,
      focus: returning ? old.returnFocus : undefined,
    };
    history[options.replace ? "replaceState" : "pushState"](
      { ...history.state, koujou: next },
      "",
      routeHref(route) + location.hash,
    );
    current.current = next;
    setEntry(next);
    if (!options.replace)
      requestAnimationFrame(() => {
        if (options.focus === false) return;
        (
          document.getElementById(
            next.focus ??
              (typeof options.focus === "string"
                ? options.focus
                : "screen-title"),
          ) ?? document.getElementById("screen-title")
        )?.focus();
        window.scrollTo(0, next.scroll);
      });
  };
  return (
    <NavigationContext.Provider
      value={{
        route: entry.route,
        returnTo: entry.returnTo,
        destination: (screen) =>
          visited.current.get(screen) ?? { workspace, screen },
        navigate,
      }}
    >
      {children}
    </NavigationContext.Provider>
  );
}

export function NavigationLink({
  to,
  children,
  related = false,
  ...props
}: {
  to: Route;
  children: ReactNode;
  related?: boolean;
  id?: string;
  className?: string;
  "aria-current"?: "page";
}) {
  const navigation = useNavigation();
  const click = (event: MouseEvent<HTMLAnchorElement>) => {
    if (
      !navigation ||
      event.button !== 0 ||
      event.ctrlKey ||
      event.metaKey ||
      event.shiftKey ||
      event.altKey
    )
      return;
    event.preventDefault();
    event.currentTarget.focus();
    navigation.navigate(to, { related });
  };
  return (
    <a
      {...props}
      className={["target-link", props.className].filter(Boolean).join(" ")}
      href={routeHref(to)}
      onClick={click}
    >
      {children}
    </a>
  );
}
