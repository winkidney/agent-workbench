import { useEffect, useRef, useState } from "react";
import {
  Bot,
  ChevronRight,
  Cpu,
  Eye,
  FileText,
  Plug,
  Square,
  SquareTerminal,
  Workflow,
} from "lucide-react";
import { api } from "../lib/api";
import { AnsiText } from "../transcript/Timeline";
import {
  KIND_LABEL,
  backgroundChanged,
  elapsed,
  running,
  summary,
  taskState,
  taskTitle,
  type BackgroundTask,
} from "./background";

const ICONS: Record<string, typeof Bot> = {
  shell: SquareTerminal,
  terminal: SquareTerminal,
  monitor: Eye,
  agent: Bot,
  mcp: Plug,
  workflow: Workflow,
  process: Cpu,
};

// The end of a Claude task's output file, refreshed while it runs.
function TaskOutput({
  sessionId,
  task,
}: {
  sessionId: string;
  task: BackgroundTask;
}) {
  const [text, setText] = useState<string | null>(null);
  const [error, setError] = useState("");
  const box = useRef<HTMLPreElement>(null);
  const live = task.status === "running";
  useEffect(() => {
    let cancelled = false;
    const read = () =>
      api<{ text: string }>(
        `sessions/${encodeURIComponent(sessionId)}/background-output?task=${encodeURIComponent(task.id)}`,
      )
        .then((v) => {
          if (cancelled) return;
          setText(v.text);
          setError("");
        })
        .catch((e) => !cancelled && setError(String(e.message ?? e)));
    void read();
    const timer = live ? setInterval(read, 2000) : undefined;
    return () => {
      cancelled = true;
      if (timer) clearInterval(timer);
    };
  }, [sessionId, task.id, live]);
  useEffect(() => {
    const el = box.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [text]);
  if (error) return <p className="bg-output-note">{error}</p>;
  if (text === null) return <p className="bg-output-note">Loading…</p>;
  return (
    <pre className="bg-output" ref={box}>
      {text ? <AnsiText text={text} /> : "No output yet."}
    </pre>
  );
}

// Above the composer: what runs outside the turns, and a way to stop it.
export function BackgroundTray({
  sessionId,
  tasks,
  readOnly,
}: {
  sessionId: string;
  tasks: BackgroundTask[];
  readOnly: boolean;
}) {
  const [open, setOpen] = useState(false);
  const [shown, setShown] = useState<string | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const [error, setError] = useState("");
  const [now, setNow] = useState(() => Date.now());
  const live = running(tasks);
  useEffect(() => {
    setOpen(false);
    setShown(null);
    setError("");
  }, [sessionId]);
  useEffect(() => {
    if (!live.length) return;
    const timer = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, [live.length]);
  if (!tasks.length) return null;
  const stop = async (task?: string) => {
    setBusy(task ?? "*");
    setError("");
    try {
      await api(
        `sessions/${encodeURIComponent(sessionId)}/background-stop`,
        "POST",
        {
          task: task ?? "",
        },
      );
      backgroundChanged(sessionId);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(null);
    }
  };
  return (
    <div className={"bg-tray" + (live.length ? " live" : "")}>
      <div className="bg-head-row">
        <button
          type="button"
          className="bg-head"
          aria-expanded={open}
          onClick={() => setOpen(!open)}
        >
          <span className="bg-ring" aria-hidden />
          <span className="bg-summary">{summary(tasks, now)}</span>
          <ChevronRight
            size={12}
            className={open ? "fold-chevron open" : "fold-chevron"}
          />
        </button>
        {!readOnly && live.length > 1 && (
          <button
            type="button"
            className="goal-action"
            disabled={busy !== null}
            onClick={() => void stop()}
          >
            Stop all
          </button>
        )}
        {!readOnly && live.length === 1 && !open && (
          <button
            type="button"
            className="goal-action"
            disabled={busy !== null}
            title={`Stop ${taskTitle(live[0])}`}
            onClick={() => void stop(live[0].id)}
          >
            Stop
          </button>
        )}
      </div>
      {open && (
        <ul className="bg-list">
          {tasks.map((t) => {
            const Icon = ICONS[t.kind] ?? Cpu;
            return (
              <li key={t.id} className={"bg-task " + t.status}>
                <div className="bg-task-row">
                  <Icon size={13} className="bg-kind" aria-hidden />
                  <span className="bg-kind-label">
                    {KIND_LABEL[t.kind] ?? t.kind}
                  </span>
                  <span className="bg-title" title={t.description}>
                    {taskTitle(t)}
                  </span>
                  <span className="bg-time">{elapsed(t, now)}</span>
                  <span className="bg-state" title={t.summary}>
                    {taskState(t)}
                  </span>
                  <span className="bg-actions">
                    {t.output && (
                      <button
                        type="button"
                        className="goal-action"
                        aria-expanded={shown === t.id}
                        onClick={() => setShown(shown === t.id ? null : t.id)}
                      >
                        <FileText size={11} />
                        Output
                      </button>
                    )}
                    {!readOnly && t.status === "running" && (
                      <button
                        type="button"
                        className="goal-action"
                        disabled={busy !== null}
                        onClick={() => void stop(t.id)}
                      >
                        <Square size={9} fill="currentColor" />
                        Stop
                      </button>
                    )}
                  </span>
                </div>
                {t.status !== "running" && t.summary && (
                  <p className="bg-task-summary">{t.summary}</p>
                )}
                {shown === t.id && (
                  <TaskOutput sessionId={sessionId} task={t} />
                )}
              </li>
            );
          })}
        </ul>
      )}
      {error && <p className="bg-error">{error}</p>}
    </div>
  );
}
