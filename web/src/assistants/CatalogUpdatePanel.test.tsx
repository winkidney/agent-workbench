import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { CatalogUpdatePanel } from "./CatalogUpdatePanel";
import type { Assistant } from "../lib/assistants";

const a = (update?: object) =>
  ({
    dir: "mail",
    path: "/h/assistants/mail",
    name: "Mail",
    agent: "claude",
    catalog: { url: "u", path: "mail", commit: "aaaaaaaa", update },
  }) as unknown as Assistant;

describe("an assistant's update from its catalog", () => {
  it("shows what it would do, what runs, and asks about conflicts", () => {
    const html = renderToStaticMarkup(
      <CatalogUpdatePanel
        assistant={a({
          dir: "mail",
          catalog: "u",
          from: "aaaaaaa1",
          commit: "bbbbbbb2",
          files: [
            { rel: "AGENTS.md", result: "update" },
            { rel: "tools/fetch.py", result: "merge", runs: true },
            {
              rel: ".agents/skills/x/SKILL.md",
              result: "conflict",
              theirs: "/d/x.theirs",
            },
          ],
        })}
      />,
    );
    expect(html).toContain("Update available (3)");
    expect(html).toContain("the catalog&#x27;s change merged into yours");
    expect(html).toContain(
      "What runs on this computer changes: tools/fetch.py",
    );
    expect(html).toContain("Keep mine");
    expect(html).toContain("Use the catalog&#x27;s");
    expect(html).toContain("Ask Moss to merge");
    expect(html.match(/type="radio"/g)).toHaveLength(2);
  });
  it("offers a check when nothing is known, and nothing without a catalog", () => {
    expect(
      renderToStaticMarkup(<CatalogUpdatePanel assistant={a()} />),
    ).toContain("Check for updates");
    const own = { ...a(), catalog: undefined } as Assistant;
    expect(renderToStaticMarkup(<CatalogUpdatePanel assistant={own} />)).toBe(
      "",
    );
  });
});
