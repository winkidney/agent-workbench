import { Lightbulb, LightbulbOff } from "lucide-react";
import { usePreferences } from "../composer/suggestion";

// Prompt suggestions for every session, in the header's ⋯ menu next to the
// notifications. Off, Claude Code makes none (no extra request per turn).
export function SuggestToggle() {
  const { prefs, error, update } = usePreferences();
  const on = prefs?.suggestions ?? false;
  const hint = error
    ? error
    : !prefs
      ? "Loading…"
      : on
        ? "After a Claude turn; Tab fills it in"
        : "Off for all sessions";
  return (
    <button
      type="button"
      className="notify-toggle"
      role="switch"
      aria-checked={on}
      disabled={!prefs}
      title="After each turn, Claude Code predicts what you would type next (one more request on the cached conversation). Codex sessions don't have this yet."
      onClick={() => void update({ suggestions: !on })}
    >
      {on ? <Lightbulb size={14} /> : <LightbulbOff size={14} />}
      <span className="notify-text">
        <span>Suggest next prompt</span>
        <small>{hint}</small>
      </span>
      <span className="notify-state">{on ? "On" : "Off"}</span>
    </button>
  );
}
