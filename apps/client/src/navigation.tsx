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
  priority: ["作業", "Issue優先度"],
  publications: ["作業", "外部起票"],
  context: ["Context", "Context"],
  workflows: ["工程", "Run・Workflow定義"],
  inspections: ["工程", "検品定義・証拠管理"],
  hopper: ["自動化", "Hopper"],
  triage: ["自動化", "Triage方針・評価"],
  operations: ["自動化", "共通の処理運用"],
  usage: ["分析", "費用・品質・利用量"],
  github: ["分析", "GitHub観測"],
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
  view?: string;
  query?: string;
  cursor?: string;
};
type Entry = {
  scope: string;
  route: Route;
  scroll: number;
  returnTo?: Route;
  returnScroll?: number;
};

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
  return `?${p}`;
}

type Navigation = {
  route: Route;
  returnTo?: Route;
  destination: (screen: Screen) => Route;
  navigate: (
    route: Route,
    options?: { replace?: boolean; related?: boolean },
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
  children,
}: {
  screens: Screen[];
  background?: boolean;
  children: ReactNode;
}) {
  const navigation = useNavigation();
  const active = !navigation || screens.includes(navigation.route.screen);
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
    return saved?.scope === scope
      ? saved
      : {
          scope,
          route: readRoute(
            typeof location === "undefined" ? "" : location.search,
            workspace,
          ),
          scroll: 0,
        };
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
      const next = stored?.scope === scope ? stored : initial();
      setEntry(next);
      requestAnimationFrame(() => window.scrollTo(0, next.scroll));
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
      document.getElementById("screen-title")?.focus();
      return;
    }
    const old = { ...current.current, scroll: window.scrollY };
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
        document.getElementById("screen-title")?.focus();
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
    navigation.navigate(to, { related });
  };
  return (
    <a {...props} href={routeHref(to)} onClick={click}>
      {children}
    </a>
  );
}
