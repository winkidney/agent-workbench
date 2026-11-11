import { useState } from "react";
import { ChevronRight, ListTodo } from "lucide-react";
import type { State } from "../transcript/reducer";
import type { Step } from "../transcript/model";

// The newest plan in the transcript (Claude TodoWrite or Codex plan updates).
export function latestPlan(state: State): Step[] | undefined {
  for (let i = state.keys.length - 1; i >= 0; i--) {
    const b = state.blocks.get(state.keys[i])!;
    if (b.steps?.length) return b.steps;
  }
  return undefined;
}

export function TodoPanel({ steps, busy }: { steps?: Step[]; busy: boolean }) {
  const [open, setOpen] = useState(true);
  if (!steps?.length) return null;
  const done = steps.filter((s) => s.status === "completed").length;
  if (done === steps.length && !busy) return null;
  const current = steps.find((s) => s.status === "in_progress");
  return (
    <div className="todo-panel">
      <button
        type="button"
        className="todo-head"
        aria-expanded={open}
        onClick={() => setOpen(!open)}
      >
        <ListTodo size={13} />
        <span>
          Todos {done}/{steps.length}
        </span>
        {!open && current && (
          <span className="todo-current">· {current.text}</span>
        )}
        <ChevronRight
          size={12}
          className={open ? "fold-chevron open" : "fold-chevron"}
        />
      </button>
      {open && (
        <ul className="steps">
          {steps.map((s, i) => (
            <li key={i} className={"step " + s.status}>
              <span className="step-box">
                {s.status === "completed"
                  ? "☒"
                  : s.status === "in_progress"
                    ? "◐"
                    : "☐"}
              </span>
              <span>{s.text}</span>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
