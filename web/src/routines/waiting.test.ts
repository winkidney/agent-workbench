import { describe, expect, it } from "vitest";
import { waitingByAssistant, waitingTotal } from "./waiting";
import type { Routine, RoutineRun } from "./when";

const run = (id: string) => ({ id, status: "waiting" }) as RoutineRun;
const routine = (r: Partial<Routine>) =>
  ({ id: "r", name: "r", enabled: true, createdBy: "user", ...r }) as Routine;

describe("runs waiting for approval", () => {
  it("counts them by the assistant they belong to", () => {
    const w = waitingByAssistant([
      routine({
        name: "Check mail",
        createdBy: "file:mail",
        waiting: [run("1"), run("2")],
      }),
      routine({ name: "Quiet", createdBy: "file:mail" }),
      routine({
        name: "Sent to it",
        target: { kind: "assistant", assistant: "news" },
        waiting: [run("3")],
      }),
      routine({ name: "Moss's", waiting: [run("4")] }),
      routine({ name: "Empty", createdBy: "file:news", waiting: [] }),
    ]);
    expect(w).toEqual({
      mail: { count: 2, routines: ["Check mail"], reports: [], running: [] },
      news: { count: 1, routines: ["Sent to it"], reports: [], running: [] },
      main: { count: 1, routines: ["Moss's"], reports: [], running: [] },
    });
    expect(waitingTotal(w)).toBe(4);
    expect(waitingTotal({})).toBe(0);
  });
  it("names the runs under way, in the background too", () => {
    const at = (status: RoutineRun["status"], session?: string) =>
      ({ id: status, status, started: "", session }) as RoutineRun;
    const w = waitingByAssistant([
      routine({
        id: "a",
        name: "Check mail",
        createdBy: "file:mail",
        last: at("sent"),
      }),
      routine({
        id: "b",
        name: "Summary",
        createdBy: "file:mail",
        recent: [at("queued", "s1")],
      }),
      routine({
        id: "c",
        name: "Done",
        createdBy: "file:mail",
        last: at("done"),
      }),
      routine({
        id: "d",
        name: "Held",
        createdBy: "file:news",
        last: at("held"),
      }),
    ]);
    expect(w).toEqual({
      mail: {
        count: 0,
        routines: [],
        reports: [],
        running: [
          { routine: "a", name: "Check mail", run: at("sent") },
          { routine: "b", name: "Summary", run: at("queued", "s1") },
        ],
      },
    });
    expect(waitingTotal(w)).toBe(0);
  });
  it("counts the reports not yet seen in the bell's total", () => {
    const report = {
      id: "x",
      status: "done",
      report: "Two loops are due.",
      forYou: true,
      session: "s1",
    } as RoutineRun;
    const w = waitingByAssistant([
      routine({ id: "d", name: "Daily check", report }),
      routine({
        id: "m",
        name: "Check mail",
        createdBy: "file:mail",
        waiting: [run("1")],
      }),
    ]);
    expect(w.main).toEqual({
      count: 0,
      routines: [],
      reports: [{ routine: "d", name: "Daily check", run: report }],
      running: [],
    });
    expect(waitingTotal(w)).toBe(2);
  });
});
