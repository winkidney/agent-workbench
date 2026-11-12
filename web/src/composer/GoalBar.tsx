import { useEffect, useState } from "react";
import { ChevronRight, Target, X } from "lucide-react";
import {
  compose,
  goalDone,
  goalLabel,
  goalMeta,
  goalTone,
  type Goal,
} from "./goal";

// Goals the user closed after they ended, per browser.
const dismissKey = (session: string) => "workbench:goal-dismissed:" + session;
const goalKey = (g: Goal) => g.updated + "\n" + g.objective;
function dismissedFor(session: string): string | null {
  try {
    return localStorage.getItem(dismissKey(session));
  } catch {
    return null;
  }
}

// The session's goal above the composer, as the CLIs show it in their
// status rows, with its controls: pause and resume (Codex), edit (Codex),
// clear.
export function GoalBar({
  sessionId,
  agent,
  goal,
  running,
  readOnly,
  onCommand,
  onSend,
  onInterrupt,
}: {
  sessionId: string;
  agent: string;
  goal: Goal | null;
  running: boolean;
  readOnly: boolean;
  onCommand: (name: string, args: string) => Promise<unknown>;
  onSend: (text: string, how: "send") => Promise<unknown>;
  onInterrupt: () => void;
}) {
  const [open, setOpen] = useState(false);
  const [dismissed, setDismissed] = useState(() => dismissedFor(sessionId));
  // Claude takes /goal clear between turns: stop the turn, then clear.
  const [clearAfterStop, setClearAfterStop] = useState(false);
  useEffect(() => {
    setDismissed(dismissedFor(sessionId));
    setClearAfterStop(false);
  }, [sessionId]);
  useEffect(() => {
    if (!clearAfterStop || running) return;
    setClearAfterStop(false);
    void onSend("/goal clear", "send").catch(() => {});
  }, [clearAfterStop, running]);
  if (!goal) return null;
  const done = goalDone(goal.status);
  if (done && dismissed === goalKey(goal)) return null;
  const codex = agent === "codex";
  const meta = goalMeta(goal);
  const run = (args: string) => void onCommand("goal", args).catch(() => {});
  const clear = () => {
    if (codex) return run("clear");
    if (running) {
      setClearAfterStop(true);
      onInterrupt();
    } else void onSend("/goal clear", "send").catch(() => {});
  };
  const dismiss = () => {
    try {
      localStorage.setItem(dismissKey(sessionId), goalKey(goal));
    } catch {
      // Hidden for this page view only.
    }
    setDismissed(goalKey(goal));
  };
  const resumable =
    codex && !done && goal.status !== "active" && goal.status !== "complete";
  return (
    <div
      className={"goal-bar " + goalTone(goal.status)}
      data-status={goal.status}
    >
      <div className="goal-row">
        <button
          type="button"
          className="goal-head"
          aria-expanded={open}
          title={open ? "Hide details" : goal.objective}
          onClick={() => setOpen(!open)}
        >
          <Target
            size={13}
            className={
              "goal-glyph" +
              (running && goal.status === "active" ? " live" : "")
            }
            aria-hidden
          />
          <span className="goal-label">{goalLabel(agent, goal.status)}</span>
          {!open && <span className="goal-objective">{goal.objective}</span>}
          {meta && <span className="goal-meta">{meta}</span>}
          <ChevronRight
            size={12}
            className={open ? "fold-chevron open" : "fold-chevron"}
          />
        </button>
        {!readOnly && (
          <div className="goal-actions">
            {codex && goal.status === "active" && (
              <button
                type="button"
                className="goal-action"
                onClick={() => run("pause")}
              >
                Pause
              </button>
            )}
            {resumable && (
              <button
                type="button"
                className="goal-action"
                onClick={() => run("resume")}
              >
                Resume
              </button>
            )}
            {codex && !done && (
              <button
                type="button"
                className="goal-action"
                onClick={() =>
                  compose(sessionId, `/goal edit ${goal.objective}`)
                }
              >
                Edit
              </button>
            )}
            {!done && (
              <button
                type="button"
                className="goal-action"
                disabled={clearAfterStop}
                title={
                  !codex && running
                    ? "Stop this turn, then clear the goal"
                    : "Clear the goal"
                }
                onClick={clear}
              >
                {!codex && running ? "Stop & clear" : "Clear"}
              </button>
            )}
            {done && (
              <button
                type="button"
                className="goal-action icon"
                aria-label="Close"
                title="Close"
                onClick={dismiss}
              >
                <X size={12} />
              </button>
            )}
          </div>
        )}
      </div>
      {open && (
        <div className="goal-detail">
          <p className="goal-text">{goal.objective}</p>
          {meta && <p className="goal-reason">{meta}</p>}
          {goal.lastReason && (
            <p className="goal-reason">
              <span>Last check</span> {goal.lastReason}
            </p>
          )}
        </div>
      )}
    </div>
  );
}
