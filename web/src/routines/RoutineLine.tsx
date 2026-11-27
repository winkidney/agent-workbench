import { useState, type ReactNode } from "react";
import { api } from "../lib/api";
import { describeWhen, routineState, type Routine } from "./when";
import { Heartbeat } from "./Heartbeat";
import { RunNowButton } from "./RunNowButton";

// A routine as every list shows it (Settings → Routines, an assistant's card
// and its settings): its name with the switch and Run now, then when it runs,
// whether it runs next, how its last run went and its heartbeat. The same
// controls everywhere, so a routine runs with one click wherever it is seen.
export function RoutineLine({
  routine: r,
  head,
  onChanged,
  onOpenRuns,
  heartbeat = true,
}: {
  routine: Routine;
  // In place of the name: Settings → Routines puts its expand button here.
  head?: ReactNode;
  // It was switched or run: read the routines again.
  onChanged: () => void;
  // The heartbeat was clicked: show all its runs.
  onOpenRuns?: () => void;
  // Off while all its runs show below it (an open routine in Settings).
  heartbeat?: boolean;
}) {
  const [error, setError] = useState("");
  const toggle = async () => {
    setError("");
    try {
      await api(`routines/${r.id}`, "PATCH", { enabled: !r.enabled });
      onChanged();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  };
  const state = routineState(r);
  return (
    <div className="routine-line">
      <div className="routine-line-head">
        {head ?? <span className="routine-name">{r.name}</span>}
        <div className="routine-line-controls">
          <button
            type="button"
            role="switch"
            aria-checked={r.enabled}
            aria-label={`${r.name}: ${r.enabled ? "turn off" : "turn on"}`}
            className="routine-switch"
            onClick={() => void toggle()}
          >
            {r.enabled ? "On" : "Off"}
          </button>
          <RunNowButton
            id={r.id}
            iconSize={13}
            // The server refuses it too (RunRoutine in routine.go).
            disabled={r.assistantOff}
            title={r.assistantOff ? "Its assistant is turned off" : undefined}
            onStarted={onChanged}
          />
        </div>
      </div>
      <div className="routine-line-meta">
        <span>{describeWhen(r.when)}</span>
        {state && <span>{state}</span>}
        {r.last?.status === "failed" && (
          <span className="routine-last-failed">Last run failed</span>
        )}
        {heartbeat && (
          <Heartbeat compact runs={r.recent ?? []} onOpen={onOpenRuns} />
        )}
      </div>
      {error && <p className="error-text">{error}</p>}
    </div>
  );
}
