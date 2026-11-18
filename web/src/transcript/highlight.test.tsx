import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { Markdown } from "./Markdown";
import { loadHighlighter } from "./highlight";

const render = async (text: string) => {
  // Once loaded, the highlighter is used from the first render.
  await loadHighlighter();
  return renderToStaticMarkup(<Markdown text={text} />);
};

describe("code block highlighting", () => {
  it("highlights fences that name a language, tagging storage keywords", async () => {
    const html = await render(
      "```ts\nconst x = 1;\nreturn x;\n```\n\n```\nplain const text\n```",
    );
    expect(html).toContain(
      '<span class="hljs-keyword hljs-storage">const</span>',
    );
    expect(html).toContain('<span class="hljs-keyword">return</span>');
    expect(html).toContain('data-lang="ts"');
    // The unlabeled block stays plain and is marked for Claude's code color.
    expect(html).toMatch(
      /class="md-code unlabeled"><button[^]*?<pre><code>plain const text\n<\/code><\/pre>/,
    );
  });

  it("leaves unknown languages and inline code plain", async () => {
    const html = await render("Use `const` here.\n\n```notalang\nconst x\n```");
    expect(html).not.toContain("hljs-keyword");
    expect(html).toContain("<code>const</code>");
  });
});

describe("numbered lines", () => {
  const lines = (html: string) =>
    Array.from(
      html.matchAll(/<span class="(code-line[^"]*)" data-line="(\d+)">/g),
      (m) => [Number(m[2]), m[1]] as const,
    );

  it("numbers each line from 1 and marks the target ones", async () => {
    await loadHighlighter();
    const html = renderToStaticMarkup(
      <Markdown text={"```\na\n\nb\nc\n```"} lines={{ line: 2, end: 3 }} />,
    );
    expect(lines(html)).toEqual([
      [1, "code-line"],
      [2, "code-line target"],
      [3, "code-line target"],
      [4, "code-line"],
    ]);
    // Each line keeps its break, so the copied text is the file's.
    expect(html.replace(/<[^>]+>/g, "")).toContain("a\n\nb\nc");
  });

  it("keeps a token's color on every line it spans", async () => {
    await loadHighlighter();
    const html = renderToStaticMarkup(
      <Markdown text={"```js\n/* one\ntwo */\nx\n```"} lines={{}} />,
    );
    expect(lines(html).map(([n]) => n)).toEqual([1, 2, 3]);
    expect(html).toContain('<span class="hljs-comment">/* one</span>');
    expect(html).toContain('<span class="hljs-comment">two */</span>');
  });

  it("are left out of the conversation's code blocks", async () => {
    await loadHighlighter();
    expect(
      renderToStaticMarkup(<Markdown text={"```js\nx\n```"} />),
    ).not.toContain("code-line");
  });
});
