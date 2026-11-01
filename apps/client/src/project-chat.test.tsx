import { expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { ProjectChat } from "./project-chat";

const render = (role: string, embedded = false) =>
  renderToStaticMarkup(
    <ProjectChat
      native={false}
      api="api"
      user="alice"
      workspace="w"
      role={role}
      canExecute={false}
      embedded={embedded}
    />,
  );

it("lays the chat out as a thread list beside the conversation, in the screen and in the popover", () => {
  const screen = render("editor");
  expect(screen).toContain('class="chat-layout"');
  expect(screen).toContain('aria-label="Projectとスレッド"');
  expect(screen).toContain('aria-label="会話"');
  expect(screen).toContain("Projectを再取得");
  expect(screen).not.toContain("data-embedded");
  const popover = render("editor", true);
  expect(popover).toContain('class="chat-layout" data-embedded="true"');
  expect(popover).toContain("Projectを再取得");
});
it("does not render the composer or thread creation for a viewer before a thread is open", () => {
  const html = render("viewer");
  expect(html).not.toContain("スレッドを作成");
  expect(html).not.toContain("保存してAIへ相談");
  expect(html).toContain("スレッドを開くか");
});
