import { CornerDownRight, Play, Send, X, Zap } from "lucide-react";

export type Queued = { id: string; text: string };

// Messages waiting for the running turn to end, above the composer. Each can
// be steered into that turn now, or sent now once no turn runs.
export function QueueList({
  items,
  paused,
  running,
  note,
  onRemove,
  onResume,
  onSteer,
}: {
  items: Queued[];
  paused: boolean;
  running: boolean;
  // Why a message was queued instead of steered.
  note?: string;
  onRemove: (id: string) => void;
  onResume: () => void;
  onSteer: (id: string) => void;
}) {
  if (!items.length) return null;
  const steer = running && !paused;
  return (
    <div className="queue" aria-label="Queued messages">
      <div className="queue-head">
        <span>
          {paused
            ? "Queued · paused after interrupt"
            : "Queued · sends when this turn ends"}
          {note && <span className="queue-note"> · {note}</span>}
        </span>
        {paused && (
          <button type="button" className="queue-resume" onClick={onResume}>
            <Play size={11} /> Send next
          </button>
        )}
      </div>
      {items.map((q) => (
        <div key={q.id} className="queue-item">
          <CornerDownRight size={12} />
          <span className="queue-text">{q.text}</span>
          <button
            type="button"
            className="queue-steer"
            title={steer ? "Send into the running turn now" : "Send now"}
            onClick={() => onSteer(q.id)}
          >
            {steer ? <Zap size={11} /> : <Send size={11} />}
            {steer ? "Steer" : "Send now"}
          </button>
          <button
            type="button"
            aria-label="Remove queued message"
            title="Remove"
            onClick={() => onRemove(q.id)}
          >
            <X size={12} />
          </button>
        </div>
      ))}
    </div>
  );
}
