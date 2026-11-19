import { useCallback, useEffect, useState } from "react";
import {
  ChevronDown,
  ChevronRight,
  Pencil,
  Play,
  Plus,
  Trash2,
} from "lucide-react";
import { api } from "../../lib/api";
import {
  describeWhen,
  draftOf,
  newDraft,
  runSummary,
  shortTime,
  type Draft,
  type Routine,
  type RoutineRun,
} from "../../routines/when";
import { Button } from "../ui/button";
import { groupRoutines, type Assistant } from "../../lib/assistants";

// Every routine: what it does, when it runs next and how its last run went;
// a routine opens to its recent runs and their output.
export function RoutinesTab({
  open,
  sessionTitle,
  onEdit,
  assistants = [],
}: {
  open: boolean;
  sessionTitle: (id: string) => string;
  onEdit: (d: Draft) => void;
  // For grouping by assistant (lib/assistants.ts groupRoutines).
  assistants?: Assistant[];
}) {
  const [list, setList] = useState<Routine[] | null>(null);
  const [error, setError] = useState("");
  const [shown, setShown] = useState<string | null>(null);
  const [runs, setRuns] = useState<RoutineRun[]>([]);
  const load = useCallback(async () => {
    try {
      setList(await api<Routine[]>("routines"));
      setError("");
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  }, []);
  const loadRuns = useCallback(async (id: string) => {
    try {
      setRuns(await api<RoutineRun[]>(`routines/${id}/runs`));
    } catch {
      setRuns([]);
    }
  }, []);
  useEffect(() => {
    if (!open) return;
    void load();
    // A run started or ended, or a routine changed elsewhere (App.tsx).
    const changed = () => {
      void load();
      if (shown) void loadRuns(shown);
    };
    window.addEventListener("workbench:routines", changed);
    return () => window.removeEventListener("workbench:routines", changed);
  }, [open, load, loadRuns, shown]);
  const act = async (path: string, method: string, body?: unknown) => {
    try {
      await api(path, method, body);
      await load();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  };
  const target = (r: Routine) => {
    const t = r.target;
    if (!t) return "Keeps the output";
    if (t.kind === "session") return "To " + sessionTitle(t.session);
    if (t.kind === "assistant")
      return t.background
        ? `In the background, as ${t.assistant}`
        : "To the assistant " + t.assistant;
    return `New ${t.agent === "claude" ? "Claude" : "Codex"} session in ${t.cwd}`;
  };
  return (
    <>
      <section className="settings-section">
        <h3>Routines</h3>
        <p className="subtle">
          Messages and commands this computer runs on a schedule, while the
          workbench runs. A run it misses is skipped.
        </p>
        <Button
          variant="outline"
          size="sm"
          onClick={() => onEdit(newDraft(undefined, ""))}
        >
          <Plus size={14} />
          New routine
        </Button>
      </section>
      {error && (
        <p className="error-text" role="alert">
          {error}
        </p>
      )}
      {list && list.length === 0 && (
        <p className="subtle">
          None yet. Schedule a message from a session's ⋯ menu, or make one
          here.
        </p>
      )}
      {groupRoutines(list ?? [], assistants).map((g) => (
        <section key={g.dir} className="routine-group" aria-label={g.name}>
          <h4 className="routine-group-title">
            {g.name} <span className="subtle">{g.items.length}</span>
          </h4>
          <ul className="routine-list">
            {g.items.map((r) => {
              const open = shown === r.id;
              return (
                <li key={r.id} className="routine-item">
                  <div className="routine-head">
                    <button
                      type="button"
                      className="routine-toggle"
                      aria-expanded={open}
                      onClick={() => {
                        setShown(open ? null : r.id);
                        if (!open) void loadRuns(r.id);
                      }}
                    >
                      {open ? (
                        <ChevronDown size={14} />
                      ) : (
                        <ChevronRight size={14} />
                      )}
                      <span className="routine-name">{r.name}</span>
                    </button>
                    <button
                      type="button"
                      role="switch"
                      aria-checked={r.enabled}
                      aria-label={r.enabled ? "Turn off" : "Turn on"}
                      className="routine-switch"
                      onClick={() =>
                        void act(`routines/${r.id}`, "PATCH", {
                          enabled: !r.enabled,
                        })
                      }
                    >
                      {r.enabled ? "On" : "Off"}
                    </button>
                  </div>
                  <p className="routine-meta">
                    {describeWhen(r.when)}
                    {r.command && (
                      <>
                        {" · "}
                        <code>{r.command.run}</code>
                      </>
                    )}
                    {" · "}
                    {target(r)}
                    {r.createdBy === "agent:cli" && " · made from a terminal"}
                    {r.createdBy.startsWith("agent:") &&
                      r.createdBy !== "agent:cli" &&
                      ` · made by the agent in ${sessionTitle(r.createdBy.slice(6))}`}
                    {r.createdBy.startsWith("file:") &&
                      ` · from ${r.createdBy.slice(5)}/assistant.toml`}
                  </p>
                  <p className="routine-meta">
                    {r.assistantOff
                      ? "Waiting: its assistant is turned off"
                      : r.enabled && r.next
                        ? `Next ${shortTime(r.next)}`
                        : "Off"}
                    {r.last &&
                      ` · Last ${shortTime(r.last.started)}: ${runSummary(r.last)}`}
                  </p>
                  {open && (
                    <div className="routine-details">
                      <div className="routine-actions">
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() =>
                            void act(`routines/${r.id}/run`, "POST").then(() =>
                              loadRuns(r.id),
                            )
                          }
                        >
                          <Play size={14} />
                          Run now
                        </Button>
                        {/* An assistant's routines change in its file. */}
                        {!r.createdBy.startsWith("file:") && (
                          <>
                            <Button
                              variant="ghost"
                              size="sm"
                              onClick={() => onEdit(draftOf(r))}
                            >
                              <Pencil size={14} />
                              Edit
                            </Button>
                            <Button
                              variant="ghost"
                              size="sm"
                              onClick={() => {
                                if (confirm(`Delete the routine “${r.name}”?`))
                                  void act(`routines/${r.id}`, "DELETE");
                              }}
                            >
                              <Trash2 size={14} />
                              Delete
                            </Button>
                          </>
                        )}
                      </div>
                      {runs.length === 0 && (
                        <p className="subtle">No runs yet.</p>
                      )}
                      <ol className="routine-runs">
                        {runs.map((run) => (
                          <li key={run.id}>
                            <span className={`routine-run ${run.status}`}>
                              {shortTime(run.started)} · {runSummary(run)}
                              {run.exit !== undefined && ` (exit ${run.exit})`}
                            </span>
                            {run.output && (
                              <details>
                                <summary>Output</summary>
                                <pre className="routine-output">
                                  {run.output}
                                </pre>
                              </details>
                            )}
                          </li>
                        ))}
                      </ol>
                    </div>
                  )}
                </li>
              );
            })}
          </ul>
        </section>
      ))}
    </>
  );
}
