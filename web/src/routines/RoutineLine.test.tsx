import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { RoutineLine } from "./RoutineLine";
import type { Routine } from "./when";

const routine = (over: Partial<Routine> = {}) =>
  ({
    id: "r1",
    name: "Daily report",
    enabled: true,
    when: { kind: "daily", time: "10:00" },
    createdBy: "file:bilibili",
    created: "",
    recent: [
      {
        id: "run1",
        routine: "r1",
        started: "2026-11-27T02:00:00Z",
        status: "sent",
      },
    ],
    ...over,
  }) as Routine;

const html = (r: Routine, heartbeat?: boolean) =>
  renderToStaticMarkup(
    <RoutineLine routine={r} onChanged={() => {}} heartbeat={heartbeat} />,
  );

describe("RoutineLine", () => {
  it("has the switch and Run now, its schedule and its heartbeat", () => {
    const out = html(routine());
    expect(out).toContain('role="switch"');
    expect(out).toContain('aria-checked="true"');
    expect(out).toContain("Run now");
    expect(out).toContain("Daily at 10:00");
    expect(out).toContain("heartbeat");
    expect(out).not.toContain('disabled=""');
  });
  it("cannot run while its assistant is turned off, and says why", () => {
    const out = html(routine({ assistantOff: true }));
    expect(out).toMatch(
      /disabled=""[^>]*title="Its assistant is turned off"|title="Its assistant is turned off"[^>]*disabled=""/,
    );
    expect(out).toContain("Waiting: its assistant is turned off");
  });
  it("leaves the heartbeat out when asked", () => {
    expect(html(routine(), false)).not.toContain("heartbeat");
  });
  it("says a last run failed", () => {
    expect(
      html(routine({ last: { status: "failed" } as Routine["last"] })),
    ).toContain("Last run failed");
  });
});
