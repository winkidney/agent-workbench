import { AgentIcon } from "../components/AgentIcon";
import type { RecordRemains } from "./useTranscript";

// A Codex thread whose record is gone: Codex still lists it, but there is
// nothing to show or continue. What its index keeps is shown instead.
export function RecordMissing({ remains }: { remains: RecordRemains }) {
  const created = remains.created ? new Date(remains.created) : null;
  return (
    <div className="empty-conversation record-missing">
      <AgentIcon agent="codex" size={48} />
      <h1>This conversation's record is gone.</h1>
      <p>
        Codex still lists it, but its record file no longer exists, so it can't
        be shown or continued.
        {created && ` It was started on ${created.toLocaleDateString()}.`}
      </p>
      {remains.firstMessage && (
        <blockquote className="record-first-message">
          {remains.firstMessage}
        </blockquote>
      )}
      {remains.tokensUsed ? (
        <p className="subtle">
          {remains.tokensUsed.toLocaleString()} tokens were used.
        </p>
      ) : null}
    </div>
  );
}
