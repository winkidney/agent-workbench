import { useState } from "react";
import { Loader2, Plus } from "lucide-react";
import { api } from "../../lib/api";
import { ChangeList } from "../../assistants/Changes";
import {
  assistantsChanged,
  folderFor,
  useChanges,
  type Assistant,
  type Assistants,
} from "../../lib/assistants";
import { Button } from "../ui/button";
import { Input } from "../ui/input";

type Agent = "claude" | "codex";

function AgentChoice({
  value,
  onChange,
}: {
  value: Agent;
  onChange: (a: Agent) => void;
}) {
  return (
    <div className="agent-switch" role="radiogroup" aria-label="Agent">
      {(["claude", "codex"] as Agent[]).map((a) => (
        <button
          type="button"
          role="radio"
          aria-checked={value === a}
          key={a}
          className={value === a ? "on" : ""}
          onClick={() => onChange(a)}
        >
          {a === "claude" ? "Claude" : "Codex"}
        </button>
      ))}
    </div>
  );
}

// Assistants: one folder each in a repository of their own (ADR 0010).
// Their procedures and routines are files there; this tab sets the
// repository up, adds assistants and opens them.
export function AssistantsTab({
  value,
  onOpen,
}: {
  value: Assistants;
  onOpen: (dir: string) => void;
}) {
  const [root, setRoot] = useState("~/assistants");
  const [agent, setAgent] = useState<Agent>("claude");
  const [name, setName] = useState("");
  const [folder, setFolder] = useState("");
  const [sessions, setSessions] = useState<Assistant["sessions"]>("per-task");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [adding, setAdding] = useState(false);
  const changes = useChanges(!!value.root);
  const run = async (f: () => Promise<unknown>) => {
    setBusy(true);
    setError("");
    try {
      await f();
      assistantsChanged();
      return true;
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
      return false;
    } finally {
      setBusy(false);
    }
  };
  if (!value.root)
    return (
      <section className="settings-section">
        <h3>Assistants</h3>
        <p className="subtle">
          An assistant does one kind of work your way: its procedure, skills,
          scripts and schedule live in a folder of its own, in one git
          repository. Setting up makes the repository with shared rules, a
          shared wiki and a personal assistant to start with.
        </p>
        <form
          className="assistant-form"
          onSubmit={(e) => {
            e.preventDefault();
            void run(() => api("assistants/setup", "POST", { root, agent }));
          }}
        >
          <label className="routine-field">
            <span className="routine-label">Folder</span>
            <Input value={root} onChange={(e) => setRoot(e.target.value)} />
          </label>
          <div className="routine-row">
            <span className="routine-label">Agent</span>
            <AgentChoice value={agent} onChange={setAgent} />
          </div>
          {error && (
            <p className="error-text" role="alert">
              {error}
            </p>
          )}
          <div>
            <Button type="submit" disabled={busy || !root.trim()}>
              {busy && <Loader2 size={14} className="spin" />}
              Set up
            </Button>
          </div>
        </form>
      </section>
    );
  return (
    <>
      <section className="settings-section">
        <h3>Assistants</h3>
        <p className="subtle">
          In <code>{value.root}</code>. Each folder's <code>AGENTS.md</code> is
          its procedure and <code>assistant.toml</code> its settings and
          routines; edit them there, or ask the assistant to.
        </p>
        {!adding && (
          <Button variant="outline" size="sm" onClick={() => setAdding(true)}>
            <Plus size={14} />
            New assistant
          </Button>
        )}
        {adding && (
          <form
            className="assistant-form"
            onSubmit={(e) => {
              e.preventDefault();
              void run(() =>
                api("assistants", "POST", {
                  name,
                  dir: folder || folderFor(name),
                  agent,
                  sessions,
                }),
              ).then((ok) => {
                if (!ok) return;
                setAdding(false);
                setName("");
                setFolder("");
              });
            }}
          >
            <label className="routine-field">
              <span className="routine-label">Name</span>
              <Input
                value={name}
                autoFocus
                placeholder="Weekly report"
                onChange={(e) => setName(e.target.value)}
              />
            </label>
            <label className="routine-field">
              <span className="routine-label">Folder</span>
              <Input
                className="routine-mono"
                value={folder}
                placeholder={folderFor(name) || "weekly-report"}
                onChange={(e) => setFolder(e.target.value)}
              />
            </label>
            <div className="routine-row">
              <span className="routine-label">Agent</span>
              <AgentChoice value={agent} onChange={setAgent} />
            </div>
            <div className="routine-row">
              <span className="routine-label">Topics</span>
              <div
                className="agent-switch"
                role="radiogroup"
                aria-label="Topics"
              >
                {(
                  [
                    ["per-task", "A session per request"],
                    ["continuous", "One ongoing conversation"],
                  ] as [Assistant["sessions"], string][]
                ).map(([v, label]) => (
                  <button
                    type="button"
                    role="radio"
                    aria-checked={sessions === v}
                    key={v}
                    className={sessions === v ? "on" : ""}
                    onClick={() => setSessions(v)}
                  >
                    {label}
                  </button>
                ))}
              </div>
            </div>
            {error && (
              <p className="error-text" role="alert">
                {error}
              </p>
            )}
            <div className="confirm-actions">
              <Button
                type="button"
                variant="ghost"
                onClick={() => {
                  setAdding(false);
                  setError("");
                }}
              >
                Cancel
              </Button>
              <Button type="submit" disabled={busy || !name.trim()}>
                {busy && <Loader2 size={14} className="spin" />}
                Create
              </Button>
            </div>
          </form>
        )}
      </section>
      <ul className="routine-list">
        {value.assistants.map((a) => (
          <li key={a.dir} className="routine-item">
            <div className="routine-head">
              <span className="routine-name">{a.name}</span>
              <Button
                variant="ghost"
                size="sm"
                disabled={!!a.error}
                onClick={() => onOpen(a.dir)}
              >
                Open
              </Button>
            </div>
            <p className="routine-meta">
              <code>{a.dir}/</code>
              {a.agent && ` · ${a.agent === "claude" ? "Claude" : "Codex"}`}
              {a.sessions &&
                ` · ${a.sessions === "continuous" ? "one ongoing conversation" : "a session per request"}`}
              {a.memory === "wiki" && " · keeps the shared wiki"}
              {a.routines > 0 &&
                ` · ${a.routines} routine${a.routines === 1 ? "" : "s"}`}
            </p>
            {a.error && (
              <p className="routine-meta error-text">
                assistant.toml: {a.error}
              </p>
            )}
          </li>
        ))}
      </ul>
      <ChangeList list={changes} />
    </>
  );
}
