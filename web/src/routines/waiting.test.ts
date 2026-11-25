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
      mail: { count: 2, routines: ["Check mail"] },
      news: { count: 1, routines: ["Sent to it"] },
      main: { count: 1, routines: ["Moss's"] },
    });
    expect(waitingTotal(w)).toBe(4);
    expect(waitingTotal({})).toBe(0);
  });
});
