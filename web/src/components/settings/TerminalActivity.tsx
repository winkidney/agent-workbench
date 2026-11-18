import { useEffect, useState } from "react";
import { ChevronRight } from "lucide-react";
import { api } from "../../lib/api";

type Entry = {
  time: string;
  session: string;
  terminal: string;
  title: string;
  action: string;
  device: string;
};

// Who opened and ended terminals, the last week (terminal.go): when, from
// which device, in which session. Never what a terminal showed or was sent.
// Collapsed at first; the record is fetched when opened and let go when
// closed again.
export function TerminalActivity() {
  const [open, setOpen] = useState(false);
  const [list, setList] = useState<Entry[] | null>(null);
  const [error, setError] = useState("");
  useEffect(() => {
    if (!open) {
      setList(null);
      setError("");
      return;
    }
    let live = true;
    api<Entry[]>("terminal-audit")
      .then((v) => live && setList(v))
      .catch(
        (e) => live && setError(e instanceof Error ? e.message : String(e)),
      );
    return () => {
      live = false;
    };
  }, [open]);
  return (
    <details
      className="settings-section terminal-activity"
      open={open}
      onToggle={(e) => setOpen(e.currentTarget.open)}
    >
      <summary>
        <ChevronRight size={14} aria-hidden />
        <h3>Terminal activity</h3>
      </summary>
      <p className="subtle">
        Terminals opened and ended in the last 7 days, newest first. What they
        showed or were sent is never kept.
      </p>
      {error && (
        <p role="alert" className="error-text">
          {error}
        </p>
      )}
      {open && !list && !error && <p className="subtle">Loading…</p>}
      {list && !list.length && <p className="subtle">No terminals yet.</p>}
      {!!list?.length && (
        <ul>
          {list.map((e, i) => (
            <li key={i}>
              <time dateTime={e.time}>{new Date(e.time).toLocaleString()}</time>
              <span>
                {e.title} {e.action}
              </span>
              <span className="subtle">
                {e.device} · {e.session}
              </span>
            </li>
          ))}
        </ul>
      )}
    </details>
  );
}
