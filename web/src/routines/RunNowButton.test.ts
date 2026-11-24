import { describe, expect, it } from "vitest";
import { runNowNote } from "./RunNowButton";
import type { RoutineRun } from "./when";

const run = (over: Partial<RoutineRun>): RoutineRun => ({
  id: "r",
  routine: "x",
  name: "Check",
  due: "2026-11-24T04:00:00Z",
  started: "2026-11-24T04:00:00Z",
  status: "running",
  ...over,
});

describe("runNowNote", () => {
  it("says a started run started", () => {
    expect(runNowNote(run({}))).toEqual({ text: "Started", failed: false });
    expect(runNowNote(run({ status: "sent" })).text).toBe("Started");
  });
  it("says why a run was skipped", () => {
    expect(
      runNowNote(
        run({
          status: "skipped",
          error: "the previous run is still running",
        }),
      ),
    ).toEqual({
      text: "Skipped: the previous run is still running",
      failed: true,
    });
  });
});
