import { useEffect, useState } from "react";
import { FileText, Loader2, X } from "lucide-react";
import { Dialog, DialogContent, DialogTitle } from "../components/ui/dialog";
import { Button } from "../components/ui/button";
import { Input } from "../components/ui/input";
import { Textarea } from "../components/ui/textarea";
import { api } from "../lib/api";
import { NewSessionOptions } from "../AgentControls";
import { OPTION_KEYS, type Options } from "../agentOptions";
import {
  assistantsChanged,
  routinesOf,
  type Assistant,
} from "../lib/assistants";
import type { Routine } from "../routines/when";
import { RoutineLine } from "../routines/RoutineLine";
import { openRoutine } from "../routines/open";

type Agent = "claude" | "codex";

// A table as assistant.toml keeps it: only the keys that are set.
const table = (o?: Options) => {
  const out: Options = {};
  for (const k of OPTION_KEYS) if (o?.[k]) out[k] = o[k];
  return out;
};
const sameTable = (a?: Options, b?: Options) =>
  OPTION_KEYS.every((k) => (a?.[k] ?? "") === (b?.[k] ?? ""));

const lines = (s: string) =>
  s
    .split("\n")
    .map((l) => l.trim())
    .filter(Boolean);

// One assistant's settings (ADR 0015): its home card, agent, topics, on/off,
// routines and files. Saving changes only those lines of its assistant.toml.
export function AssistantSettings({
  assistant: a,
  installed,
  onClose,
  onOpenFile,
}: {
  assistant: Assistant;
  installed?: { claude?: boolean; codex?: boolean };
  onClose: () => void;
  onOpenFile?: (path: string) => void;
}) {
  const [name, setName] = useState(a.name);
  const [description, setDescription] = useState(a.description ?? "");
  const [capabilities, setCapabilities] = useState(
    (a.capabilities ?? []).join("\n"),
  );
  const [examples, setExamples] = useState((a.examples ?? []).join("\n"));
  const [requires, setRequires] = useState((a.requires ?? []).join("\n"));
  const [agent, setAgent] = useState<Agent>(
    a.agent === "codex" ? "codex" : "claude",
  );
  const [sessions, setSessions] = useState<Assistant["sessions"]>(
    a.sessions || "per-task",
  );
  const [enabled, setEnabled] = useState(a.enabled !== false);
  // Its own session settings, per agent (ADR 0017), and what an unset one
  // falls back to (Settings → Agents).
  const [options, setOptions] = useState<Partial<Record<Agent, Options>>>(
    () => ({
      claude: table(a.options?.claude),
      codex: table(a.options?.codex),
    }),
  );
  const [fallbacks, setFallbacks] = useState<Options>({});
  useEffect(() => {
    let live = true;
    setFallbacks({});
    api<Options>(`agents/${agent}/defaults`)
      .then((v) => live && setFallbacks(v))
      .catch(() => {});
    return () => {
      live = false;
    };
  }, [agent]);
  const [routines, setRoutines] = useState<Routine[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const loadRoutines = () =>
    api<Routine[]>("routines")
      .then((list) => setRoutines(routinesOf(list, a.dir)))
      .catch(() => setRoutines([]));
  useEffect(() => {
    void loadRoutines();
  }, [a.dir]);
  const save = async () => {
    const patch: Record<string, unknown> = {};
    if (name.trim() !== a.name) patch.name = name.trim();
    if (description.trim() !== (a.description ?? ""))
      patch.description = description.trim();
    if (lines(capabilities).join("\n") !== (a.capabilities ?? []).join("\n"))
      patch.capabilities = lines(capabilities);
    if (lines(examples).join("\n") !== (a.examples ?? []).join("\n"))
      patch.examples = lines(examples);
    if (lines(requires).join("\n") !== (a.requires ?? []).join("\n"))
      patch.requires = lines(requires);
    if (agent !== a.agent) patch.agent = agent;
    if (sessions !== a.sessions) patch.sessions = sessions;
    if (enabled !== (a.enabled !== false)) patch.enabled = enabled;
    const changedOptions: Partial<Record<Agent, Options>> = {};
    for (const v of ["claude", "codex"] as Agent[])
      if (!sameTable(options[v], a.options?.[v]))
        changedOptions[v] = table(options[v]);
    if (Object.keys(changedOptions).length) patch.options = changedOptions;
    if (!Object.keys(patch).length) return onClose();
    setBusy(true);
    setError("");
    try {
      await api(`assistants/${a.dir}`, "PATCH", patch);
      assistantsChanged();
      onClose();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };
  const main = a.dir === "main";
  return (
    <Dialog open onOpenChange={(open) => !open && !busy && onClose()}>
      <DialogContent className="modal modal-content setup-dialog assistant-settings">
        <DialogTitle asChild>
          <h2>{a.name}: settings</h2>
        </DialogTitle>
        <button
          className="modal-close"
          aria-label="Close"
          disabled={busy}
          onClick={onClose}
        >
          <X size={18} />
        </button>
        <form
          className="assistant-form"
          onSubmit={(e) => {
            e.preventDefault();
            void save();
          }}
        >
          <label className="routine-field">
            <span className="routine-label">Name</span>
            <Input
              value={name}
              maxLength={60}
              onChange={(e) => setName(e.target.value)}
            />
          </label>
          <label className="routine-field">
            <span className="routine-label">What it does (one line)</span>
            <Input
              value={description}
              maxLength={200}
              onChange={(e) => setDescription(e.target.value)}
            />
          </label>
          <label className="routine-field">
            <span className="routine-label">What it can do (one per line)</span>
            <Textarea
              rows={4}
              value={capabilities}
              onChange={(e) => setCapabilities(e.target.value)}
            />
          </label>
          <label className="routine-field">
            <span className="routine-label">
              Requests to try (one per line)
            </span>
            <Textarea
              rows={3}
              value={examples}
              onChange={(e) => setExamples(e.target.value)}
            />
          </label>
          <label className="routine-field">
            <span className="routine-label">
              What it needs (one per line: a signed-in CLI, an MCP server, a
              token)
            </span>
            <Textarea
              rows={2}
              value={requires}
              onChange={(e) => setRequires(e.target.value)}
            />
          </label>
          <div className="routine-row">
            <span className="routine-label">Agent</span>
            <div className="agent-switch" role="radiogroup" aria-label="Agent">
              {(["claude", "codex"] as Agent[]).map((v) => (
                <button
                  type="button"
                  role="radio"
                  aria-checked={agent === v}
                  key={v}
                  className={agent === v ? "on" : ""}
                  disabled={installed?.[v] === false && agent !== v}
                  title={installed?.[v] === false ? "Not installed" : undefined}
                  onClick={() => setAgent(v)}
                >
                  {v === "claude" ? "Claude" : "Codex"}
                </button>
              ))}
            </div>
          </div>
          <div className="assistant-settings-block">
            <span className="routine-label">
              Its {agent === "claude" ? "Claude" : "Codex"} sessions start with
            </span>
            <NewSessionOptions
              agent={agent}
              cwd={a.path}
              value={options[agent] ?? {}}
              onChange={(o) => setOptions((x) => ({ ...x, [agent]: o }))}
              defaultsFrom={null}
              fallbacks={fallbacks}
              labelPrefix="Default"
            />
            <span className="subtle assistant-settings-hint">
              "Default" follows Settings → Agents. A request can still choose
              its own; background runs take only the model and effort.
            </span>
          </div>
          <div className="routine-row">
            <span className="routine-label">Topics</span>
            <div className="agent-switch" role="radiogroup" aria-label="Topics">
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
          {!main && (
            <label className="routine-row">
              <input
                type="checkbox"
                checked={enabled}
                onChange={(e) => setEnabled(e.target.checked)}
              />
              <span>
                Turned on (off: hidden under Show more, routines wait)
              </span>
            </label>
          )}
          {routines.length > 0 && (
            <div className="assistant-settings-block">
              <span className="routine-label">Routines</span>
              <ul className="routine-lines">
                {routines.map((r) => (
                  <li key={r.id}>
                    <RoutineLine
                      routine={r}
                      onChanged={() => void loadRoutines()}
                      onOpenRuns={() => {
                        onClose();
                        openRoutine(r.id);
                      }}
                    />
                  </li>
                ))}
              </ul>
            </div>
          )}
          {onOpenFile && (
            <div className="assistant-settings-block">
              <span className="routine-label">Files</span>
              <div className="assistant-home-foot">
                {["AGENTS.md", "assistant.toml", "config.toml"].map((f) => (
                  <Button
                    key={f}
                    type="button"
                    variant="ghost"
                    size="sm"
                    onClick={() => {
                      onClose();
                      onOpenFile(a.path + "/" + f);
                    }}
                  >
                    <FileText size={14} />
                    {f}
                  </Button>
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
              disabled={busy}
              onClick={onClose}
            >
              Cancel
            </Button>
            <Button type="submit" disabled={busy || !name.trim()}>
              {busy && <Loader2 size={14} className="spin" />}
              Save
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}
