import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { WaitingCard, previewFilePath } from "./AssistantHome";
import type { Routine } from "../routines/when";

const routine = (preview?: string) =>
  ({
    id: "r1",
    name: "Check mail",
    waiting: [{ id: "run1", output: "2 new mail (preview: raw/preview.md):" }],
    preview,
  }) as unknown as Routine;

const card = (r: Routine) =>
  renderToStaticMarkup(
    <WaitingCard routine={r} deciding={false} onDecide={() => {}} />,
  );

describe("a routine waiting for approval (ADR 0023)", () => {
  it("offers its preview and waits for it before Approve", () => {
    const html = card(routine("/home/me/mail/raw/preview.md"));
    expect(html).toContain("Preview");
    expect(html).toContain("Open the preview first");
    expect(html).toMatch(/<button[^>]*disabled=""[^>]*>Approve<\/button>/);
    // Dismiss sends nothing, so it never waits.
    expect(html).not.toMatch(/<button[^>]*disabled=""[^>]*>Dismiss<\/button>/);
  });

  it("approves at once without a preview", () => {
    const html = card(routine());
    expect(html).not.toContain("Preview");
    expect(html).not.toMatch(/<button[^>]*disabled=""[^>]*>Approve<\/button>/);
  });

  it("opens only relative Markdown files beside the preview (ADR 0026)", () => {
    expect(previewFilePath("mail/Check mail", "2026-11-27/abc.md")).toBe(
      "routines/mail%2FCheck%20mail/preview?file=2026-11-27%2Fabc.md",
    );
    expect(previewFilePath("r1", "./a.md")).toBe(
      "routines/r1/preview?file=a.md",
    );
    for (const path of [
      "",
      "/etc/passwd.md",
      "https://evil.example/a.md",
      "file:///home/me/a.md",
      "notes.txt",
      "2026-11-27",
    ])
      expect(previewFilePath("r1", path)).toBeNull();
  });
});
