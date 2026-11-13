import { describe, expect, it } from "vitest";
import themesCss from "./themes.css?raw";
import styleCss from "./style.css?raw";
import initJs from "../public/theme-init.js?raw";
import { THEMES } from "./themes";

type Block = { selector: string; vars: Map<string, string> };

function blocks(css: string): Block[] {
  const out: Block[] = [];
  const clean = css.replace(/\/\*[\s\S]*?\*\//g, "");
  for (const m of clean.matchAll(/([^{}]+)\{([^{}]*)\}/g)) {
    const vars = new Map<string, string>();
    for (const d of m[2].matchAll(/--([\w-]+):\s*([^;]+);/g))
      vars.set(d[1], d[2].trim());
    out.push({ selector: m[1].replace(/\s+/g, " ").trim(), vars });
  }
  return out;
}

const REQUIRED = [
  "background",
  "foreground",
  "card",
  "card-foreground",
  "popover",
  "popover-foreground",
  "primary",
  "primary-foreground",
  "secondary",
  "secondary-foreground",
  "muted",
  "muted-foreground",
  "accent",
  "accent-foreground",
  "border",
  "input",
  "ring",
  "sidebar",
  "sidebar-foreground",
  "destructive",
  "success",
  "warning",
  "info",
  "diff-add",
  "diff-del",
  ...Array.from({ length: 16 }, (_, i) => `ansi-${i}`),
  "t-code",
  "t-h1",
  "t-h2",
  "t-h3",
  "t-link",
  "t-quote",
  "syn-keyword",
  "syn-string",
  "syn-number",
  "syn-function",
  "syn-type",
  "syn-property",
  "syn-variable",
  "syn-punct",
  "syn-comment",
];

// WCAG 2 contrast ratio of two #rrggbb colors.
function luminance(hex: string) {
  const [r, g, b] = [1, 3, 5].map((i) => {
    const v = parseInt(hex.slice(i, i + 2), 16) / 255;
    return v <= 0.04045 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4;
  });
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}
function contrast(a: string, b: string) {
  const [x, y] = [luminance(a), luminance(b)].sort((p, q) => q - p);
  return (x + 0.05) / (y + 0.05);
}

const theme = blocks(themesCss);
const named = THEMES.filter((t) => t.id !== "default");

function palette(id: string, mode: "light" | "dark") {
  const found = theme.filter((b) =>
    mode === "light"
      ? b.selector.includes(`[data-palette="${id}"]`) &&
        !b.selector.includes(".dark")
      : b.selector.includes(`.dark [data-palette="${id}"]`),
  );
  expect(found, `${id} ${mode} block`).toHaveLength(1);
  return found[0].vars;
}

describe("themes", () => {
  it("has a light and a dark block for every theme, and no others", () => {
    const ids = new Set(
      [...themesCss.matchAll(/data-palette="([\w-]+)"/g)].map((m) => m[1]),
    );
    expect([...ids].sort()).toEqual(named.map((t) => t.id).sort());
  });

  it("covers every color token of Default", () => {
    const own = new Set(["radius", "mono"]);
    for (const b of blocks(styleCss)) {
      const isDefault = b.selector.includes('[data-palette="default"]');
      const isAnsi = [...b.vars.keys()].some((k) => k.startsWith("ansi-"));
      if (!isDefault && !isAnsi) continue;
      for (const k of b.vars.keys())
        if (!own.has(k)) expect(REQUIRED, `--${k}`).toContain(k);
    }
  });

  for (const t of named)
    for (const mode of ["light", "dark"] as const)
      describe(`${t.name} ${mode}`, () => {
        const v = palette(t.id, mode);
        const c = (k: string) => {
          const value = v.get(k)!;
          expect(value, `--${k}`).toMatch(/^#[0-9a-f]{6}$/);
          return value;
        };
        const atLeast = (k: string, on: string[], min: number) => {
          for (const bg of on)
            expect(
              contrast(c(k), c(bg)),
              `--${k} on --${bg}`,
            ).toBeGreaterThanOrEqual(min);
        };

        it("defines every token", () => {
          for (const k of REQUIRED) expect(v.has(k), `--${k}`).toBe(true);
        });

        it("keeps text readable", () => {
          const surfaces = ["background", "card", "popover", "sidebar"];
          atLeast("foreground", [...surfaces, "muted"], 4.5);
          atLeast("muted-foreground", surfaces, 4.5);
          atLeast("muted-foreground", ["muted"], 3);
          atLeast("primary-foreground", ["primary"], 4.5);
          for (const k of ["destructive", "success", "warning", "info"])
            atLeast(k, surfaces, 3);
        });

        it("keeps conversation colors readable", () => {
          for (const k of ["t-code", "t-h1", "t-h2", "t-h3", "t-link"])
            atLeast(k, ["background", "card"], 3);
          atLeast("t-quote", ["background", "card"], 4.5);
          for (const k of REQUIRED.filter((k) => k.startsWith("syn-")))
            atLeast(k, ["muted"], k === "syn-variable" ? 4.5 : 3);
        });

        it("keeps terminal colors at least as readable as Default's", () => {
          // On tool output (--muted), Default's ANSI colors go down to 1.93:1
          // in light and 2.94:1 in dark, its grey (bright black) to 2.65:1.
          for (const i of [1, 2, 3, 4, 5, 6, 9, 10, 11, 12, 13, 14])
            atLeast(`ansi-${i}`, ["muted"], mode === "light" ? 1.9 : 2.9);
          atLeast("ansi-8", ["muted"], 2.6);
        });
      });

  it("recolors every conversation color the agents set", () => {
    const reset = blocks(themesCss).find(
      (b) => b.selector === ":root[data-palette] [data-agent]",
    )!;
    const perAgent = blocks(styleCss).filter((b) =>
      b.selector.includes("[data-agent="),
    );
    for (const b of perAgent)
      for (const k of b.vars.keys())
        expect(reset.vars.get(k), `--${k}`).toBe("inherit");
    const hljs = (css: string) =>
      new Set([...css.matchAll(/\.hljs-[\w-]+/g)].map((m) => m[0]));
    const themed = hljs(themesCss);
    for (const cls of hljs(styleCss)) expect(themed.has(cls), cls).toBe(true);
  });

  it("rounds corners only by the three radii, or fully for dots", () => {
    for (const css of [styleCss, themesCss])
      for (const [, v] of css.matchAll(/border-radius:\s*([^;]+);/g))
        expect(v, v).toMatch(
          /^(var\(--radius-(sm|md|lg)\)( |$))+(0 0)?$|^50%$|^2px$/,
        );
  });

  it("applies the same themes before the first paint", () => {
    const list = initJs.match(/\[([^\]]*)\]\.includes\(p\)/)![1];
    const ids = [...list.matchAll(/'([\w-]+)'/g)].map((m) => m[1]);
    expect(ids).toEqual(named.map((t) => t.id));
  });
});
