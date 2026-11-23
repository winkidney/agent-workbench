import { useEffect, useState } from "react";
import { Archive, Trash2 } from "lucide-react";
import { Dialog, DialogContent, DialogTitle } from "../components/ui/dialog";
import { Button } from "../components/ui/button";
import { api } from "../lib/api";
import type { FolderOp } from "./Sidebar";

export type FolderTarget = { path: string; name: string; op: FolderOp };

type Skip = { id: string; title: string; reason: string };
// What archiving or deleting a folder's sessions does (archive.go).
type Result = {
  codex: number;
  // Of those, codex exec runs (listed under Other).
  automated: number;
  claude: number;
  skipped: Skip[];
  failed: Skip[];
};

const count = (n: number, one: string) =>
  `${n.toLocaleString()} ${one}${n === 1 ? "" : "s"}`;

// Confirms archiving or deleting every session in a project folder and its
// folders: it shows what will happen first (a dry run), then what did.
export function FolderDialog({
  target,
  onClose,
  onDone,
}: {
  target: FolderTarget;
  onClose: () => void;
  onDone: () => void;
}) {
  const { path, name, op } = target;
  const [plan, setPlan] = useState<Result | null>(null);
  const [done, setDone] = useState<Result | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  useEffect(() => {
    let live = true;
    api<Result>("folders/sessions", "POST", { path, op, dryRun: true })
      .then((r) => live && setPlan(r))
      .catch((e) => live && setError(String(e?.message ?? e)));
    return () => {
      live = false;
    };
  }, [path, op]);
  const run = async () => {
    setBusy(true);
    setError("");
    try {
      setDone(await api<Result>("folders/sessions", "POST", { path, op }));
      onDone();
    } catch (e) {
      setError(String((e as Error)?.message ?? e));
    } finally {
      setBusy(false);
    }
  };
  const verb = op === "delete" ? "Delete" : "Archive";
  const nothing = plan && plan.codex === 0 && plan.claude === 0;
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !busy) onClose();
      }}
    >
      <DialogContent className="modal modal-content confirm-dialog">
        <DialogTitle asChild>
          <h2>
            {verb} all sessions in {name}?
          </h2>
        </DialogTitle>
        <p className="subtle" title={path}>
          {path} and every folder under it. Related sessions started elsewhere
          are not included.
        </p>
        {!plan && !error && <p>Counting sessions…</p>}
        {plan && !done && (
          <>
            {nothing ? (
              <p>There is nothing to {op} here.</p>
            ) : (
              <ul className="folder-plan">
                {plan.codex > 0 && (
                  <li>
                    {count(plan.codex, "Codex session")}
                    {plan.automated > 0 &&
                      ` (${plan.automated.toLocaleString()} of them codex exec runs, listed under Other)`}{" "}
                    will be{" "}
                    {op === "delete"
                      ? "deleted from Codex, including the Codex app and CLI. This cannot be undone."
                      : "archived (Archived at the bottom of the sidebar brings them back)."}
                  </li>
                )}
                {plan.claude > 0 && (
                  <li>
                    {count(plan.claude, "Claude session")} will be hidden from
                    the sidebar: Claude Code cannot archive or delete them, and
                    their records stay as they are.
                  </li>
                )}
              </ul>
            )}
            {plan.skipped.length > 0 && (
              <p className="subtle">
                {count(plan.skipped.length, "session")} at work or open in
                another client will be skipped.
              </p>
            )}
          </>
        )}
        {done && (
          <>
            <p>
              {done.codex > 0 &&
                `${op === "delete" ? "Deleted" : "Archived"} ${count(done.codex, "Codex session")}${done.automated > 0 ? ` (${done.automated.toLocaleString()} codex exec runs)` : ""}. `}
              {done.claude > 0 &&
                `Hid ${count(done.claude, "Claude session")}. `}
              {done.skipped.length > 0 &&
                `Skipped ${count(done.skipped.length, "session")} at work or in another client.`}
              {done.codex + done.claude + done.skipped.length === 0 &&
                "Nothing changed."}
            </p>
            {done.failed.length > 0 && (
              <>
                <p className="error-text">
                  {count(done.failed.length, "session")} could not be{" "}
                  {op === "delete" ? "deleted" : "archived"}:
                </p>
                <ul className="folder-plan">
                  {done.failed.slice(0, 10).map((f) => (
                    <li key={f.id}>
                      {f.title || f.id}: {f.reason}
                    </li>
                  ))}
                  {done.failed.length > 10 && (
                    <li>and {done.failed.length - 10} more</li>
                  )}
                </ul>
              </>
            )}
          </>
        )}
        {error && <p className="error-text">{error}</p>}
        <div className="confirm-actions">
          {done ? (
            <Button onClick={onClose}>Close</Button>
          ) : (
            <>
              <Button variant="ghost" disabled={busy} onClick={onClose}>
                Cancel
              </Button>
              <Button
                className={op === "delete" ? "danger-button" : undefined}
                disabled={busy || !plan || !!nothing}
                onClick={() => void run()}
              >
                {op === "delete" ? <Trash2 size={14} /> : <Archive size={14} />}
                {busy ? (op === "delete" ? "Deleting…" : "Archiving…") : verb}
              </Button>
            </>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}
