import { useEffect, useRef, useState } from "react";
import {
  ArrowLeft,
  Eye,
  FileText,
  Settings,
  ShieldAlert,
  Sparkles,
  Wrench,
} from "lucide-react";
import { api } from "../lib/api";
import {
  openAssistantSettings,
  routinesOf,
  setUpAssistant,
  type Assistant,
} from "../lib/assistants";
import { describeWhen, firstLine, type Routine } from "../routines/when";
import { Heartbeat } from "../routines/Heartbeat";
import { MossIcon } from "../components/MossIcon";
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
}: {
  assistant: Assistant;
  // An example request was picked: put it in the composer.
  onTry: (text: string) => void;
  // Opens one of its files in the preview panel.
  onOpenFile?: (path: string) => void;
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
  // Approves or dismisses every run of r that waits, oldest first: an
  // approved run hands what its command printed to the agent (ADR 0021).
  const decide = async (r: Routine, action: "approve" | "dismiss") => {
    setDeciding(r.id);
    setError("");
    try {
      for (const run of r.waiting ?? [])
        await api(`routines/${r.id}/runs/${run.id}/${action}`, "POST");
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setDeciding("");
      setReload((n) => n + 1);
    }
  };
  const waiting = (routines ?? []).filter((r) => r.waiting?.length);
  const agent = a.agent === "codex" ? "Codex" : "Claude";
  return (
    <section className="assistant-home" aria-label={`${a.name}: about`}>
      <header className="assistant-home-head">
        {/* Every assistant is the box camera, its agent on its screen. */}
        {(a.agent === "claude" || a.agent === "codex") && (
          <MossIcon agent={a.agent} size={64} title={a.name} />
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
      {waiting.map((r) => (
        <WaitingCard
          key={r.id}
          routine={r}
          deciding={deciding === r.id}
          onDecide={(action) => void decide(r, action)}
        />
      ))}
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
          <ul className="assistant-home-list">
            {routines.map((r) => (
              <li key={r.id}>
                {r.name} · {describeWhen(r.when)} ·{" "}
                <span className="subtle">
                  {r.assistantOff
                    ? "waiting (turned off)"
                    : r.enabled
                      ? "on"
                      : "off"}
                </span>
                {!!r.recent?.length && (
                  <span className="assistant-home-pulse">
                    <Heartbeat compact runs={r.recent} />
                  </span>
                )}
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
