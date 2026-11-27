import { useCallback, useEffect, useState } from "react";
import { ChevronDown, ChevronRight, Pencil, Plus, Trash2 } from "lucide-react";
import { api } from "../../lib/api";
import {
  draftOf,
  heartbeatSummary,
  newDraft,
  runDuration,
  runKind,
  runResult,
  shortTime,
  type Draft,
  type Routine,
  type RoutineRun,
} from "../../routines/when";
import { Heartbeat } from "../../routines/Heartbeat";
import { RoutineLine } from "../../routines/RoutineLine";
import { Button } from "../ui/button";
import { groupRoutines, type Assistant } from "../../lib/assistants";

// Every routine: what it does, when it runs next and its heartbeat (its
// latest runs as bars). A routine opens to all its kept runs: the heartbeat,
// the runs worth a look (it sent something or failed) and, for the run
// picked, its output. Quiet runs show only as bars.
export function RoutinesTab({
  open,
  sessionTitle,
  onEdit,
  onOpenSession,
  assistants = [],
  focus,
}: {
  open: boolean;
  sessionTitle: (id: string) => string;
  onEdit: (d: Draft) => void;
  // A run's conversation was asked for.
  onOpenSession?: (id: string) => void;
  // For grouping by assistant (lib/assistants.ts groupRoutines).
  assistants?: Assistant[];
  // A routine to open on its runs (routines/open.ts).
  focus?: string;
}) {
  const [list, setList] = useState<Routine[] | null>(null);
  const [error, setError] = useState("");
  const [shown, setShown] = useState<string | null>(null);
  const [runs, setRuns] = useState<RoutineRun[]>([]);
  // The run whose details show, in the open routine.
  const [picked, setPicked] = useState<string | null>(null);
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
  // Who made it, when not the user in this page.
  const made = (r: Routine) => {
    const by = r.createdBy;
    if (by === "agent:cli") return "made from a terminal";
    if (by.startsWith("agent:"))
      return `made by the agent in ${sessionTitle(by.slice(6))}`;
    if (by.startsWith("file:")) return `from ${by.slice(5)}/assistant.toml`;
    return "";
  };
  const expand = (id: string) => {
    setShown(id);
    setPicked(null);
    void loadRuns(id);
  };
  const collapse = () => {
    setShown(null);
    setPicked(null);
  };
  // Opened on one routine (routines/open.ts): it opens on its runs, in view.
  const focused = useCallback((el: HTMLLIElement | null) => {
    el?.scrollIntoView({ block: "nearest" });
  }, []);
  useEffect(() => {
    if (open && focus) expand(focus);
  }, [open, focus]);
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
                <li
                  key={r.id}
                  className="routine-item"
                  ref={r.id === focus ? focused : undefined}
                >
                  <RoutineLine
                    routine={r}
                    head={
                      <button
                        type="button"
                        className="routine-toggle"
                        aria-expanded={open}
                        onClick={() => (open ? collapse() : expand(r.id))}
                      >
                        {open ? (
                          <ChevronDown size={14} />
                        ) : (
                          <ChevronRight size={14} />
                        )}
                        <span className="routine-name">{r.name}</span>
                      </button>
                    }
                    heartbeat={!open}
                    onChanged={() => {
                      void load();
                      if (open) void loadRuns(r.id);
                    }}
                    onOpenRuns={() => expand(r.id)}
                  />
                  <p className="routine-meta">
                    {[
                      r.command && <code key="c">{r.command.run}</code>,
                      target(r),
                      made(r),
                    ]
                      .filter(Boolean)
                      .flatMap((part, i) => (i ? [" · ", part] : [part]))}
                  </p>
                  {open && (
                    <div className="routine-details">
                      {/* An assistant's routines change in its file. */}
                      {!r.createdBy.startsWith("file:") && (
                        <div className="routine-actions">
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
                        </div>
                      )}
                      <RunHistory
                        runs={runs}
                        picked={picked}
                        onPick={setPicked}
                        sessionTitle={sessionTitle}
                        onOpenSession={onOpenSession}
                      />
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

// How many runs worth a look the open routine lists.
const NOTABLE_SHOWN = 10;

// An open routine's kept runs: the heartbeat with a count, the runs that sent
// something or failed, and the details of the one picked.
function RunHistory({
  runs,
  picked,
  onPick,
  sessionTitle,
  onOpenSession,
}: {
  runs: RoutineRun[];
  picked: string | null;
  onPick: (id: string | null) => void;
  sessionTitle: (id: string) => string;
  onOpenSession?: (id: string) => void;
}) {
  if (!runs.length) return <p className="subtle">No runs yet.</p>;
  const oldest = runs[runs.length - 1];
  const notable = runs
    .filter((r) => ["delivered", "failed"].includes(runKind(r)))
    .slice(0, NOTABLE_SHOWN);
  const run = runs.find((r) => r.id === picked);
  const pick = (id: string) => onPick(id === picked ? null : id);
  return (
    <div className="run-history">
      <p className="run-history-count">
        {heartbeatSummary(runs)} · since {shortTime(oldest.started)}
      </p>
      <Heartbeat
        runs={runs}
        selected={picked ?? undefined}
        onSelect={(r) => pick(r.id)}
      />
      {notable.length ? (
        <ul className="run-notable" aria-label="Runs worth a look">
          {notable.map((r) => (
            <li key={r.id}>
              <button
                type="button"
                className={`run-notable-item ${runKind(r)}`}
                aria-pressed={r.id === picked}
                onClick={() => pick(r.id)}
              >
                <span className={`heartbeat-dot ${runKind(r)}`} aria-hidden />
                <span className="run-notable-time">{shortTime(r.started)}</span>
                <span className="run-notable-text">{runResult(r)}</span>
              </button>
            </li>
          ))}
        </ul>
      ) : (
        <p className="subtle run-history-quiet">
          Nothing to report: no run sent anything or failed.
        </p>
      )}
      {run && (
        <div className="run-detail" aria-label="Run details">
          <p>
            <strong>{shortTime(run.started)}</strong>
            {runDuration(run) && ` · took ${runDuration(run)}`}
            {run.exit !== undefined && ` · exit ${run.exit}`}
          </p>
          <p className={runKind(run) === "failed" ? "error-text" : undefined}>
            {runResult(run)}
          </p>
          {run.session && onOpenSession && (
            <Button
              variant="ghost"
              size="sm"
              onClick={() => onOpenSession(run.session!)}
            >
              Open “{sessionTitle(run.session)}”
            </Button>
          )}
          {run.output ? (
            <pre className="routine-output">{run.output}</pre>
          ) : (
            // A command that ran; a message has no output of its own.
            run.exit !== undefined && (
              <p className="subtle">It printed nothing.</p>
            )
          )}
        </div>
      )}
    </div>
  );
}
