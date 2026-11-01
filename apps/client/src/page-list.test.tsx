import { renderToStaticMarkup } from "react-dom/server";
import { expect, it, vi } from "vitest";
import { PageList } from "./page-list";
import { Disclosure } from "./disclosure";

it("bounds rendered fixed-input references independently of the response count", () => {
  const values = Array.from({ length: 10000 }, (_, index) => ({
    id: `context-${index}`,
    revision: `fixed-${index}`,
  }));
  const render = vi.fn((value: (typeof values)[number], index: number) => (
    <li key={index}>
      {value.id} · {value.revision}
    </li>
  ));
  const html = renderToStaticMarkup(
    <PageList label="この入力の配置" items={values}>
      {render}
    </PageList>,
  );
  expect(render).toHaveBeenCalledTimes(100);
  expect(html.match(/<li>/g)).toHaveLength(100);
  expect(html).toContain("1–100 / 10000件");
  expect(html).not.toContain("context-100 ·");
  expect(html).toContain("この入力の配置の次のページ");
});

it("does not materialize a closed raw record, while keeping it available when opened", () => {
  const render = vi.fn(() => <pre>fixed-raw-record</pre>);
  expect(
    renderToStaticMarkup(<Disclosure title="原記録">{render}</Disclosure>),
  ).not.toContain("fixed-raw-record");
  expect(render).not.toHaveBeenCalled();
  expect(
    renderToStaticMarkup(
      <Disclosure title="原記録" defaultOpen>
        {render}
      </Disclosure>,
    ),
  ).toContain("fixed-raw-record");
});
