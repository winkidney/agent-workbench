import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { Markdown } from "./Markdown";
import {
  DiagramCache,
  isMermaid,
  MERMAID_MAX,
  mermaidTheme,
  serialQueue,
  svgImage,
  type Tokens,
} from "./mermaid";

const FENCE = "Before\n\n```mermaid\ngraph TD\n  A-->B\n```\n\nAfter";

describe("mermaid diagrams", () => {
  it("takes fences named mermaid, in any case", () => {
    expect(isMermaid("mermaid")).toBe(true);
    expect(isMermaid("Mermaid")).toBe(true);
    expect(isMermaid("ts")).toBe(false);
    expect(isMermaid(undefined)).toBe(false);
  });

  it("shows the source until the diagram is drawn, and while written", () => {
    const html = renderToStaticMarkup(<Markdown text={FENCE} />);
    expect(html).toContain('class="md-mermaid"');
    expect(html).toContain("graph TD");
    expect(html).toContain('aria-label="Copy code"');
    expect(html).not.toContain("md-code");
    expect(html).not.toContain("md-mermaid-figure");
    // Other fences are code blocks as before.
    expect(
      renderToStaticMarkup(<Markdown text={"```ts\nconst a = 1\n```"} />),
    ).toContain("md-code");
    // While the reply is written, the source; the same block either way.
    const live = renderToStaticMarkup(<Markdown text={FENCE} streaming />);
    expect(live).toContain("md-mermaid");
    expect(live).toContain("graph TD");
    expect(live).not.toContain("md-mermaid-figure");
  });

  it("keeps one view's diagrams, bounded, for one theme", () => {
    const cache = new DiagramCache(2);
    const d = (svg: string) => ({ svg, background: "#fff" });
    cache.set("light:default", "a", d("A"));
    cache.set("light:default", "b", d("B"));
    // Reading a keeps it; adding c drops the least recently used, b.
    expect(cache.get("light:default", "a")).toEqual(d("A"));
    cache.set("light:default", "c", d("C"));
    expect(cache.size).toBe(2);
    expect(cache.get("light:default", "b")).toBeUndefined();
    expect(cache.get("light:default", "a")).toEqual(d("A"));
    // Another theme: nothing drawn for the old one is kept.
    expect(cache.get("dark:default", "a")).toBeUndefined();
    expect(cache.size).toBe(0);
  });

  it("draws one at a time, and skips owners that left", async () => {
    const enqueue = serialQueue();
    const order: string[] = [];
    let release!: () => void;
    const first = enqueue(
      () =>
        new Promise<string>((r) => {
          order.push("first starts");
          release = () => r("first");
        }),
      () => false,
    );
    let gone = false;
    const second = enqueue(
      async () => {
        order.push("second runs");
        return "second";
      },
      () => gone,
    );
    const third = enqueue(
      async () => {
        order.push("third runs");
        return "third";
      },
      () => false,
    );
    await Promise.resolve();
    // The second's owner leaves while the first is drawing.
    gone = true;
    release();
    expect(await first).toBe("first");
    expect(await second).toBeNull();
    expect(await third).toBe("third");
    expect(order).toEqual(["first starts", "third runs"]);
    // A failing task does not stop the queue.
    const failing = enqueue(
      () => Promise.reject(new Error("bad")),
      () => false,
    );
    await expect(failing).rejects.toThrow("bad");
    expect(
      await enqueue(
        async () => 1,
        () => false,
      ),
    ).toBe(1);
  });

  it("colors diagrams with the page's tokens", () => {
    const t: Tokens = {
      background: "#ffffff",
      foreground: "#111111",
      card: "#fafafa",
      muted: "#f0f0f0",
      "muted-foreground": "#777777",
      accent: "#eeeeee",
      border: "#dddddd",
      warning: "#c08000",
      destructive: "#cc0000",
      note: "#f6ead6",
    };
    const v = mermaidTheme(t, false, "Inter");
    expect(v.darkMode).toBe(false);
    expect(v.background).toBe("#ffffff");
    expect(v.primaryColor).toBe("#f0f0f0");
    expect(v.primaryTextColor).toBe("#111111");
    expect(v.lineColor).toBe("#777777");
    expect(v.noteBkgColor).toBe("#f6ead6");
    expect(v.noteBorderColor).toBe("#c08000");
    expect(v.fontFamily).toBe("Inter");
    expect(v.dropShadow).toBe("none");
    // Every value is hex, which Mermaid can shade.
    for (const [k, c] of Object.entries(v))
      if (k.endsWith("Color") || k.endsWith("Bkg") || k.endsWith("Border"))
        expect(c, k).toMatch(/^#[0-9a-f]{6}$/);
    expect(mermaidTheme(t, true, "Inter").darkMode).toBe(true);
  });

  it("leaves very large sources as code", () => {
    const big =
      "```mermaid\ngraph TD\n" + "A-->B\n".repeat(MERMAID_MAX / 5) + "```";
    const html = renderToStaticMarkup(<Markdown text={big} />);
    expect(html).toContain("md-mermaid");
    expect(html).toContain("graph TD");
  });

  it("makes a standalone image of a drawn diagram for the viewer", () => {
    const svg =
      '<svg id="mmd-1" width="100%" xmlns="http://www.w3.org/2000/svg" style="max-width: 538px;" viewBox="-8 -8 538 278"><g stroke-width="2"/></svg>';
    const url = svgImage(svg, "#101010");
    expect(url.startsWith("data:image/svg+xml;charset=utf-8,")).toBe(true);
    const out = decodeURIComponent(url.split(",").slice(1).join(","));
    expect(out).toContain('width="538" height="278"');
    expect(out).toContain('style="background:#101010"');
    expect(out).not.toContain('width="100%"');
    expect(out).not.toContain("max-width");
    // Only the root's attributes change.
    expect(out).toContain('stroke-width="2"');
  });
});
