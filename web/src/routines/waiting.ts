import { useEffect, useState } from "react";
import { api } from "../lib/api";
import { routineOwner } from "../lib/assistants";
import { underWay, type Routine, type RoutineRun } from "./when";

// A routine's run under way (when.ts underWay).
export type RunningRun = { routine: string; name: string; run: RoutineRun };

// By the assistant they belong to: runs that wait for the user's approval
// (ADR 0021), how many and the routines they come from; and the runs under
// way, which its sidebar row shows as running.
export type WaitingRuns = Record<
  string,
  { count: number; routines: string[]; running: RunningRun[] }
>;

export function waitingByAssistant(list: Routine[]): WaitingRuns {
  const out: WaitingRuns = {};
  for (const r of list) {
    const n = r.waiting?.length ?? 0;
    const run = underWay(r);
    if (!n && !run) continue;
    const w = (out[routineOwner(r)] ??= {
      count: 0,
      routines: [],
      running: [],
    });
    if (n) {
      w.count += n;
      w.routines.push(r.name);
    }
    if (run) w.running.push({ routine: r.id, name: r.name, run });
  }
  return out;
}

export const waitingTotal = (w: WaitingRuns) =>
  Object.values(w).reduce((n, x) => n + x.count, 0);

// Kept in step with the routines: read again when one changes (App.tsx
// dispatches workbench:routines for every routine event) and when the page
// comes back to the front, for events missed while disconnected.
export function useWaitingRuns(ready: boolean): WaitingRuns {
  const [waiting, setWaiting] = useState<WaitingRuns>({});
  useEffect(() => {
    if (!ready) return;
    let live = true;
    let timer = 0;
    const load = () =>
      api<Routine[]>("routines")
        .then((list) => live && setWaiting(waitingByAssistant(list)))
        .catch(() => {});
    // A run's start and end come close together: read once for both.
    const soon = () => {
      window.clearTimeout(timer);
      timer = window.setTimeout(load, 300);
    };
    void load();
    window.addEventListener("workbench:routines", soon);
    window.addEventListener("focus", soon);
    return () => {
      live = false;
      window.clearTimeout(timer);
      window.removeEventListener("workbench:routines", soon);
      window.removeEventListener("focus", soon);
    };
  }, [ready]);
  return waiting;
}
