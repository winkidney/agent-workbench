import { CornerDownRight, Play, Send, X, Zap } from "lucide-react";
import { uploadUrl } from "./attachments";
import type { ViewItem } from "../transcript/media";

// images: upload ids of the images added to it.
export type Queued = { id: string; text: string; images?: string[] };

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
  onView,
}: {
  items: Queued[];
  paused: boolean;
  running: boolean;
  // Why a message was queued instead of steered.
  note?: string;
  onRemove: (id: string) => void;
  onResume: () => void;
  onSteer: (id: string) => void;
  onView?: (items: ViewItem[], index: number) => void;
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
          {!!q.images?.length && (
            <span className="queue-images">
              {q.images.map((image, i, all) => (
                <button
                  key={image + i}
                  type="button"
                  className="queue-image"
                  aria-label="View image"
                  onClick={() =>
                    onView?.(
                      all.map((id) => ({
                        kind: "image",
                        src: uploadUrl(id),
                        title: "Image",
                      })),
                      i,
                    )
                  }
                >
                  <img src={uploadUrl(image)} alt="" />
                </button>
              ))}
            </span>
          )}
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
