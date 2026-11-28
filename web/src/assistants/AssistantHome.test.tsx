import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import {
  AssistantHome,
  HeldCard,
  ReportCard,
  RunningNow,
  WaitingCard,
  WaitingSession,
  previewFilePath,
  routineOfSession,
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
    // Under one heading that counts them, as the sidebar's bell does.
    expect(html).toContain('aria-label="Waiting for you"');
    expect(html).toContain('<span class="assistant-home-count">2</span>');
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

describe("where what waits comes from, and what runs", () => {
  const now = Date.parse("2026-11-28T10:00:00Z");
  const at = (status: string, session?: string, ago = 125_000) => ({
    id: status + (session ?? ""),
    status,
    session,
    started: new Date(now - ago).toISOString(),
  });
  const routines = [
    { id: "sum", name: "Daily summary", last: at("done", "claude:q") },
    { id: "mail", name: "Check mail", recent: [at("sent")] },
    { id: "bili", name: "Comments", last: at("sent", "claude:b", 40_000) },
    { id: "quiet", name: "Quiet", last: at("done") },
  ] as unknown as Routine[];

  it("names the routine a session came from, else the session", () => {
    expect(routineOfSession(routines, "claude:q")).toBe("Daily summary");
    expect(routineOfSession(routines, "claude:mine")).toBeUndefined();
    const s = {
      id: "claude:q",
      title: "[Routine: Daily summary] Yesterday",
      attention: "question",
      updated: new Date(now - 12 * 60_000).toISOString(),
    } as Session;
    const html = renderToStaticMarkup(
      <WaitingSession
        session={s}
        from="Daily summary"
        now={now}
        onOpen={() => {}}
      />,
    );
    expect(html).toContain(
      "<strong>Daily summary</strong> · Has a question for you · 12m ago",
    );
    expect(
      renderToStaticMarkup(
        <WaitingSession
          session={{ ...s, title: "Mine" }}
          now={now}
          onOpen={() => {}}
        />,
      ),
    ).toContain("<strong>Mine</strong>");
  });

  it("lists the runs under way, with Open or their runs", () => {
    const html = renderToStaticMarkup(
      <RunningNow routines={routines} now={now} onOpenSession={() => {}} />,
    );
    expect(html).toContain(
      "<strong>Check mail</strong> · working in the background · 2 min",
    );
    expect(html).toContain(
      "<strong>Comments</strong> · working in a conversation · 40 s",
    );
    expect(html).not.toContain("Daily summary");
    expect(html.match(/>Open<\/button>/g)).toHaveLength(1);
    expect(html.match(/>Runs<\/button>/g)).toHaveLength(1);
    expect(
      renderToStaticMarkup(
        <RunningNow
          routines={[routines[3]]}
          now={now}
          onOpenSession={() => {}}
        />,
      ),
    ).toBe("");
  });
});

describe("a routine's report for the user", () => {
  const withReport = (session?: string) =>
    ({
      id: "d",
      name: "Daily check",
      report: {
        id: "x",
        status: "done",
        finished: new Date(Date.now() - 5 * 60_000).toISOString(),
        report: "Due today:\n\n- **renew** the domain",
        forYou: true,
        session,
      },
    }) as unknown as Routine;
  it("shows the reply itself, with Open and Dismiss", () => {
    const html = renderToStaticMarkup(
      <ReportCard
        routine={withReport("claude:s1")}
        now={Date.now()}
        onSee={() => {}}
      />,
    );
    expect(html).toContain("<strong>Daily check</strong> · report · 5m ago");
    expect(html).toContain("<strong>renew</strong>");
    expect(html).toContain(">Open<");
    expect(html).toContain(">Dismiss<");
  });
  it("offers no Open without a session, and is nothing without a report", () => {
    const html = renderToStaticMarkup(
      <ReportCard routine={withReport()} now={Date.now()} onSee={() => {}} />,
    );
    expect(html).not.toContain(">Open<");
    expect(
      renderToStaticMarkup(
        <ReportCard
          routine={{ id: "q", name: "Quiet" } as Routine}
          now={Date.now()}
          onSee={() => {}}
        />,
      ),
    ).toBe("");
  });
});
