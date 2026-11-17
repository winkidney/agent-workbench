import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { FileList, type Entry } from "./FileList";

const entries: Entry[] = [
  { name: "dist", path: "dist", directory: true, size: 0 },
  { name: "app.zip", path: "app.zip", directory: false, size: 2048 },
];

const render = (located?: string) =>
  renderToStaticMarkup(
    <FileList
      path=""
      entries={entries}
      located={located}
      refreshing={false}
      hasSession
      onUp={() => {}}
      onRefresh={() => {}}
      onOpen={() => {}}
      onReveal={() => {}}
    />,
  );

describe("the panel's file list", () => {
  it("marks the entry a link pointed at", () => {
    const html = render("app.zip");
    expect(html).toContain('class="located" aria-haspopup="menu"');
    expect(html.match(/located/g)).toHaveLength(1);
    expect(render()).not.toContain("located");
  });
  it("offers each entry a menu", () => {
    expect(render().match(/aria-haspopup="menu"/g)).toHaveLength(2);
  });
});
