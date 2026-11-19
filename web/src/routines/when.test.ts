import { describe, expect, it } from "vitest";
import {
  describeWhen,
  draftOf,
  newDraft,
  routineLabel,
  routineOf,
  shortTime,
  type Routine,
} from "./when";

describe("routines", () => {
  it("describes schedules in words", () => {
    expect(describeWhen({ kind: "daily", time: "09:00" })).toBe(
      "Daily at 09:00",
    );
    expect(
      describeWhen({ kind: "daily", time: "09:00", days: [5, 1, 2, 3, 4] }),
    ).toBe("Weekdays at 09:00");
    expect(describeWhen({ kind: "daily", time: "17:00", days: [5, 1] })).toBe(
      "Mon, Fri at 17:00",
    );
    expect(describeWhen({ kind: "interval", minutes: 120 })).toBe(
      "Every 2 hours",
    );
    expect(describeWhen({ kind: "interval", minutes: 30 })).toBe(
      "Every 30 minutes",
    );
    expect(describeWhen({ kind: "interval", minutes: 1440 })).toBe("Every day");
  });

  it("shows nearby times without the date", () => {
    const now = new Date(2026, 10, 19, 10, 0);
    expect(shortTime(new Date(2026, 10, 19, 17, 5).toISOString(), now)).toBe(
      "today 17:05",
    );
    expect(shortTime(new Date(2026, 10, 20, 8, 0).toISOString(), now)).toBe(
      "tomorrow 08:00",
    );
    expect(shortTime(new Date(2026, 11, 1, 8, 0).toISOString(), now)).toBe(
      "2026-12-01 08:00",
    );
  });

  it("turns the form into a routine, or says what is missing", () => {
    const d = newDraft("codex:t");
    expect(routineOf(d)).toEqual({ error: "Write the message to send." });
    d.message = "Check the inbox";
    const ok = routineOf(d);
    expect("routine" in ok && ok.routine.target).toEqual({
      kind: "session",
      session: "codex:t",
    });
    // A command alone sends nothing.
    const c = newDraft(undefined, "/work");
    c.what = "command";
    c.run = "./check.sh";
    c.whenKind = "interval";
    c.every = 2;
    c.unit = "minutes";
    expect(routineOf(c)).toEqual({ error: "Run it at most every 5 minutes." });
    c.every = 30;
    const cmd = routineOf(c);
    expect("routine" in cmd && cmd.routine).toMatchObject({
      target: undefined,
      command: { run: "./check.sh", dir: "/work" },
      when: { kind: "interval", minutes: 30 },
      message: "",
    });
  });

  it("edits a saved routine with the same values", () => {
    const r: Routine = {
      id: "r1",
      name: "Mail",
      enabled: true,
      when: { kind: "interval", minutes: 180 },
      command: { run: "uv run fetch.py", dir: "/a", timeoutMin: 5 },
      target: { kind: "new", agent: "codex", cwd: "/a" },
      message: "Sort these",
      createdBy: "page",
      created: "",
    };
    const d = draftOf(r);
    expect(d).toMatchObject({
      what: "command",
      handOff: true,
      target: "new",
      agent: "codex",
      every: 3,
      unit: "hours",
    });
    const back = routineOf(d);
    expect("routine" in back && back.routine).toMatchObject({
      when: r.when,
      target: r.target,
      command: { run: "uv run fetch.py", dir: "/a", timeoutMin: 5 },
      message: "Sort these",
    });
  });

  it("finds the label a routine put on its message", () => {
    expect(routineLabel("[Routine: Morning]\n\nCheck mail")).toEqual({
      kind: "Routine",
      name: "Morning",
      rest: "Check mail",
    });
    expect(routineLabel("[Workbench: Welcome]\n\nGreet them")).toEqual({
      kind: "Workbench",
      name: "Welcome",
      rest: "Greet them",
    });
    expect(routineLabel("[Routine: x] typed by hand")).toBeUndefined();
  });
});

describe("routines to assistants", () => {
  it("sends to an assistant by its folder", () => {
    const d = newDraft();
    d.message = "Draft the report";
    d.target = "assistant";
    expect(routineOf(d)).toEqual({ error: "Choose an assistant." });
    d.assistant = "report";
    const out = routineOf(d);
    expect("routine" in out && out.routine.target).toEqual({
      kind: "assistant",
      assistant: "report",
    });
    expect(
      draftOf({
        id: "x",
        name: "x",
        enabled: true,
        when: { kind: "daily", time: "09:00" },
        target: { kind: "assistant", assistant: "report" },
        message: "m",
        createdBy: "page",
        created: "",
      }),
    ).toMatchObject({ target: "assistant", assistant: "report" });
  });
});
