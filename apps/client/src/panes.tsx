import { createContext, useContext, type ReactNode } from "react";
import { createPortal } from "react-dom";

export type Panes = { sidebar: HTMLElement | null; detail: HTMLElement | null };
const PaneContext = createContext<Panes | undefined>(undefined);
export const PaneProvider = PaneContext.Provider;

function Pane({
  slot,
  children,
  inline = false,
}: {
  slot: keyof Panes;
  children: ReactNode;
  inline?: boolean;
}) {
  const panes = useContext(PaneContext);
  if (inline || panes === undefined) return <>{children}</>;
  return panes[slot] ? createPortal(children, panes[slot]) : null;
}

/** 左サイドバーへ差し込む（画面の一覧・絞り込みなど）。 */
export const SidebarPane = (props: { children: ReactNode }) => (
  <Pane slot="sidebar" {...props} />
);
/** 右の詳細ペインへ差し込む。shell外や inline 指定ではその場に描く。 */
export const DetailPane = (props: {
  children: ReactNode;
  inline?: boolean;
}) => <Pane slot="detail" {...props} />;
