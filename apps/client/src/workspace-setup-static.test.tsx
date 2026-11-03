import { renderToStaticMarkup } from "react-dom/server";
import { expect, it } from "vitest";
import { GiteaOAuth } from "./gitea-oauth";
import { ServerModelConnections } from "./server-model";

it("separates server administration, account authorization and Workspace sharing", () => {
  const manager = renderToStaticMarkup(
    <GiteaOAuth
      native={false}
      workspace="owned"
      serverAdmin={false}
      onSaved={async () => {}}
    />,
  );
  expect(manager).toContain("Giteaにログイン");
  expect(manager).not.toContain("OAuthアプリを追加");
  expect(manager).not.toContain("クライアントシークレット");
  const admin = renderToStaticMarkup(
    <GiteaOAuth
      native={false}
      workspace="owned"
      serverAdmin
      onSaved={async () => {}}
    />,
  );
  expect(admin).toContain("OAuthアプリを追加");
  expect(admin).toContain("コールバックURL");
  expect(admin).not.toContain("共有するGiteaアカウント");
  const serverOnly = renderToStaticMarkup(
    <GiteaOAuth
      native={false}
      workspace="owned"
      serverAdmin
      canManage={false}
      onSaved={async () => {}}
    />,
  );
  expect(serverOnly).toContain("OAuthアプリを追加");
  expect(serverOnly).not.toContain("Giteaにログイン");
  expect(
    renderToStaticMarkup(
      <ServerModelConnections native={false} workspace="owned" />,
    ),
  ).not.toContain("Workspaceへの割当");
  expect(
    renderToStaticMarkup(
      <ServerModelConnections native={false} workspace="owned" serverAdmin />,
    ),
  ).toContain("Workspaceへの割当");
});
