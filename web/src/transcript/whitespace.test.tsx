import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { Markdown } from "./Markdown";

const render = (text: string) => renderToStaticMarkup(<Markdown text={text} />);

describe("whitespace between blocks", () => {
  it("leaves no newlines around nested lists", () => {
    const html = render(
      [
        "- **One pool.** daemon reads three variables",
        "  - config.rs has a `PoolConfig`",
        "- **Scheduler**",
        "  - first",
        "  - second",
        "",
        "Next paragraph",
      ].join("\n"),
    );
    expect(html).not.toMatch(/>\s*\n\s*</);
    expect(html).toContain("<li>first</li><li>second</li>");
  });

  it("leaves no newlines between loose list items", () => {
    const html = render("- a\n\n- b\n\n  - c\n\n  - d\n");
    expect(html).not.toMatch(/>\s*\n\s*</);
    expect(html).toContain("<li><p>c</p></li><li><p>d</p></li>");
  });

  it("keeps line breaks inside text and between inline elements", () => {
    const html = render("- line one\n  line two\n- **a**\n  **b**");
    expect(html).toContain("line one\nline two");
    expect(html).toContain("<strong>a</strong>\n<strong>b</strong>");
  });

  it("keeps code blocks as written", () => {
    const html = render("```\na\n\nb\n```");
    expect(html).toContain("a\n\nb");
  });
});
