import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { AssistantIcon, ICONS, iconOf } from "./AssistantIcon";
import { ICON_COLORS, ICON_DRAWINGS } from "./assistantIcons";
import { centreSquare } from "../assistants/iconUpload";

describe("assistant icons", () => {
  it("has twelve drawings at 16 and 32, in their palette", () => {
    expect(ICONS.map((i) => i.name)).toEqual([
      "camera",
      "mail",
      "review",
      "video",
      "calendar",
      "bell",
      "globe",
      "chart",
      "book",
      "chat",
      "doc",
      "toolbox",
    ]);
    for (const [name, d] of Object.entries(ICON_DRAWINGS)) {
      for (const [n, grid] of [
        [16, d.g16],
        [32, d.g32],
      ] as const) {
        expect(grid, name).toHaveLength(n);
        for (const row of grid) {
          expect(row, name).toHaveLength(n);
          for (const ch of row)
            expect(ch === "." || ch in ICON_COLORS, `${name} ${ch}`).toBe(true);
        }
      }
      // The sidebar's drawings keep within the middle 14×14 rows and
      // columns, as its other icons do.
      expect(d.g16[0].trim().replace(/\./g, ""), name).toBe("");
      expect(d.g16[15].replace(/\./g, ""), name).toBe("");
    }
  });

  it("names the assistant and its agent, with the agent as a badge", () => {
    const big = renderToStaticMarkup(
      <AssistantIcon icon="mail" agent="claude" size={64} name="Mail" />,
    );
    expect(big).toContain('aria-label="Mail · Claude"');
    expect(big).toContain("assistant-icon-mark");
    // The agent's mark in the badge.
    expect(big).toContain('class="assistant-badge claude"');
    expect(big).toContain("agent-icon claude");
    const small = renderToStaticMarkup(
      <AssistantIcon icon="mail" agent="codex" size={16} name="Mail" />,
    );
    expect(small).toContain("assistant-icon-dot");
    expect(small).toContain('class="assistant-badge codex"');
    expect(small).not.toContain("agent-icon");
    expect(small).toContain('width="16"');
    const plain = renderToStaticMarkup(
      <AssistantIcon
        icon="mail"
        agent="codex"
        size={32}
        badge={false}
        decorative
      />,
    );
    expect(plain).not.toContain("assistant-badge");
    expect(plain).toContain('aria-hidden="true"');
  });

  it("shows an uploaded image square, and the camera by default", () => {
    const img = renderToStaticMarkup(
      <AssistantIcon
        {...iconOf({ dir: "mail box", icon: "file", iconVersion: 7 })}
        agent="claude"
        size={64}
        name="Mail"
      />,
    );
    expect(img).toContain('src="/api/assistants/mail%20box/icon?v=7"');
    expect(img).toContain('width="64" height="64"');
    const camera = renderToStaticMarkup(
      <AssistantIcon {...iconOf({ dir: "main" })} agent="codex" size={16} />,
    );
    expect(camera).toContain("<svg");
    // The same blank-screened camera for both agents: only the badge differs.
    const claude = renderToStaticMarkup(
      <AssistantIcon {...iconOf({ dir: "main" })} agent="claude" size={16} />,
    );
    const drawing = (html: string) =>
      html.slice(html.indexOf("<svg"), html.indexOf("</svg>"));
    expect(drawing(claude)).toBe(drawing(camera));
    expect(drawing(camera)).not.toContain("var(--agent-claude)");
    expect(iconOf({ dir: "x", icon: "bell" })).toEqual({ icon: "bell" });
  });

  it("cuts a picture to the square in its middle", () => {
    expect(centreSquare(400, 300)).toEqual({ x: 50, y: 0, side: 300 });
    expect(centreSquare(300, 401)).toEqual({ x: 0, y: 50, side: 300 });
    expect(centreSquare(128, 128)).toEqual({ x: 0, y: 0, side: 128 });
  });
});
