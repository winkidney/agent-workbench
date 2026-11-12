// Markdown paragraphs and list items keep single newlines as line breaks
// (white-space: pre-line), as the CLIs print them. The newlines that
// markdown leaves between blocks (around nested lists, between loose list
// items) would then show as blank lines, so they are removed here. Breaks
// inside text, and between inline elements, stay.

type Node = {
  type: string;
  tagName?: string;
  value?: string;
  children?: Node[];
};

const CONTAINERS = new Set([
  "ul",
  "ol",
  "li",
  "blockquote",
  "div",
  "table",
  "thead",
  "tbody",
  "tfoot",
  "tr",
]);
const BLOCKS = new Set([
  ...CONTAINERS,
  "p",
  "pre",
  "h1",
  "h2",
  "h3",
  "h4",
  "h5",
  "h6",
  "hr",
  "td",
  "th",
]);

const isBlock = (n: Node | undefined) =>
  n?.type === "element" && BLOCKS.has(n.tagName ?? "");
const isBreak = (n: Node) =>
  n.type === "text" && /^\s*$/.test(n.value ?? "") && /\n/.test(n.value ?? "");

export function rehypeBlockWhitespace() {
  const walk = (n: Node) => {
    if (n.type === "element" && n.tagName === "pre") return;
    const kids = n.children;
    if (!kids) return;
    if (n.type === "root" || CONTAINERS.has(n.tagName ?? ""))
      n.children = kids.filter(
        (c, i) =>
          !isBreak(c) ||
          !(
            i === 0 ||
            i === kids.length - 1 ||
            isBlock(kids[i - 1]) ||
            isBlock(kids[i + 1])
          ),
      );
    n.children!.forEach(walk);
  };
  return (tree: Node) => walk(tree);
}
