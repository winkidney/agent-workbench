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
