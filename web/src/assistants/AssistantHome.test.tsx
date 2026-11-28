import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import {
  AssistantHome,
  HeldCard,
  WaitingCard,
  previewFilePath,
} from "./AssistantHome";
import type { Assistant } from "../lib/assistants";
import type { Session } from "../sidebar/tree";
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

describe("a routine whose runs wait for their turn (ADR 0027)", () => {
  const held = (n: number, behindSession?: string) =>
    ({
      id: "r1",
      name: "Daily summary",
      held: Array.from({ length: n }, (_, i) => ({
        id: `h${i}`,
        status: "held",
        behind: "Check mail",
        behindSession,
      })),
    }) as unknown as Routine;
  const render = (r: Routine) =>
    renderToStaticMarkup(
      <HeldCard routine={r} deciding={false} onAct={() => {}} />,
    );

  it("says what it waits for, with Start now and Dismiss", () => {
    const html = render(held(1, "claude:s1"));
    expect(html).toContain("Daily summary: 1 run waits for its turn");
    expect(html).toMatch(/class="held-behind"[^>]*>Check mail<\/button>/);
    expect(html).toContain("Start now");
    expect(html).toContain("Dismiss");
  });

  it("says several go as one message, and shows nothing without runs", () => {
    expect(render(held(3))).toContain(
      "3 runs wait for its turn, after Check mail. It starts when that is done, as one message.",
    );
    expect(render(held(0))).toBe("");
  });
});

describe("an assistant's card", () => {
  const mail = {
    dir: "mail",
    path: "/home/u/assistants/mail",
    name: "Mail",
    agent: "claude",
    sessions: "per-request",
    ready: true,
  } as unknown as Assistant;
  const waiting = [
    {
      id: "claude:q",
      agent: "claude",
      title: "[Routine: Daily summary] Yesterday",
      attention: "question",
    },
    {
      id: "claude:a",
      agent: "claude",
      title: "Check mail",
      attention: "approval",
    },
  ] as unknown as Session[];

  it("names the sessions that wait for the user, each with Open", () => {
    const html = renderToStaticMarkup(
      <AssistantHome
        assistant={mail}
        onTry={() => {}}
        waiting={waiting}
        onOpenSession={() => {}}
      />,
    );
    expect(html).toContain("Has a question for you");
    expect(html).toContain("[Routine: Daily summary] Yesterday");
    expect(html).toContain("Needs your approval");
    expect(html.match(/>Open<\/button>/g)).toHaveLength(2);
  });

  it("says nothing of them when none waits", () => {
    for (const props of [{ waiting: [] }, {}])
      expect(
        renderToStaticMarkup(
          <AssistantHome assistant={mail} onTry={() => {}} {...props} />,
        ),
      ).not.toContain(">Open</button>");
  });
});
