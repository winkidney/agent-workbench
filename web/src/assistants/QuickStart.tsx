import { Sparkles } from "lucide-react";
import { pickStarter, type Starter } from "../lib/assistants";

// A row of ways to start (lib/assistants.ts MAIN_STARTERS): a click puts the
// start of the message in the composer (nothing is sent), or opens the
// routine dialog.
export function QuickStart({
  starters,
  onPick,
}: {
  starters: Starter[];
  onPick: (text: string) => void;
}) {
  return (
    <div className="quick-start" role="group" aria-label="Quick start">
      <Sparkles size={13} aria-hidden className="quick-start-icon" />
      {starters.map((s) => (
        <button
          key={s.label}
          type="button"
          className="quick-start-chip"
          onClick={() => pickStarter(s, onPick)}
        >
          {s.label}
        </button>
      ))}
    </div>
  );
}
