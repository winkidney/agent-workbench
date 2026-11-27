import { describe, expect, it } from "vitest";
import { runKind, runResult, runSummary, type RoutineRun } from "./when";

const run = (over: Partial<RoutineRun>) =>
  ({
    id: "r",
    routine: "x",
    name: "Check mail",
    due: "",
    started: "",
    status: "done",
    ...over,
  }) as RoutineRun;

describe("runs that take turns (ADR 0027)", () => {
  it("says what a held run waits for, and counts it as under way", () => {
    const held = run({ status: "held", behind: "Daily summary" });
    expect(runSummary(held)).toBe("Waiting for its turn, after Daily summary");
    expect(runKind(held)).toBe("active");
    expect(runSummary(run({ status: "held" }))).toBe("Waiting for its turn");
  });

  it("shows merged runs as quiet, and the run that carried them", () => {
    const merged = run({ status: "merged", mergedInto: "a" });
    expect(runKind(merged)).toBe("quiet");
    expect(runResult(merged)).toBe("Merged: sent with a later run");
    expect(runSummary(run({ status: "sent", merged: 2 }))).toBe(
      "Sent; the agent is working (with 2 more runs)",
    );
    expect(runSummary(run({ status: "sent", merged: 1 }))).toBe(
      "Sent; the agent is working (with 1 more run)",
    );
  });
});
