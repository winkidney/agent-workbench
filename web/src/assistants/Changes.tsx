import { useState } from "react";
import { BookOpen, Loader2, RotateCcw, Undo2, X } from "lucide-react";
import { api } from "../lib/api";
import {
  changesChanged,
  changeText,
  type AssistantChange,
} from "../lib/assistants";
import { shortTime } from "../routines/when";
import { Button } from "../components/ui/button";

// Undo and Retry for a change; an error shows next to them.
function useAct() {
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const act = (c: AssistantChange, what: "undo" | "retry") => {
    setBusy(c.id);
    setError("");
    api(`assistants/changes/${c.id}/${what}`, "POST")
      .then(changesChanged)
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
      .finally(() => setBusy(""));
  };
  return { busy, error, act };
}

function Actions({
  change: c,
  busy,
  act,
}: {
  change: AssistantChange;
  busy: string;
  act: (c: AssistantChange, what: "undo" | "retry") => void;
}) {
  const working = busy === c.id;
  return (
    <>
      {(c.status === "done" || c.status === "failed") && c.commit && (
        <Button
          size="sm"
          variant="ghost"
          disabled={working}
          onClick={() => act(c, "undo")}
        >
          {working ? (
            <Loader2 size={14} className="spin" />
          ) : (
            <Undo2 size={14} />
          )}
          Undo
        </Button>
      )}
      {c.status === "failed" && c.kind === "ingest" && (
        <Button
          size="sm"
          variant="ghost"
          disabled={working}
          onClick={() => act(c, "retry")}
        >
          <RotateCcw size={14} />
          Retry
        </Button>
      )}
    </>
  );
}

function Files({ files }: { files?: string[] }) {
  if (!files?.length) return null;
  return (
    <details className="change-files">
      <summary>
        {files.length} file{files.length === 1 ? "" : "s"}
      </summary>
      <ul>
        {files.map((f) => (
          <li key={f}>
            <code>{f}</code>
          </li>
        ))}
      </ul>
    </details>
  );
}

// The latest memory update from the open conversation, above the composer
// (ADR 0011); it can be closed until the next one.
export function ChangeBanner({ change }: { change: AssistantChange }) {
  const [closed, setClosed] = useState("");
  const { busy, error, act } = useAct();
  if (closed === change.id + change.status) return null;
  return (
    <div className="archived-banner change-banner" role="status">
      {change.status === "running" ? (
        <Loader2 size={14} className="spin" />
      ) : (
        <BookOpen size={14} />
      )}
      <span>
        {changeText(change)}
        {error && <span className="error-text"> {error}</span>}
      </span>
      <Files files={change.status === "done" ? change.files : undefined} />
      <Actions change={change} busy={busy} act={act} />
      <button
        className="change-close"
        aria-label="Close"
        onClick={() => setClosed(change.id + change.status)}
      >
        <X size={14} />
      </button>
    </div>
  );
}

// Settings → Assistants: what background runs changed lately.
export function ChangeList({
  list,
  inside = false,
}: {
  list: AssistantChange[];
  // Inside a part with its own title.
  inside?: boolean;
}) {
  const { busy, error, act } = useAct();
  if (!list.length) return null;
  const Box = inside ? "div" : "section";
  return (
    <Box className={inside ? undefined : "settings-section"}>
      {!inside && <h3>Recent changes</h3>}
      <p className="subtle">
        What background runs wrote: memory from conversations, and routines with{" "}
        <code>background = true</code>. Each is a git commit; Undo reverts it.
      </p>
      {error && (
        <p className="error-text" role="alert">
          {error}
        </p>
      )}
      <ul className="routine-list">
        {list.map((c) => (
          <li key={c.id} className="routine-item">
            <div className="routine-head">
              <span className="routine-name">{c.title}</span>
              <span className="change-actions">
                <Actions change={c} busy={busy} act={act} />
              </span>
            </div>
            <p className="routine-meta">
              {shortTime(c.started)} · {c.assistant} ·{" "}
              {c.kind === "ingest" ? "from a conversation" : "routine"}
            </p>
            <p className={`routine-meta change-${c.status}`}>{changeText(c)}</p>
            <Files files={c.files} />
          </li>
        ))}
      </ul>
    </Box>
  );
}
