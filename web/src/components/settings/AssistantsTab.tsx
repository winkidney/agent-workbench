import { useState } from "react";
import {
  ChevronDown,
  ChevronRight,
  Loader2,
  PackagePlus,
  Pencil,
  Plus,
} from "lucide-react";
import { api } from "../../lib/api";
import { ChangeList } from "../../assistants/lazy";
import {
  assistantsChanged,
  browseCatalog,
  folderFor,
  LANGUAGES,
  MAIN_NAME,
  openAssistantSettings,
  openSessionLater,
  reloadAssistants,
  setUpAssistants,
  useChanges,
  type Assistant,
  type Assistants,
  type Language,
  type SyncState,
} from "../../lib/assistants";
import { shortTime } from "../../routines/when";
import { CatalogSection } from "../../assistants/CatalogSection";
import { Button } from "../ui/button";
import { Input } from "../ui/input";
import { SettingsDisclosure } from "./SettingsDisclosure";

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
        {!adding && (
          <div className="assistants-actions">
            <Button variant="outline" size="sm" onClick={() => setAdding(true)}>
              <Plus size={14} />
              New assistant
            </Button>
            <Button variant="outline" size="sm" onClick={browseCatalog}>
              <PackagePlus size={14} />
              Add from a catalog
            </Button>
          </div>
        )}
        {adding && (
          <form
            className="assistant-form"
            onSubmit={(e) => {
              e.preventDefault();
              let session = "";
              void run(async () => {
                const out = await api<{ session?: string }>(
                  "assistants",
                  "POST",
                  { name, dir: folder || folderFor(name), agent, sessions },
                );
                session = out.session ?? "";
              }).then((ok) => {
                if (!ok) return;
                setAdding(false);
                setName("");
                setFolder("");
                // Its first conversation asks what it is for.
                if (session) openSessionLater(session);
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
        <ul className="routine-list">
          {value.assistants.map((a) => (
            <AssistantItem key={a.dir} assistant={a} onOpen={onOpen} />
          ))}
        </ul>
        <SettingsDisclosure
          id="assistant-files"
          small
          title="About these files"
        >
          <p className="subtle">
            In <code>{value.root}</code>. Each folder's <code>AGENTS.md</code>{" "}
            is its procedure and <code>assistant.toml</code> its settings and
            routines, from its author; your own choices, routines and card text
            are in <code>settings.toml</code>, and your additions to the
            procedure in <code>AGENTS.local.md</code>. Edit them there, or ask
            the assistant to.
          </p>
          <div>
            <Button
              variant="ghost"
              size="sm"
              title="Read the assistant.toml files again, after editing them by hand"
              onClick={() => void reloadAssistants()}
            >
              Reload assistants
            </Button>
          </div>
        </SettingsDisclosure>
      </section>
      <CatalogSection />
      {!!value.mossUpdates?.length && (
        <section className="settings-section" role="status">
          <h3>A newer Moss</h3>
          <p className="subtle">
            This version of Agent Workbench brings a newer {MAIN_NAME} for files
            you changed: {value.mossUpdates.map((f) => f).join(", ")}.
          </p>
          <div className="confirm-actions">
            <Button
              variant="ghost"
              size="sm"
              onClick={() =>
                void api("assistants/moss", "POST", { take: false }).then(
                  assistantsChanged,
                )
              }
            >
              Keep mine
            </Button>
            <Button
              size="sm"
              onClick={() =>
                void api("assistants/moss", "POST", { take: true }).then(
                  assistantsChanged,
                )
              }
            >
              Use the new version
            </Button>
          </div>
        </section>
      )}
      <LanguageSection value={value.language ?? "zh-Hans"} />
      <BackupSection root={value.root} sync={value.sync} />
      {changes.length > 0 && (
        <SettingsDisclosure
          id="assistant-changes"
          title="Recent changes"
          note={String(changes.length)}
        >
          <ChangeList list={changes} inside />
        </SettingsDisclosure>
      )}
    </>
  );
}

// One assistant: open it, rename it, switch its agent (PATCH
// /api/assistants/{dir}). What is used most shows; the rest unfolds.
function AssistantItem({
  assistant: a,
  onOpen,
}: {
  assistant: Assistant;
  onOpen: (dir: string) => void;
}) {
  const [naming, setNaming] = useState<string | null>(null);
  const [more, setMore] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const update = async (change: {
    name?: string;
    agent?: Agent;
    enabled?: boolean;
  }) => {
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
  const agentOK = !a.error && (a.agent === "claude" || a.agent === "codex");
  return (
    <li className="routine-item">
      <div className="routine-head">
        {naming === null ? (
          <button
            type="button"
            className="routine-toggle"
            aria-expanded={more}
            aria-controls={`assistant-more-${a.dir}`}
            onClick={() => setMore(!more)}
          >
            {more ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
            <span className="routine-name">
              {a.name}
              {a.dir === "main" && (
                <span className="subtle"> · your personal assistant</span>
              )}
            </span>
            {agentOK && (
              <span className="assistant-agent-mark">
                {a.agent === "claude" ? "Claude" : "Codex"}
              </span>
            )}
          </button>
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
          {a.dir !== "main" && !a.error && (
            <button
              type="button"
              role="switch"
              aria-checked={a.enabled !== false}
              aria-label={`${a.name} on`}
              className="routine-switch"
              disabled={busy}
              onClick={() => void update({ enabled: a.enabled === false })}
            >
              {a.enabled === false ? "Off" : "On"}
            </button>
          )}
          {!a.error && (
            <Button
              variant="ghost"
              size="sm"
              onClick={() => openAssistantSettings(a.dir)}
            >
              Settings
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
      {more && (
        <div className="assistant-more" id={`assistant-more-${a.dir}`}>
          {agentOK && (
            <AgentChoice
              value={a.agent as Agent}
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
          {naming === null && !a.error && (
            <Button
              variant="ghost"
              size="sm"
              aria-label={`Rename ${a.name}`}
              onClick={() => setNaming(a.name)}
            >
              <Pencil size={14} />
              Rename
            </Button>
          )}
          {a.dir === "main" && !a.error && (
            <Button
              variant="ghost"
              size="sm"
              disabled={busy}
              title="Send the welcome again"
              onClick={() => {
                setBusy(true);
                setError("");
                api<{ session: string }>("assistants/main/welcome", "POST")
                  .then((out) => openSessionLater(out.session))
                  .catch((e) =>
                    setError(e instanceof Error ? e.message : String(e)),
                  )
                  .finally(() => setBusy(false));
              }}
            >
              Welcome
            </Button>
          )}
        </div>
      )}
      {error && <p className="routine-meta error-text">{error}</p>}
      {a.error && (
        <p className="routine-meta error-text">assistant.toml: {a.error}</p>
      )}
    </li>
  );
}

// The language assistants write to the user in (PATCH settings).
function LanguageSection({ value }: { value: Language }) {
  const [error, setError] = useState("");
  return (
    <section className="settings-section language-row">
      <h3>Language</h3>
      <div
        className="agent-switch"
        role="radiogroup"
        aria-label="Language"
        aria-describedby="language-note"
      >
        {LANGUAGES.map(([v, label]) => (
          <button
            type="button"
            role="radio"
            aria-checked={value === v}
            key={v}
            className={value === v ? "on" : ""}
            onClick={() =>
              api("assistants/settings", "PATCH", { language: v })
                .then(() => {
                  setError("");
                  assistantsChanged();
                })
                .catch((e) =>
                  setError(e instanceof Error ? e.message : String(e)),
                )
            }
          >
            {label}
          </button>
        ))}
      </div>
      <span id="language-note" className="subtle">
        What assistants write to you in, and the welcome
      </span>
      {error && <p className="error-text">{error}</p>}
    </section>
  );
}

// Where the repository of assistants is backed up: its remote, what is not
// pushed yet; a private GitHub repository made with gh when there is none.
function BackupSection({ root, sync }: { root: string; sync?: SyncState }) {
  const [name, setName] = useState(() => root.split("/").pop() || "assistants");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const act = (path: string, body?: unknown) => {
    setBusy(true);
    setError("");
    api(path, "POST", body)
      .then(assistantsChanged)
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
      .finally(() => setBusy(false));
  };
  const note = !sync
    ? undefined
    : !sync.remote
      ? "Not backed up"
      : sync.error
        ? "Push failed"
        : sync.ahead > 0
          ? `${sync.ahead} not pushed`
          : sync.ahead === 0
            ? "Up to date"
            : "Not pushed yet";
  return (
    <SettingsDisclosure
      id="assistant-backup"
      title="Backup"
      note={note}
      // What needs the user shows by itself.
      force={!!sync?.remote && (!!sync.error || sync.ahead !== 0)}
    >
      {sync?.remote ? (
        <>
          <p className="subtle">
            Changes are pushed to <code>{sync.remote}</code> within a few
            minutes of each commit.{" "}
            {sync.ahead > 0
              ? `${sync.ahead} commit${sync.ahead === 1 ? "" : "s"} not pushed yet.`
              : sync.ahead === 0
                ? "Up to date."
                : "Not pushed yet."}
            {sync.pushed && ` Last push ${shortTime(sync.pushed)}.`}
          </p>
          {sync.error && <p className="error-text">{sync.error}</p>}
          <div>
            <Button
              variant="outline"
              size="sm"
              disabled={busy}
              onClick={() => act("assistants/sync")}
            >
              {busy && <Loader2 size={14} className="spin" />}
              Push now
            </Button>
          </div>
        </>
      ) : (
        <>
          <p className="subtle">
            Back the repository up to a private GitHub repository, to keep it
            safe and use it on other computers. Uses the GitHub CLI (gh), signed
            in as you.
          </p>
          <form
            className="assistant-rename"
            onSubmit={(e) => {
              e.preventDefault();
              act("assistants/github", { name });
            }}
          >
            <Input
              value={name}
              aria-label="Repository name"
              onChange={(e) => setName(e.target.value)}
            />
            <Button type="submit" size="sm" disabled={busy || !name.trim()}>
              {busy && <Loader2 size={14} className="spin" />}
              Create private repository
            </Button>
          </form>
        </>
      )}
      {error && <p className="error-text">{error}</p>}
    </SettingsDisclosure>
  );
}
