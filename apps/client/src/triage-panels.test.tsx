import { renderToStaticMarkup } from "react-dom/server";
import { expect, it } from "vitest";
import { TriageIssue } from "./triage-panels";

it("offers Issue information creation before a query, only to editors", () => {
  const render = (canEdit: boolean) =>
    renderToStaticMarkup(
      <TriageIssue
        scope={{ id: "scope:issue", version: "fixed" }}
        canEdit={canEdit}
        admin={false}
        call={async () => {
          throw Error("no query during render");
        }}
        write={async () => {
          throw Error("no write during render");
        }}
        run={async (action) => action()}
      />,
    );
  expect(render(true)).toContain("Issue情報を新規作成");
  expect(render(true)).toContain("Issue情報を取得");
  expect(render(false)).not.toContain("Issue情報を新規作成");
});
