import { useEffect, useState } from "react";
import type { Options } from "react-markdown";

type Plugins = NonNullable<Options["rehypePlugins"]>;
type Node = {
  type: string;
  value?: string;
  properties?: { className?: unknown };
  children?: Node[];
};

// Claude Code colors these keywords with its "_storage" scope instead of the
// keyword color (read from the Claude Code 2.1.322 highlighter).
export const STORAGE_KEYWORDS = new Set([
  "const",
  "let",
  "var",
  "function",
  "class",
  "type",
  "interface",
  "enum",
  "namespace",
  "module",
  "def",
  "fn",
  "func",
  "struct",
  "trait",
  "impl",
]);

const classes = (n: Node): string[] =>
  Array.isArray(n.properties?.className)
    ? (n.properties!.className as string[])
    : [];

// Tags storage keywords as hljs-storage; only Claude's palette colors it.
export function rehypeStorage() {
  const walk = (n: Node) => {
    if (n.type === "element" && classes(n).includes("hljs-keyword")) {
      const t = n.children?.length === 1 ? n.children[0] : undefined;
      if (t?.type === "text" && STORAGE_KEYWORDS.has((t.value ?? "").trim()))
        n.properties!.className = [...classes(n), "hljs-storage"];
    }
    n.children?.forEach(walk);
  };
  return (tree: Node) => walk(tree);
}

// highlight.js (through rehype-highlight) is loaded on first use, in its own
// chunk. Only fences that name a language are highlighted, as in both CLIs.
let loaded: Plugins | null = null;
let loading: Promise<Plugins> | null = null;
export function loadHighlighter(): Promise<Plugins> {
  loading ??= import("rehype-highlight").then((m) => {
    loaded = [[m.default, { detect: false }], rehypeStorage];
    return loaded;
  });
  return loading;
}

// Text with a labeled fence (```ts) gets the highlighter once it has loaded;
// until then it renders plain.
const LABELED_FENCE = /^ {0,3}(`{3,}|~{3,})[ \t]*[\w.+#-]/m;
const MAX_HIGHLIGHT = 200_000;
export function useHighlighter(text: string): Plugins | undefined {
  const wanted = text.length <= MAX_HIGHLIGHT && LABELED_FENCE.test(text);
  const [plugins, setPlugins] = useState(loaded);
  useEffect(() => {
    if (!wanted || plugins) return;
    let live = true;
    loadHighlighter()
      .then((p) => live && setPlugins(p))
      .catch(() => {});
    return () => {
      live = false;
    };
  }, [wanted, plugins]);
  return wanted && plugins ? plugins : undefined;
}

// The lines of a node list, each a list of fragments: an element a line
// break falls inside is repeated on both sides, so a comment or string that
// spans lines keeps its color on each.
function splitLines(nodes: Node[]): Node[][] {
  const out: Node[][] = [[]];
  for (const n of nodes) {
    if (n.type === "text") {
      (n.value ?? "").split("\n").forEach((part, i) => {
        if (i) out.push([]);
        if (part) out[out.length - 1].push({ type: "text", value: part });
      });
    } else if (n.type === "element") {
      splitLines(n.children ?? []).forEach((frag, i) => {
        if (i) out.push([]);
        if (frag.length) out[out.length - 1].push({ ...n, children: frag });
      });
    } else out[out.length - 1].push(n);
  }
  return out;
}

export type LineTarget = { line?: number; end?: number };

// Numbers the lines of each code block (`data-line`, drawn by CSS) and marks
// those from line to end. Runs after the highlighter. Each line keeps its
// line break, so copied text is unchanged.
export function rehypeLines(target: LineTarget = {}) {
  const from = target.line ?? 0;
  const to = Math.max(from, target.end ?? from);
  const walk = (n: Node) => {
    const code = n.children?.[0] as (Node & { tagName?: string }) | undefined;
    if (
      n.type === "element" &&
      (n as { tagName?: string }).tagName === "pre" &&
      code?.type === "element" &&
      code.tagName === "code"
    ) {
      const lines = splitLines(code.children ?? []);
      if (lines.length > 1 && !lines[lines.length - 1].length) lines.pop();
      code.children = lines.map((frag, i) => ({
        type: "element",
        tagName: "span",
        properties: {
          className:
            from && i + 1 >= from && i + 1 <= to
              ? ["code-line", "target"]
              : ["code-line"],
          dataLine: i + 1,
        },
        children: [
          ...frag,
          ...(i < lines.length - 1 ? [{ type: "text", value: "\n" }] : []),
        ],
      })) as Node[];
      return;
    }
    n.children?.forEach(walk);
  };
  return (tree: Node) => walk(tree);
}
