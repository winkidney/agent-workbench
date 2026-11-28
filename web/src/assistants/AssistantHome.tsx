import { useEffect, useRef, useState } from "react";
import {
  ArrowLeft,
  BellRing,
  Clock,
  Eye,
  FileText,
  MessageSquareText,
  Settings,
  ShieldAlert,
  Sparkles,
  Wrench,
} from "lucide-react";
import { api } from "../lib/api";
import {
  openAssistantSettings,
  openSessionLater,
  routinesOf,
  setUpAssistant,
  type Assistant,
} from "../lib/assistants";
import {
  firstLine,
  since,
  underWay,
  whereRuns,
  type Routine,
} from "../routines/when";
import { RoutineLine } from "../routines/RoutineLine";
import { openRoutine } from "../routines/open";
import { seeReport } from "../routines/waiting";
import { AssistantIcon, iconOf } from "../components/AssistantIcon";
import { ATTENTION_LABEL } from "../attention";
import { relativeTime, type Session } from "../sidebar/tree";
import { PixelPulse } from "../components/pixel";
import { Button } from "../components/ui/button";
import { Markdown } from "../transcript/Markdown";
import { EnvContext, type Env } from "../transcript/media";

// An assistant's home card (ADR 0015): what it does, what it can do,
// requests to try, its routines, and whether it is set up. Shown above a new
// request and from the ⓘ button in its conversations.
export function AssistantHome({
  assistant: a,
  onTry,
  onOpenFile,
  waiting: sessions = [],
  onOpenSession = openSessionLater,
}: {
  assistant: Assistant;
  // An example request was picked: put it in the composer.
  onTry: (text: string) => void;
  // Opens one of its files in the preview panel.
  onOpenFile?: (path: string) => void;
  // Its sessions that need the user (sidebar/tree.ts waitingAt): its bell
  // in the sidebar counts them, so the card names each with Open.
  waiting?: Session[];
  onOpenSession?: (id: string) => void;
}) {
  // Its routines, read for this view only, and again when one changes (a
  // run that starts waiting for approval).
  const [routines, setRoutines] = useState<Routine[] | null>(null);
  const [error, setError] = useState("");
  const [deciding, setDeciding] = useState("");
  const [reload, setReload] = useState(0);
  useEffect(() => {
    let live = true;
    const load = () =>
      api<Routine[]>("routines")
        .then((list) => live && setRoutines(routinesOf(list, a.dir)))
        .catch(() => live && setRoutines([]));
    void load();
    window.addEventListener("workbench:routines", load);
    return () => {
      live = false;
      window.removeEventListener("workbench:routines", load);
    };
  }, [a.dir, reload]);
  // Approves or dismisses every run of r that waits, at once: approved,
  // what their commands printed goes to the agent as one message, one
  // conversation (ADR 0021, 0027).
  const decide = async (r: Routine, action: "approve" | "dismiss") => {
    setDeciding(r.id);
    setError("");
    try {
      await api(`routines/${encodeURIComponent(r.id)}/${action}`, "POST");
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setDeciding("");
      setReload((n) => n + 1);
    }
  };
  // A held run of r: start it now beside what holds the lane, or drop it.
  const actOnHeld = async (
    r: Routine,
    run: string,
    action: "start" | "dismiss",
  ) => {
    setDeciding(r.id);
    setError("");
    try {
      await api(
        `routines/${encodeURIComponent(r.id)}/runs/${encodeURIComponent(run)}/${action}`,
        "POST",
      );
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setDeciding("");
      setReload((n) => n + 1);
    }
  };
  // The user saw r's report: Open goes on to its session.
  const see = async (r: Routine, open?: string) => {
    if (open) onOpenSession(open);
    try {
      await seeReport(r.id);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setReload((n) => n + 1);
    }
  };
  const waiting = (routines ?? []).filter((r) => r.waiting?.length);
  const held = (routines ?? []).filter((r) => r.held?.length);
  const reports = (routines ?? []).filter((r) => r.report);
  // What waits for the user, as its sidebar bell counts it (waitingFor.ts).
  const needs =
    sessions.length +
    reports.length +
    waiting.reduce((n, r) => n + (r.waiting?.length ?? 0), 0);
  const now = useNow(!!routines?.some(underWay));
  const agent = a.agent === "codex" ? "Codex" : "Claude";
  return (
    <section className="assistant-home" aria-label={`${a.name}: about`}>
      <header className="assistant-home-head">
        {/* Its icon, its agent as a badge (ADR 0030). */}
        {(a.agent === "claude" || a.agent === "codex") && (
          <AssistantIcon
            {...iconOf(a)}
            agent={a.agent}
            size={64}
            name={a.name}
          />
        )}
        <div>
          <h2>{a.name}</h2>
          <p className="subtle">
            {agent} ·{" "}
            {a.sessions === "continuous"
              ? "one ongoing conversation"
              : "a new session for each request"}
            {a.enabled === false && " · turned off"}
          </p>
        </div>
      </header>
      {a.sessions === "continuous" && (
        <p className="subtle assistant-home-note">
          One ongoing conversation: a fresh one starts each morning or when it
          gets long, and what matters carries over. Start fresh yourself (⋯
          menu) only when you change to an unrelated subject.
        </p>
      )}
      {!a.ready && (
        <div className="assistant-home-setup" role="status">
          <Wrench size={14} />
          <span>Not set up yet: it asks you for what it needs.</span>
          <Button
            size="sm"
            onClick={() =>
              void setUpAssistant(a.dir).catch((e) =>
                setError(e instanceof Error ? e.message : String(e)),
              )
            }
          >
            Set up
          </Button>
        </div>
      )}
      {(needs > 0 || held.length > 0) && (
        <div
          className="assistant-home-block"
          role="group"
          aria-label="Waiting for you"
        >
          <h3>
            {needs > 0 ? (
              <>
                Waiting for you{" "}
                <span className="assistant-home-count">{needs}</span>
              </>
            ) : (
              "Waiting for its turn"
            )}
          </h3>
          {sessions.map((s) => (
            <WaitingSession
              key={s.id}
              session={s}
              from={routineOfSession(routines ?? [], s.id)}
              now={now}
              onOpen={() => onOpenSession(s.id)}
            />
          ))}
          {reports.map((r) => (
            <ReportCard
              key={r.id}
              routine={r}
              now={now}
              onSee={(open) => void see(r, open)}
            />
          ))}
          {waiting.map((r) => (
            <WaitingCard
              key={r.id}
              routine={r}
              deciding={deciding === r.id}
              onDecide={(action) => void decide(r, action)}
            />
          ))}
          {held.map((r) => (
            <HeldCard
              key={r.id}
              routine={r}
              deciding={deciding === r.id}
              onAct={(run, action) => void actOnHeld(r, run, action)}
            />
          ))}
        </div>
      )}
      <RunningNow
        routines={routines ?? []}
        now={now}
        onOpenSession={onOpenSession}
      />
      {error && <p className="error-text">{error}</p>}
      <p className="assistant-home-description">
        {a.description || (
          <span className="subtle">
            No description yet: ask it what it does, or add one in its settings.
          </span>
        )}
      </p>
      {!!a.capabilities?.length && (
        <>
          <h3>What it can do</h3>
          <ul className="assistant-home-list">
            {a.capabilities.map((c) => (
              <li key={c}>{c}</li>
            ))}
          </ul>
        </>
      )}
      {!!a.requires?.length && (
        <>
          <h3>What it needs</h3>
          <ul className="assistant-home-list">
            {a.requires.map((r) => (
              <li key={r}>{r}</li>
            ))}
          </ul>
        </>
      )}
      {!!a.examples?.length && (
        <>
          <h3>Try</h3>
          <div className="quick-start" role="group" aria-label="Try">
            <Sparkles size={13} aria-hidden className="quick-start-icon" />
            {a.examples.map((e) => (
              <button
                key={e}
                type="button"
                className="quick-start-chip"
                onClick={() => onTry(e)}
              >
                {e}
              </button>
            ))}
          </div>
        </>
      )}
      {routines && routines.length > 0 && (
        <>
          <h3>Routines</h3>
          <ul className="routine-lines" aria-label="Routines">
            {routines.map((r) => (
              <li key={r.id}>
                <RoutineLine
                  routine={r}
                  onChanged={() => setReload((n) => n + 1)}
                  onOpenRuns={() => openRoutine(r.id)}
                />
              </li>
            ))}
          </ul>
        </>
      )}
      <footer className="assistant-home-foot">
        <Button
          variant="ghost"
          size="sm"
          onClick={() => openAssistantSettings(a.dir)}
        >
          <Settings size={14} />
          Settings
        </Button>
        {onOpenFile && (
          <Button
            variant="ghost"
            size="sm"
            onClick={() => onOpenFile(a.path + "/AGENTS.md")}
          >
            <FileText size={14} />
            Its procedure (AGENTS.md)
          </Button>
        )}
      </footer>
    </section>
  );
}

// Now, ticking every 30 seconds while on: for how long runs have run.
function useNow(on: boolean): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!on) return;
    setNow(Date.now());
    const t = window.setInterval(() => setNow(Date.now()), 30_000);
    return () => window.clearInterval(t);
  }, [on]);
  return now;
}

// The routine a session came from: the routine whose run it was sent to.
export function routineOfSession(
  routines: Routine[],
  session: string,
): string | undefined {
  return routines.find((r) =>
    [r.last, ...(r.recent ?? [])].some((run) => run?.session === session),
  )?.name;
}

// A session of the assistant that needs the user: where it comes from (its
// routine, else its name), what it wants, since when, and Open.
export function WaitingSession({
  session: s,
  from,
  now,
  onOpen,
}: {
  session: Session;
  from?: string;
  now: number;
  onOpen: () => void;
}) {
  const ago = s.updated ? relativeTime(s.updated, now) : "";
  return (
    <div
      className="assistant-home-setup assistant-home-approve"
      role="status"
      title={s.title || s.nativeId}
    >
      <BellRing size={14} />
      <span>
        <strong>{from ?? (s.title || s.nativeId)}</strong> ·{" "}
        {ATTENTION_LABEL[s.attention ?? ""] ?? "Needs you"}
        {ago && ` · ${ago === "now" ? "just now" : `${ago} ago`}`}
      </span>
      <Button size="sm" onClick={onOpen}>
        Open
      </Button>
    </div>
  );
}

// A routine's report for the user (routine.go reportOf): the agent's reply,
// which the card shows as it is; Open goes to the conversation, Dismiss
// only records that it was read. Either takes it out of the bell.
export function ReportCard({
  routine: r,
  now,
  onSee,
}: {
  routine: Routine;
  now: number;
  onSee: (open?: string) => void;
}) {
  const run = r.report;
  if (!run) return null;
  const at = run.finished || run.started;
  const ago = at ? relativeTime(at, now) : "";
  return (
    <div className="assistant-home-setup assistant-home-report" role="status">
      <MessageSquareText size={14} />
      <span>
        <strong>{r.name}</strong> · report
        {ago && ` · ${ago === "now" ? "just now" : `${ago} ago`}`}
      </span>
      {run.session && (
        <Button size="sm" onClick={() => onSee(run.session)}>
          Open
        </Button>
      )}
      <Button size="sm" variant="ghost" onClick={() => onSee()}>
        Dismiss
      </Button>
      <div className="assistant-home-preview">
        <EnvContext.Provider value={PREVIEW_ENV}>
          <Markdown text={run.report ?? ""} />
        </EnvContext.Provider>
      </div>
    </div>
  );
}

// The assistant's routine runs under way: where each runs and for how long,
// with its conversation (Open) or, in the background, its runs.
export function RunningNow({
  routines,
  now,
  onOpenSession,
}: {
  routines: Routine[];
  now: number;
  onOpenSession: (id: string) => void;
}) {
  const running = routines.flatMap((r) => {
    const run = underWay(r);
    return run ? [{ r, run }] : [];
  });
  if (!running.length) return null;
  return (
    <div className="assistant-home-block" role="group" aria-label="Running now">
      <h3>Running now</h3>
      {running.map(({ r, run }) => (
        <div
          key={r.id}
          className="assistant-home-setup assistant-home-running"
          role="status"
        >
          <PixelPulse live />
          <span>
            <strong>{r.name}</strong> · {whereRuns(run)} ·{" "}
            {since(run.started, now)}
          </span>
          {run.session ? (
            <Button size="sm" onClick={() => onOpenSession(run.session!)}>
              Open
            </Button>
          ) : (
            <Button size="sm" variant="ghost" onClick={() => openRoutine(r.id)}>
              Runs
            </Button>
          )}
        </div>
      ))}
    </div>
  );
}

// A preview shows outside content (a mail's sender and subject), and the
// user only reads it before deciding (ADR 0023): its links do nothing, but a
// link to a Markdown file beside the preview (a mail's file) shows that file
// in place (ADR 0026), and the links in that file do nothing at all.
const PREVIEW_ENV: Env = {
  cwd: "",
  openUrl: () => {},
  openPath: () => {},
  thumb: () => null,
  reveal: () => {},
  locate: () => {},
  view: () => {},
};

// A routine whose runs wait for the assistant's other routine work to
// finish (ADR 0027): they start on their own when it does; Start now runs
// the oldest beside it, Dismiss drops it.
export function HeldCard({
  routine: r,
  deciding,
  onAct,
}: {
  routine: Routine;
  deciding: boolean;
  onAct: (run: string, action: "start" | "dismiss") => void;
}) {
  const runs = r.held ?? [];
  const first = runs[0];
  if (!first) return null;
  const n = runs.length;
  return (
    <div className="assistant-home-setup assistant-home-held" role="status">
      <Clock size={14} />
      <span>
        {r.name}: {n === 1 ? "1 run waits" : `${n} runs wait`} for its turn
        {first.behind ? (
          <>
            , after{" "}
            {first.behindSession ? (
              <button
                type="button"
                className="held-behind"
                onClick={() => openSessionLater(first.behindSession!)}
              >
                {first.behind}
              </button>
            ) : (
              first.behind
            )}
          </>
        ) : null}
        . It starts when that is done
        {n > 1 ? `, as one message` : ""}.
      </span>
      <Button
        size="sm"
        variant="ghost"
        disabled={deciding}
        title="Run it now, beside what it waits for"
        onClick={() => onAct(first.id, "start")}
      >
        Start now
      </Button>
      <Button
        size="sm"
        variant="ghost"
        disabled={deciding}
        onClick={() => onAct(first.id, "dismiss")}
      >
        Dismiss
      </Button>
    </div>
  );
}

// The API path that reads a file a preview links to: a relative .md path
// only; the server checks it stays in the preview's folder (ADR 0026).
export function previewFilePath(routine: string, path: string): string | null {
  const p = path.trim().replace(/^\.\//, "");
  if (!p || p.startsWith("/") || /^[a-z]+:/i.test(p) || !/\.md$/i.test(p))
    return null;
  return `routines/${encodeURIComponent(routine)}/preview?file=${encodeURIComponent(p)}`;
}

// A routine whose runs wait for approval (ADR 0021). With a preview, the
// workbench reads the file itself and shows it here, on any page, and
// Approve waits until the user has opened what the waiting runs wrote
// (ADR 0023). Dismiss never waits: it sends nothing.
export function WaitingCard({
  routine: r,
  deciding,
  onDecide,
}: {
  routine: Routine;
  deciding: boolean;
  onDecide: (action: "approve" | "dismiss") => void;
}) {
  const runs = r.waiting ?? [];
  const said = runs.length === 1 ? firstLine(runs[0].output) : "";
  // The runs it waits for: a newer one rewrites the preview, so it is read
  // again before Approve.
  const runsKey = runs.map((run) => run.id).join(",");
  const [open, setOpen] = useState(false);
  const [text, setText] = useState<string | null>(null);
  const [failed, setFailed] = useState("");
  const [seen, setSeen] = useState("");
  // The file a preview link opened, shown in place of the list.
  const [file, setFile] = useState<{
    path: string;
    text: string | null;
    failed: string;
  } | null>(null);
  const box = useRef<HTMLDivElement>(null);
  const listTop = useRef(0);
  const openFile = async (path: string) => {
    const url = previewFilePath(r.id, path);
    if (!url) return;
    listTop.current = box.current?.scrollTop ?? 0;
    setFile({ path, text: null, failed: "" });
    // A later click wins over a slower answer.
    const settle = (text: string | null, failed: string) =>
      setFile((cur) => (cur?.path === path ? { path, text, failed } : cur));
    try {
      settle((await api<{ text: string }>(url)).text, "");
    } catch (e) {
      settle(null, e instanceof Error ? e.message : String(e));
    }
  };
  const back = () => {
    setFile(null);
    requestAnimationFrame(() => {
      if (box.current) box.current.scrollTop = listTop.current;
    });
  };
  const env: Env = { ...PREVIEW_ENV, openPath: (path) => void openFile(path) };
  const show = async () => {
    if (open) return setOpen(false);
    setOpen(true);
    setFailed("");
    try {
      const v = await api<{ text: string }>(
        `routines/${encodeURIComponent(r.id)}/preview`,
      );
      setText(v.text);
      setSeen(runsKey);
    } catch (e) {
      setText(null);
      setFailed(e instanceof Error ? e.message : String(e));
    }
  };
  const mustSee = !!r.preview && seen !== runsKey;
  return (
    <div className="assistant-home-setup assistant-home-approve" role="status">
      <ShieldAlert size={14} />
      <span>
        {r.name}:{" "}
        {said || `${runs.length} ${runs.length === 1 ? "run" : "runs"}`}{" "}
        {runs.length === 1 ? "waits" : "wait"} for your approval. Nothing has
        reached the agent yet.
        {mustSee && " Open the preview first."}
      </span>
      {r.preview && (
        <Button
          size="sm"
          variant="ghost"
          aria-expanded={open}
          onClick={() => void show()}
        >
          <Eye size={14} />
          {open ? "Hide preview" : "Preview"}
        </Button>
      )}
      <Button
        size="sm"
        disabled={deciding || mustSee}
        title={mustSee ? "Open the preview first" : undefined}
        onClick={() => onDecide("approve")}
      >
        Approve
      </Button>
      <Button
        size="sm"
        variant="ghost"
        disabled={deciding}
        onClick={() => onDecide("dismiss")}
      >
        Dismiss
      </Button>
      {open && (
        <div className="assistant-home-preview" ref={box}>
          {file ? (
            <>
              <p className="subtle assistant-home-preview-file">
                <Button size="sm" variant="ghost" onClick={back}>
                  <ArrowLeft size={14} />
                  Back to the list
                </Button>
                <span title={file.path}>{file.path}</span>
              </p>
              {file.failed ? (
                <p className="error-text">{file.failed}</p>
              ) : file.text === null ? (
                <p className="subtle">Loading…</p>
              ) : (
                <EnvContext.Provider value={PREVIEW_ENV}>
                  <Markdown text={file.text} />
                </EnvContext.Provider>
              )}
            </>
          ) : (
            <>
              <p className="subtle" title={r.preview}>
                {r.preview}
              </p>
              {failed ? (
                <p className="error-text">{failed}</p>
              ) : text === null ? (
                <p className="subtle">Loading…</p>
              ) : (
                <EnvContext.Provider value={env}>
                  <Markdown text={text} />
                </EnvContext.Provider>
              )}
            </>
          )}
        </div>
      )}
    </div>
  );
}
