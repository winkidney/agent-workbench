import { useState } from "react";
import { Loader2, X } from "lucide-react";
import { Dialog, DialogContent, DialogTitle } from "../components/ui/dialog";
import { Button } from "../components/ui/button";
import { Input } from "../components/ui/input";
import { api } from "../lib/api";
import { assistantsChanged, MAIN_NAME } from "../lib/assistants";

type Agent = "claude" | "codex";

// Assistants used before they are set up (ADR 0013): what setting up makes,
// the personal assistant's name and agent, and the folder for those who
// look. Confirming sets everything up and opens it.
export function SetUpDialog({
  defaultAgent,
  onClose,
  onDone,
}: {
  defaultAgent: Agent | "";
  onClose: () => void;
  onDone: () => void;
}) {
  const [name, setName] = useState(MAIN_NAME);
  const [agent, setAgent] = useState<Agent>(defaultAgent || "claude");
  const [root, setRoot] = useState("~/assistants");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const shown = name.trim() || MAIN_NAME;
  const submit = async () => {
    setBusy(true);
    setError("");
    try {
      await api("assistants/setup", "POST", { root, agent, name: shown });
      assistantsChanged();
      onDone();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog open onOpenChange={(open) => !open && !busy && onClose()}>
      <DialogContent className="modal modal-content setup-dialog">
        <DialogTitle asChild>
          <h2>Meet {shown}, your personal assistant</h2>
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
            void submit();
          }}
        >
          <ul className="setup-points">
            <li>
              <strong>{shown}</strong> keeps one ongoing conversation with you,
              remembers what matters in a wiki you can read, and checks your
              open items every morning.
            </li>
            <li>
              Ask {shown} to make an assistant for a job you repeat (a weekly
              report, sorting mail): it proposes one, and makes it when you
              approve.
            </li>
            <li>
              Everything is plain files in a git repository on this computer:
              every change can be read and undone.
            </li>
          </ul>
          {!defaultAgent && (
            <p className="error-text" role="alert">
              Neither Claude Code nor Codex is installed. Install one, or point
              to it in Settings → Agents.
            </p>
          )}
          <label className="routine-field">
            <span className="routine-label">Name</span>
            <Input
              value={name}
              maxLength={60}
              onChange={(e) => setName(e.target.value)}
            />
          </label>
          <div className="routine-row">
            <span className="routine-label">Agent</span>
            <div className="agent-switch" role="radiogroup" aria-label="Agent">
              {(["claude", "codex"] as Agent[]).map((a) => (
                <button
                  type="button"
                  role="radio"
                  aria-checked={agent === a}
                  key={a}
                  className={agent === a ? "on" : ""}
                  onClick={() => setAgent(a)}
                >
                  {a === "claude" ? "Claude" : "Codex"}
                </button>
              ))}
            </div>
          </div>
          <details className="setup-folder">
            <summary>Folder</summary>
            <label className="routine-field">
              <span className="routine-label">Repository of assistants</span>
              <Input
                className="routine-mono"
                value={root}
                onChange={(e) => setRoot(e.target.value)}
              />
            </label>
          </details>
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
              Not now
            </Button>
            <Button type="submit" disabled={busy || !root.trim()}>
              {busy && <Loader2 size={14} className="spin" />}
              Set up and open {shown}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}
