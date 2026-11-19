import { useEffect, useState } from "react";
import { FileText, Settings, Sparkles, Wrench } from "lucide-react";
import { api } from "../lib/api";
import {
  openAssistantSettings,
  routinesOf,
  setUpAssistant,
  type Assistant,
} from "../lib/assistants";
import { describeWhen, type Routine } from "../routines/when";
import { MossIcon } from "../components/MossIcon";
import { Button } from "../components/ui/button";

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
  // Its routines, read for this view only.
  const [routines, setRoutines] = useState<Routine[] | null>(null);
  const [error, setError] = useState("");
  useEffect(() => {
    let live = true;
    api<Routine[]>("routines")
      .then((list) => live && setRoutines(routinesOf(list, a.dir)))
      .catch(() => live && setRoutines([]));
    return () => {
      live = false;
    };
  }, [a.dir]);
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
