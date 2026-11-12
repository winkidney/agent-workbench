import { useLayoutEffect, useMemo, useRef, useState } from "react";
import { ArrowUp, Loader2 } from "lucide-react";
import { Button } from "../components/ui/button";
import { NewSessionOptions, type Options } from "../AgentControls";
import { api } from "../lib/api";
import type { Prefs, Session } from "../sidebar/tree";
import { ProjectPicker } from "./ProjectPicker";
import { AgentIcon } from "../components/AgentIcon";
import { recentProjects } from "./recent";

type Agent = "claude" | "codex";
const STORE = "workbench-new";

function remembered(): { agent?: Agent; cwd?: string } {
  try {
    return JSON.parse(localStorage.getItem(STORE) || "{}");
  } catch {
    return {};
  }
}

// Start a session in place, like a new chat: type the first message, pick the
// project and agent, and press Enter.
export function NewSession({
  sessions,
  workspaces,
  prefs,
  initialCwd,
  onCreated,
}: {
  sessions: Session[];
  workspaces: string[];
  prefs: Prefs;
  initialCwd?: string;
  onCreated: (s: Session) => void;
}) {
  const recent = useMemo(
    () => recentProjects(sessions, prefs, workspaces),
    [sessions, prefs, workspaces],
  );
  const saved = remembered();
  const [agent, setAgent] = useState<Agent>(saved.agent ?? "claude");
  const [cwd, setCwd] = useState(
    initialCwd || saved.cwd || recent[0]?.path || "",
  );
  const [options, setOptions] = useState<Options>({});
  const [text, setText] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const input = useRef<HTMLTextAreaElement>(null);
  useLayoutEffect(() => {
    const el = input.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height =
      Math.min(el.scrollHeight, Math.round(innerHeight * 0.4)) + "px";
  }, [text]);
  const remember = (next: { agent?: Agent; cwd?: string }) => {
    try {
      localStorage.setItem(STORE, JSON.stringify({ ...remembered(), ...next }));
    } catch {}
  };
  const project = recent.find((p) => p.path === cwd);
  const submit = async () => {
    const message = text.trim();
    if (!message || !cwd || busy) return;
    setBusy(true);
    setError("");
    remember({ agent, cwd });
    try {
      const s = await api<Session>("sessions", "POST", {
        agent,
        cwd,
        initialMessage: text,
        options,
        requestID: crypto.randomUUID(),
      });
      onCreated(s);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
      setBusy(false);
    }
  };
  const agentName = agent === "claude" ? "Claude Code" : "Codex";
  return (
    <div className="new-session">
      <div className="new-hero">
        <h1>What should we work on?</h1>
        <p>
          {project?.name ??
            (cwd ? cwd.split("/").filter(Boolean).pop() : "Choose a project")}
          {" · "}
          {agentName}
        </p>
      </div>
      <form
        className={"composer new-composer" + (busy ? " busy" : "")}
        onSubmit={(e) => {
          e.preventDefault();
          void submit();
        }}
      >
        <textarea
          ref={input}
          rows={2}
          autoFocus
          aria-label="First message"
          placeholder={`Describe a task for ${agentName}…`}
          value={text}
          disabled={busy}
          onChange={(e) => setText(e.target.value)}
          onKeyDown={(e) => {
            if (
              e.key === "Enter" &&
              !e.shiftKey &&
              !e.nativeEvent.isComposing
            ) {
              e.preventDefault();
              void submit();
            }
          }}
        />
        <div className="composer-bottom">
          <div className="agent-switch" role="radiogroup" aria-label="Agent">
            {(["claude", "codex"] as Agent[]).map((a) => (
              <button
                type="button"
                role="radio"
                aria-checked={agent === a}
                key={a}
                className={agent === a ? "on" : ""}
                disabled={busy}
                onClick={() => {
                  setAgent(a);
                  setOptions({});
                  remember({ agent: a });
                }}
              >
                <AgentIcon agent={a} size={16} decorative />
                {a === "claude" ? "Claude" : "Codex"}
              </button>
            ))}
          </div>
          <NewSessionOptions
            agent={agent}
            cwd={cwd}
            value={options}
            onChange={setOptions}
          />
          <div className="composer-actions">
            <Button
              size="icon"
              type="submit"
              className="composer-send"
              aria-label="Start session"
              title="Start (Enter)"
              disabled={busy || !text.trim() || !cwd}
            >
              {busy ? (
                <Loader2 size={15} className="spin" />
              ) : (
                <ArrowUp size={16} />
              )}
            </Button>
          </div>
        </div>
      </form>
      <div className="new-meta">
        <ProjectPicker
          value={cwd}
          recent={recent}
          onChange={(p) => {
            setCwd(p);
            remember({ cwd: p });
          }}
        />
        <span className="new-hint">
          {busy ? `Starting ${agentName}…` : "↵ start · ⇧↵ new line"}
        </span>
      </div>
      {error && (
        <p role="alert" className="error-text new-error">
          {error}
        </p>
      )}
    </div>
  );
}
