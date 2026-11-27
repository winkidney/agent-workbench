import { useEffect, useRef, useState } from "react";
import { Loader2, Play } from "lucide-react";
import { api } from "../lib/api";
import { Button } from "../components/ui/button";
import type { RoutineRun } from "./when";

// How long the note after a click stays.
const NOTE_MS = 4000;

export type RunNowNote = { text: string; failed: boolean };

// What a click on Run now came to: the run starts in the background
// (routine.go startRun), or is skipped while the previous one still runs.
export function runNowNote(run: RoutineRun): RunNowNote {
  if (run.status === "skipped")
    return { text: `Skipped: ${run.error || "not run"}`, failed: true };
  if (run.status === "failed")
    return { text: `Failed: ${run.error || "see its runs"}`, failed: true };
  return { text: "Started", failed: false };
}

// Run now with a light note beside it, so a click is never silent.
export function RunNowButton({
  id,
  disabled,
  title,
  iconSize = 14,
  onStarted,
}: {
  id: string;
  disabled?: boolean;
  // Why it is disabled.
  title?: string;
  iconSize?: number;
  onStarted?: (run: RoutineRun) => void;
}) {
  const [busy, setBusy] = useState(false);
  const [note, setNote] = useState<RunNowNote | null>(null);
  const timer = useRef<number>(undefined);
  useEffect(() => () => window.clearTimeout(timer.current), []);
  const show = (n: RunNowNote) => {
    setNote(n);
    window.clearTimeout(timer.current);
    timer.current = window.setTimeout(() => setNote(null), NOTE_MS);
  };
  const run = async () => {
    setBusy(true);
    try {
      const r = await api<RoutineRun>(`routines/${id}/run`, "POST");
      show(runNowNote(r));
      onStarted?.(r);
    } catch (e) {
      show({ text: e instanceof Error ? e.message : String(e), failed: true });
    } finally {
      setBusy(false);
    }
  };
  return (
    <div className="run-now">
      <Button
        type="button"
        variant="ghost"
        size="sm"
        disabled={disabled || busy}
        title={title}
        onClick={() => void run()}
      >
        {busy ? (
          <Loader2 size={iconSize} className="spin" />
        ) : (
          <Play size={iconSize} />
        )}
        Run now
      </Button>
      <span
        className={`run-now-note${note?.failed ? " failed" : ""}`}
        role="status"
      >
        {note?.text}
      </span>
    </div>
  );
}
