import { useState } from "react";
import { Loader2, X } from "lucide-react";
import { Dialog, DialogContent, DialogTitle } from "../components/ui/dialog";
import { Button } from "../components/ui/button";
import { Input } from "../components/ui/input";
import { Textarea } from "../components/ui/textarea";
import { AgentIcon } from "../components/AgentIcon";
import { ProjectPicker } from "../newsession/ProjectPicker";
import type { RecentProject } from "../newsession/recent";
import { api } from "../lib/api";
import { DAYS, routineOf, type Draft, type Routine } from "./when";
import { setUpAssistants, type Assistant } from "../lib/assistants";

// One small segmented choice, styled like the new-session agent switch.
function Choice<T extends string>({
  label,
  value,
  options,
  onChange,
}: {
  label: string;
  value: T;
  options: [T, string][];
  onChange: (v: T) => void;
}) {
  return (
    <div className="agent-switch" role="radiogroup" aria-label={label}>
      {options.map(([v, text]) => (
        <button
          type="button"
          role="radio"
          aria-checked={value === v}
          key={v}
          className={value === v ? "on" : ""}
          onClick={() => onChange(v)}
        >
          {text}
        </button>
      ))}
    </div>
  );
}

// Make or change a routine: what it does, where it goes, and when.
export function RoutineDialog({
  draft: initial,
  recent,
  assistants,
  sessionTitle,
  onClose,
  onSaved,
}: {
  draft: Draft;
  recent: RecentProject[];
  assistants: Assistant[];
  sessionTitle: (id: string) => string;
  onClose: () => void;
  onSaved?: (r: Routine) => void;
}) {
  const [d, setD] = useState<Draft>(initial);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const set = (patch: Partial<Draft>) => {
    setD((x) => ({ ...x, ...patch }));
    setError("");
  };
  const sends = d.what === "message" || d.handOff;
  const save = async () => {
    const out = routineOf(d);
    if ("error" in out) return setError(out.error);
    setBusy(true);
    try {
      const saved = d.id
        ? await api<Routine>(`routines/${d.id}`, "PUT", out.routine)
        : await api<Routine>("routines", "POST", out.routine);
      onSaved?.(saved);
      onClose();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog open onOpenChange={(open) => !open && !busy && onClose()}>
      <DialogContent className="modal modal-content routine-dialog">
        <DialogTitle asChild>
          <h2>{d.id ? "Edit routine" : "New routine"}</h2>
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
          className="routine-form"
          onSubmit={(e) => {
            e.preventDefault();
            void save();
          }}
        >
          <div className="routine-row">
            <span className="routine-label">Does</span>
            <Choice
              label="What it does"
              value={d.what}
              options={[
                ["message", "Send a message"],
                ["command", "Run a command"],
              ]}
              onChange={(what) => set({ what })}
            />
          </div>

          {d.what === "command" && (
            <>
              <label className="routine-field">
                <span className="routine-label">Command</span>
                <Input
                  className="routine-mono"
                  value={d.run}
                  placeholder="uv run tools/check.py"
                  onChange={(e) => set({ run: e.target.value })}
                />
              </label>
              <div className="routine-field">
                <span className="routine-label">Runs in</span>
                <ProjectPicker
                  value={d.dir}
                  recent={recent}
                  onChange={(dir) => set({ dir })}
                />
              </div>
              <div className="routine-row">
                <span className="routine-label">Then</span>
                <Choice
                  label="What happens to its output"
                  value={d.handOff ? "send" : "keep"}
                  options={[
                    ["keep", "Keep the output"],
                    ["send", "Send it to an agent"],
                  ]}
                  onChange={(v) => set({ handOff: v === "send" })}
                />
              </div>
              <p className="subtle routine-hint">
                {d.handOff
                  ? "Only when it prints something; it is added to the message below."
                  : "See each run's output in Settings → Routines. A failed run always notifies you."}
              </p>
              {!d.handOff && (
                <label className="routine-check">
                  <input
                    type="checkbox"
                    checked={d.notifyOnOutput}
                    onChange={(e) => set({ notifyOnOutput: e.target.checked })}
                  />
                  Notify me when it prints something
                </label>
              )}
            </>
          )}

          {sends && (
            <>
              <div className="routine-row">
                <span className="routine-label">To</span>
                <Choice
                  label="Where the message goes"
                  value={d.target}
                  options={[
                    ...(d.session
                      ? ([["session", sessionTitle(d.session)]] as [
                          "session",
                          string,
                        ][])
                      : []),
                    ...(assistants.length
                      ? ([["assistant", "An assistant"]] as [
                          "assistant",
                          string,
                        ][])
                      : []),
                    ["new", "A new session each time"],
                  ]}
                  onChange={(target) =>
                    set({
                      target,
                      assistant:
                        target === "assistant" && !d.assistant
                          ? (assistants[0]?.dir ?? "")
                          : d.assistant,
                    })
                  }
                />
              </div>
              {!assistants.length && (
                <div className="routine-row">
                  <span className="subtle">No assistants yet.</span>
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    onClick={() => {
                      onClose();
                      setUpAssistants();
                    }}
                  >
                    Set up assistants…
                  </Button>
                </div>
              )}
              {d.target === "assistant" && (
                <label className="routine-field">
                  <span className="routine-label">Assistant</span>
                  <select
                    className="routine-select"
                    value={d.assistant}
                    onChange={(e) => set({ assistant: e.target.value })}
                  >
                    {assistants.map((a) => (
                      <option key={a.dir} value={a.dir}>
                        {a.name}
                        {a.sessions === "continuous"
                          ? " (its ongoing topic)"
                          : " (a new session each time)"}
                      </option>
                    ))}
                  </select>
                </label>
              )}
              {d.target === "new" && (
                <div className="routine-field">
                  <span className="routine-label">In</span>
                  <div className="routine-inline">
                    <div className="routine-grow">
                      <ProjectPicker
                        value={d.cwd}
                        recent={recent}
                        onChange={(cwd) => set({ cwd })}
                      />
                    </div>
                    <Choice
                      label="Agent"
                      value={d.agent}
                      options={[
                        ["claude", "Claude"],
                        ["codex", "Codex"],
                      ]}
                      onChange={(agent) => set({ agent })}
                    />
                    <AgentIcon agent={d.agent} size={16} decorative />
                  </div>
                </div>
              )}
              <label className="routine-field">
                <span className="routine-label">Message</span>
                <Textarea
                  value={d.message}
                  rows={3}
                  placeholder={
                    d.what === "command"
                      ? "What to do with the output"
                      : "What the agent should do"
                  }
                  onChange={(e) => set({ message: e.target.value })}
                />
              </label>
            </>
          )}

          <div className="routine-row">
            <span className="routine-label">When</span>
            <Choice
              label="When it runs"
              value={d.whenKind}
              options={[
                ["daily", "Daily"],
                ["interval", "Every"],
                ["once", "Once"],
              ]}
              onChange={(whenKind) => set({ whenKind })}
            />
          </div>
          {d.whenKind === "daily" && (
            <div className="routine-inline routine-indent">
              <Input
                type="time"
                className="routine-time"
                value={d.time}
                onChange={(e) => set({ time: e.target.value })}
              />
              <div
                className="routine-days"
                role="group"
                aria-label="Days (none: every day)"
              >
                {DAYS.map((name, i) => (
                  <button
                    type="button"
                    key={name}
                    aria-pressed={d.days.includes(i)}
                    onClick={() =>
                      set({
                        days: d.days.includes(i)
                          ? d.days.filter((x) => x !== i)
                          : [...d.days, i],
                      })
                    }
                  >
                    {name}
                  </button>
                ))}
              </div>
            </div>
          )}
          {d.whenKind === "interval" && (
            <div className="routine-inline routine-indent">
              <Input
                type="number"
                min={1}
                className="routine-number"
                value={d.every}
                onChange={(e) => set({ every: Number(e.target.value) })}
              />
              <Choice
                label="Unit"
                value={d.unit}
                options={[
                  ["minutes", "minutes"],
                  ["hours", "hours"],
                  ["days", "days"],
                ]}
                onChange={(unit) => set({ unit })}
              />
            </div>
          )}
          {d.whenKind === "once" && (
            <div className="routine-inline routine-indent">
              <Input
                type="datetime-local"
                className="routine-datetime"
                value={d.at}
                onChange={(e) => set({ at: e.target.value })}
              />
            </div>
          )}

          <label className="routine-field">
            <span className="routine-label">Name</span>
            <Input
              value={d.name}
              placeholder="Optional; the message or command names it"
              onChange={(e) => set({ name: e.target.value })}
            />
          </label>

          {error && (
            <p className="error-text" role="alert">
              {error}
            </p>
          )}
          <div className="confirm-actions">
            <Button type="button" variant="ghost" onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit" disabled={busy}>
              {busy && <Loader2 size={14} className="spin" />}
              {d.id ? "Save" : "Create routine"}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}
