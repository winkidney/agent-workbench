import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { WaitingCard } from "./AssistantHome";
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
});
