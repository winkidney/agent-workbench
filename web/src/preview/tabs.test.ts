import { describe, expect, it } from "vitest";
import {
  MAX_TABS,
  NO_TABS,
  closeTab,
  openTab,
  scaleFor,
  stored,
  tabFromPreview,
  tabTitle,
  type PreviewTab,
} from "./tabs";

const file = (path: string): PreviewTab => ({ kind: "file", path });

describe("preview tabs", () => {
  it("keeps the six most recently opened, newest first", () => {
    let s = NO_TABS;
    for (let i = 1; i <= 8; i++) s = openTab(s, file(`f${i}.html`));
    expect(s.tabs.length).toBe(MAX_TABS);
    expect(s.tabs.map((t) => t.path)).toEqual([
      "f8.html",
      "f7.html",
      "f6.html",
      "f5.html",
      "f4.html",
      "f3.html",
    ]);
    expect(s.active).toBe(0);
    // Opening one again moves it first, without a second tab.
    s = openTab(s, file("f5.html"));
    expect(s.tabs.map((t) => t.path).slice(0, 2)).toEqual([
      "f5.html",
      "f8.html",
    ]);
    expect(s.tabs.length).toBe(MAX_TABS);
  });

  it("shows a neighbour when the shown tab closes", () => {
    let s = { tabs: [file("a"), file("b"), file("c")], active: 1 };
    s = closeTab(s, 1);
    expect(s.tabs.map((t) => t.path)).toEqual(["a", "c"]);
    expect(s.active).toBe(1);
    s = closeTab(s, 1);
    expect(s.active).toBe(0);
    expect(closeTab(s, 0)).toEqual({ tabs: [], active: -1 });
    // Closing one before the shown one keeps showing it.
    expect(
      closeTab({ tabs: [file("a"), file("b")], active: 1 }, 0).active,
    ).toBe(0);
  });

  it("keeps targets only, and tells services apart", () => {
    const s = stored({
      tabs: [{ ...file("a"), stamp: "1:2", missing: true, running: true }],
      active: 0,
    });
    expect(s.tabs[0]).toEqual({
      kind: "file",
      path: "a",
      target: undefined,
      command: undefined,
      port: undefined,
      preview: undefined,
    });
    expect(
      tabFromPreview({
        id: "p1",
        session: "s",
        url: "u",
        entry: "/",
        owned: true,
        command: "npm run dev",
        port: 5173,
      }),
    ).toEqual({
      kind: "service",
      command: "npm run dev",
      port: 5173,
      preview: "p1",
    });
    expect(
      tabFromPreview({
        id: "p2",
        session: "s",
        url: "u",
        entry: "/site/index.html",
        owned: false,
      }),
    ).toEqual({ kind: "file", path: "site/index.html" });
    expect(
      tabTitle({ kind: "service", target: "http://127.0.0.1:5173/app" }),
    ).toBe("127.0.0.1:5173/app");
    expect(tabTitle(file("site/index.html"))).toBe("index.html");
  });

  it("shrinks a page laid out wider than the space", () => {
    expect(scaleFor(640, 1280)).toBe(0.5);
    expect(scaleFor(1400, 1280)).toBe(1);
    expect(scaleFor(640, 0)).toBe(1);
  });
});
