import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import {
  heartbeatSummary,
  runDuration,
  runKind,
  runResult,
  type RoutineRun,
} from "./when";
import { Heartbeat, runTip } from "./Heartbeat";

const run = (over: Partial<RoutineRun>): RoutineRun => ({
  id: "r",
  routine: "x",
  name: "Check",
  due: "2026-11-22T04:52:39Z",
  started: "2026-11-22T04:52:39.000Z",
  finished: "2026-11-22T04:52:43.950Z",
  status: "done",
  ...over,
});

// The runs as routine.go records them.
const quietCommand = run({
  id: "a",
  status: "skipped",
  exit: 0,
  error: "nothing printed, so nothing was sent",
});
const quietDone = run({ id: "b", exit: 0 });
const answered = run({ id: "c", session: "claude:1" });
const printed = run({ id: "d", exit: 0, output: "\n3 PRs wait\nmore" });
const failed = run({
  id: "e",
  status: "failed",
  exit: 3,
  error: "exited with 3",
});
const missed = run({
  id: "f",
  status: "skipped",
  error: "missed: the workbench was not running or the computer was asleep",
});
const running = run({ id: "g", status: "sent", finished: undefined });

describe("a routine's heartbeat", () => {
  it("sorts runs by what they amount to", () => {
    expect(runKind(quietCommand)).toBe("quiet");
    expect(runKind(quietDone)).toBe("quiet");
    expect(runKind(answered)).toBe("delivered");
    expect(runKind(printed)).toBe("delivered");
    expect(runKind(failed)).toBe("failed");
    expect(runKind(missed)).toBe("missed");
    expect(
      runKind(run({ status: "skipped", error: "Code Reviewer is turned off" })),
    ).toBe("missed");
    expect(runKind(running)).toBe("active");
  });

  it("says each in a few words", () => {
    expect(runResult(quietCommand)).toBe(
      "Quiet: printed nothing, nothing sent",
    );
    expect(runResult(quietDone)).toBe("Quiet: finished, nothing to show");
    expect(runResult(missed)).toMatch(/^Missed: missed: the workbench/);
    expect(runResult(failed)).toBe("Failed: exited with 3");
    expect(runResult(answered)).toBe("Finished; the agent answered");
    expect(runResult(printed)).toBe("Printed: 3 PRs wait");
  });

  it("counts them", () => {
    expect(heartbeatSummary([])).toBe("No runs yet");
    expect(heartbeatSummary([quietCommand])).toBe("1 run: 1 quiet");
    expect(
      heartbeatSummary([
        running,
        quietCommand,
        quietDone,
        answered,
        failed,
        missed,
      ]),
    ).toBe("6 runs: 1 running, 2 quiet, 1 sent, 1 failed, 1 missed");
  });

  it("times them", () => {
    expect(runDuration(quietCommand)).toBe("5.0 s");
    expect(runDuration(running)).toBe("");
    expect(runDuration(run({ finished: "2026-11-22T05:57:39.000Z" }))).toBe(
      "1 h 5 min",
    );
    expect(runDuration(run({ finished: "2026-11-22T04:54:39.000Z" }))).toBe(
      "2 min",
    );
  });

  it("tips a bar with its time, result, exit and first output line", () => {
    const tip = runTip(failed);
    expect(tip[0]).toMatch(/ · 5\.0 s$/);
    expect(tip.slice(1)).toEqual(["Failed: exited with 3", "Exit 3"]);
    // The first line once, in the result.
    expect(runTip(printed).slice(1)).toEqual(["Printed: 3 PRs wait"]);
    expect(runTip({ ...failed, output: "gh: HTTP 502\nmore" }).at(-1)).toBe(
      "gh: HTTP 502",
    );
    expect(runTip(quietDone)).toHaveLength(2);
  });

  it("draws a bar per run, oldest first", () => {
    const html = renderToStaticMarkup(
      <Heartbeat runs={[failed, quietCommand, answered]} selected="a" />,
    );
    const kinds = [...html.matchAll(/heartbeat-bar (\w+)/g)].map((m) => m[1]);
    expect(kinds).toEqual(["delivered", "quiet", "failed"]);
    expect(html.match(/<button/g)).toHaveLength(3);
    expect(html).toContain('aria-pressed="true"');
    expect(html).toContain('aria-label="3 runs: 1 quiet, 1 sent, 1 failed"');
  });

  it("is one button inline, or only a picture without somewhere to go", () => {
    const open = renderToStaticMarkup(
      <Heartbeat compact runs={[quietCommand, quietDone]} onOpen={() => {}} />,
    );
    expect(open.match(/<button/g)).toHaveLength(1);
    expect(open).toContain("2 runs: 2 quiet. Show the runs");
    const still = renderToStaticMarkup(
      <Heartbeat compact runs={[quietDone]} />,
    );
    expect(still).not.toContain("<button");
    expect(still).toContain('role="img"');
    expect(renderToStaticMarkup(<Heartbeat runs={[]} />)).toBe("");
  });
});
