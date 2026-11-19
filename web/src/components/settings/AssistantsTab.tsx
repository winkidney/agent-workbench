import { useState } from "react";
import { Loader2, Pencil, Plus } from "lucide-react";
import { api } from "../../lib/api";
import { ChangeList } from "../../assistants/Changes";
import {
  assistantsChanged,
  folderFor,
  MAIN_NAME,
  setUpAssistants,
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
  const [agent, setAgent] = useState<Agent>(value.defaultAgent || "claude");
  const [name, setName] = useState("");
  const [folder, setFolder] = useState("");
  const [sessions, setSessions] = useState<Assistant["sessions"]>("per-task");
  // An empty procedure, or a template (internal/workbench/assistant/kinds).
  const [kind, setKind] = useState<"" | "mail">("");
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
          {MAIN_NAME}, your personal assistant, keeps one ongoing conversation
          with you and remembers what matters. Ask it for an assistant for any
          job you repeat. Everything lives as plain files in a git repository on
          this computer.
        </p>
        <div>
          <Button onClick={setUpAssistants}>Set up…</Button>
        </div>
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
                  sessions: kind === "mail" ? "per-task" : sessions,
                  kind,
                }),
              ).then((ok) => {
                if (!ok) return;
                setAdding(false);
                setName("");
                setFolder("");
                setKind("");
              });
            }}
          >
            <div className="routine-row">
              <span className="routine-label">Start from</span>
              <div
                className="agent-switch"
                role="radiogroup"
                aria-label="Start from"
              >
                {(
                  [
                    ["", "An empty procedure"],
                    ["mail", "Mail triage"],
                  ] as ["" | "mail", string][]
                ).map(([v, label]) => (
                  <button
                    type="button"
                    role="radio"
                    aria-checked={kind === v}
                    key={v || "empty"}
                    className={kind === v ? "on" : ""}
                    onClick={() => {
                      setKind(v);
                      if (v === "mail" && !name.trim()) setName("Mail");
                    }}
                  >
                    {label}
                  </button>
                ))}
              </div>
            </div>
            {kind === "mail" && (
              <p className="subtle">
                Reads new mail over IMAP every 30 minutes, without marking it
                read, and sorts it into <code>state/inbox.md</code> in the
                background. It cannot send. Its routine starts off: fill in{" "}
                <code>config.toml</code> and the password as its{" "}
                <code>AGENTS.md</code> says, then switch it on.
              </p>
            )}
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
            {kind !== "mail" && (
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
            )}
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
          <AssistantItem key={a.dir} assistant={a} onOpen={onOpen} />
        ))}
      </ul>
      <ChangeList list={changes} />
    </>
  );
}

// One assistant: open it, rename it, switch its agent. Both change its
// assistant.toml (PATCH /api/assistants/{dir}).
function AssistantItem({
  assistant: a,
  onOpen,
}: {
  assistant: Assistant;
  onOpen: (dir: string) => void;
}) {
  const [naming, setNaming] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const update = async (change: { name?: string; agent?: Agent }) => {
    setBusy(true);
    setError("");
    try {
      await api(`assistants/${a.dir}`, "PATCH", change);
      assistantsChanged();
      setNaming(null);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <li className="routine-item">
      <div className="routine-head">
        {naming === null ? (
          <span className="routine-name">
            {a.name}
            {a.dir === "main" && (
              <span className="subtle"> · your personal assistant</span>
            )}
          </span>
        ) : (
          <form
            className="assistant-rename"
            onSubmit={(e) => {
              e.preventDefault();
              void update({ name: naming });
            }}
          >
            <Input
              value={naming}
              autoFocus
              maxLength={60}
              aria-label="Name"
              onChange={(e) => setNaming(e.target.value)}
              onKeyDown={(e) => e.key === "Escape" && setNaming(null)}
            />
            <Button type="submit" size="sm" disabled={busy || !naming.trim()}>
              Save
            </Button>
          </form>
        )}
        <span className="change-actions">
          {naming === null && !a.error && (
            <Button
              variant="ghost"
              size="sm"
              aria-label={`Rename ${a.name}`}
              onClick={() => setNaming(a.name)}
            >
              <Pencil size={14} />
            </Button>
          )}
          <Button
            variant="ghost"
            size="sm"
            disabled={!!a.error}
            onClick={() => onOpen(a.dir)}
          >
            Open
          </Button>
        </span>
      </div>
      <div className="assistant-meta">
        {!a.error && (a.agent === "claude" || a.agent === "codex") && (
          <AgentChoice
            value={a.agent}
            onChange={(agent) => !busy && void update({ agent })}
          />
        )}
        <span className="routine-meta">
          <code>{a.dir}/</code>
          {a.sessions &&
            ` · ${a.sessions === "continuous" ? "one ongoing conversation" : "a session per request"}`}
          {a.memory === "wiki" && " · keeps the shared wiki"}
          {a.routines > 0 &&
            ` · ${a.routines} routine${a.routines === 1 ? "" : "s"}`}
        </span>
      </div>
      {error && <p className="routine-meta error-text">{error}</p>}
      {a.error && (
        <p className="routine-meta error-text">assistant.toml: {a.error}</p>
      )}
    </li>
  );
}
