import { createContext, useSyncExternalStore } from "react";
import type { MermaidConfig } from "mermaid";

// Mermaid diagrams in replies. The library loads on the first diagram, in its
// own chunk. This module keeps the library and a queue, nothing per diagram:
// what is drawn lives in the view that shows it (DiagramCache), and goes
// when the view does.

// Larger sources stay as code.
export const MERMAID_MAX = 20_000;

export const isMermaid = (lang?: string) => lang?.toLowerCase() === "mermaid";

// Whether the Markdown is still being written: diagrams wait for the end.
export const StreamingContext = createContext(false);

export type Drawn = { svg: string; background: string } | { error: string };

// The diagrams one view drew (a session's transcript), so a row that scrolls
// back into view shows its diagram at once. Bounded, emptied when the theme
// changes, and dropped with the view.
export class DiagramCache {
  private theme = "";
  private drawn = new Map<string, Drawn>();
  constructor(private max = 32) {}
  private forTheme(theme: string) {
    if (theme !== this.theme) {
      this.drawn.clear();
      this.theme = theme;
    }
  }
  get(theme: string, code: string): Drawn | undefined {
    this.forTheme(theme);
    const d = this.drawn.get(code);
    if (d) {
      // Most recently used last.
      this.drawn.delete(code);
      this.drawn.set(code, d);
    }
    return d;
  }
  set(theme: string, code: string, d: Drawn) {
    this.forTheme(theme);
    this.drawn.delete(code);
    this.drawn.set(code, d);
    while (this.drawn.size > this.max)
      this.drawn.delete(this.drawn.keys().next().value!);
  }
  get size() {
    return this.drawn.size;
  }
}
export const DiagramCacheContext = createContext<DiagramCache | null>(null);

// Runs tasks one at a time, in order; a task whose owner has gone (cancelled)
// is skipped when its turn comes. Mermaid draws one diagram at a time.
export function serialQueue() {
  let tail: Promise<unknown> = Promise.resolve();
  return function enqueue<T>(
    task: () => Promise<T>,
    cancelled: () => boolean,
  ): Promise<T | null> {
    const run = () => (cancelled() ? Promise.resolve(null) : task());
    const p = tail.then(run, run);
    // The next task waits for this one, without holding its result (a
    // drawn diagram): catch() alone would pass the result on.
    tail = p.then(
      () => undefined,
      () => undefined,
    );
    return p;
  };
}

// The theme, as the page shows it: dark or light, and the palette.
export function themeKey(): string {
  const el = document.documentElement;
  return (
    (el.classList.contains("dark") ? "dark" : "light") +
    ":" +
    (el.getAttribute("data-palette") ?? "default")
  );
}
// One observer for every diagram on the page, gone with the last of them.
const listeners = new Set<() => void>();
let observer: MutationObserver | null = null;
function subscribe(listener: () => void) {
  listeners.add(listener);
  if (!observer) {
    observer = new MutationObserver(() => listeners.forEach((l) => l()));
    observer.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ["class", "data-palette"],
    });
  }
  return () => {
    listeners.delete(listener);
    if (!listeners.size) {
      observer?.disconnect();
      observer = null;
    }
  };
}
export const useThemeKey = () =>
  useSyncExternalStore(subscribe, themeKey, () => "light:default");

// The page's colors, by token, as #rrggbb: Mermaid computes shades with a
// library that reads hex and rgb, not oklch. "note" is amber over the page.
export type Tokens = Record<
  | "background"
  | "foreground"
  | "card"
  | "muted"
  | "muted-foreground"
  | "accent"
  | "border"
  | "warning"
  | "destructive"
  | "note",
  string
>;
export function mermaidTheme(
  t: Tokens,
  dark: boolean,
  fontFamily: string,
): NonNullable<MermaidConfig["themeVariables"]> {
  return {
    darkMode: dark,
    fontFamily,
    fontSize: "13px",
    // Flat, like the page: Mermaid's light-gray shadow glows on dark themes.
    dropShadow: "none",
    background: t.background,
    textColor: t.foreground,
    titleColor: t.foreground,
    lineColor: t["muted-foreground"],
    // Boxes and their text.
    primaryColor: t.muted,
    primaryTextColor: t.foreground,
    primaryBorderColor: t["muted-foreground"],
    mainBkg: t.muted,
    nodeBorder: t["muted-foreground"],
    secondaryColor: t.accent,
    secondaryTextColor: t.foreground,
    secondaryBorderColor: t["muted-foreground"],
    tertiaryColor: t.card,
    tertiaryTextColor: t.foreground,
    tertiaryBorderColor: t.border,
    clusterBkg: t.card,
    clusterBorder: t.border,
    edgeLabelBackground: t.background,
    // Sequence diagrams.
    actorBkg: t.muted,
    actorBorder: t["muted-foreground"],
    actorTextColor: t.foreground,
    actorLineColor: t["muted-foreground"],
    signalColor: t.foreground,
    signalTextColor: t.foreground,
    labelBoxBkgColor: t.muted,
    labelBoxBorderColor: t["muted-foreground"],
    labelTextColor: t.foreground,
    loopTextColor: t.foreground,
    activationBkgColor: t.accent,
    activationBorderColor: t["muted-foreground"],
    sequenceNumberColor: t.background,
    noteBkgColor: t.note,
    noteTextColor: t.foreground,
    noteBorderColor: t.warning,
    errorBkgColor: t.destructive,
    errorTextColor: t.background,
  };
}

// Reads the tokens through a 1×1 canvas: each color is painted over the page
// background and read back, so translucent ones come out as they look.
function readTokens(): Tokens {
  const css = getComputedStyle(document.documentElement);
  const v = (name: string) => css.getPropertyValue("--" + name).trim();
  const canvas = document.createElement("canvas");
  canvas.width = canvas.height = 1;
  const ctx = canvas.getContext("2d", { willReadFrequently: true })!;
  const hex = (color: string, alpha = 1) => {
    ctx.globalAlpha = 1;
    ctx.fillStyle = "#fff";
    ctx.fillStyle = v("background") || "#fff";
    ctx.fillRect(0, 0, 1, 1);
    ctx.globalAlpha = alpha;
    ctx.fillStyle = color || "#000";
    ctx.fillRect(0, 0, 1, 1);
    const [r, g, b] = ctx.getImageData(0, 0, 1, 1).data;
    return "#" + [r, g, b].map((n) => n.toString(16).padStart(2, "0")).join("");
  };
  const names = [
    "background",
    "foreground",
    "card",
    "muted",
    "muted-foreground",
    "accent",
    "border",
    "warning",
    "destructive",
  ] as const;
  const out = Object.fromEntries(names.map((n) => [n, hex(v(n))])) as Tokens;
  out.note = hex(v("warning"), 0.18);
  return out;
}

type Mermaid = (typeof import("mermaid"))["default"];
let library: Promise<Mermaid> | null = null;
let configured = "";
let background = "#ffffff";
let seq = 0;
const enqueue = serialQueue();

function configure(mermaid: Mermaid, theme: string) {
  if (theme === configured) return;
  const tokens = readTokens();
  background = tokens.background;
  mermaid.initialize({
    startOnLoad: false,
    // Sanitized output, no click callbacks or script links.
    securityLevel: "strict",
    // A failed parse rejects; nothing is added to the page.
    suppressErrorRendering: true,
    // Labels as SVG text, not HTML: nothing for markup to hide in, and the
    // diagram stays whole as an image in the viewer.
    htmlLabels: false,
    flowchart: { htmlLabels: false },
    maxTextSize: MERMAID_MAX,
    // Mermaid 12's own layout (ELK) stays: its 1.4 MB loads once with the
    // first diagram, and the browser keeps it (assets are immutable).
    theme: "base",
    themeVariables: mermaidTheme(
      tokens,
      theme.startsWith("dark"),
      getComputedStyle(document.body).fontFamily,
    ),
  });
  configured = theme;
}

// Draws one diagram for the current theme; null when its owner has gone or
// the theme changed before its turn (the owner asks again).
export function drawMermaid(
  code: string,
  theme: string,
  cancelled: () => boolean,
): Promise<Drawn | null> {
  return enqueue(
    async () => {
      library ??= import("mermaid").then((m) => m.default);
      const mermaid = await library;
      if (cancelled() || themeKey() !== theme) return null;
      configure(mermaid, theme);
      const id = `mmd-${++seq}`;
      try {
        const { svg } = await mermaid.render(id, code);
        return { svg, background };
      } catch (e) {
        const text = e instanceof Error ? e.message : String(e);
        return { error: text.split("\n")[0].trim() || "invalid diagram" };
      } finally {
        // What Mermaid may leave behind while drawing.
        document.getElementById(id)?.remove();
        document.getElementById("d" + id)?.remove();
      }
    },
    () => cancelled() || themeKey() !== theme,
  );
}

// The diagram as a standalone image for the viewer: its own size from the
// viewBox (Mermaid sizes it to the column), on the page's background.
export function svgImage(svg: string, background: string): string {
  const box = svg
    .match(/viewBox="([-\d.\s]+)"/)?.[1]
    .trim()
    .split(/\s+/);
  const [w, h] = box ? [box[2], box[3]] : ["800", "600"];
  const root = svg.replace(/^<svg\b[^>]*>/, (tag) =>
    tag
      .replace(/\swidth="[^"]*"/, "")
      .replace(/\sheight="[^"]*"/, "")
      .replace(/\sstyle="[^"]*"/, "")
      .replace(
        /^<svg/,
        `<svg width="${w}" height="${h}" style="background:${background}"`,
      ),
  );
  return "data:image/svg+xml;charset=utf-8," + encodeURIComponent(root);
}
