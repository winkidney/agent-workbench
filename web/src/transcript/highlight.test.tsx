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
