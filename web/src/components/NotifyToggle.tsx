import { Bell, BellOff } from "lucide-react";
import { setNotify, useNotifyState } from "../attention";

const HINT: Record<string, string> = {
  on: "When a session needs you while this page is in the background",
  off: "Off in this browser",
  "needs-permission": "Turn on, then allow notifications in the browser",
  blocked: "Blocked in the browser's site settings",
  unsupported: "Needs https or localhost",
};

// Desktop notifications for sessions that need the user, per browser; in
// the header's ⋯ menu next to the appearance settings.
export function NotifyToggle() {
  const state = useNotifyState();
  const on = state === "on";
  const disabled = state === "unsupported" || state === "blocked";
  return (
    <button
      type="button"
      className="notify-toggle"
      role="switch"
      aria-checked={on}
      disabled={disabled}
      title={HINT[state]}
      onClick={() => void setNotify(!on)}
    >
      {on ? <Bell size={14} /> : <BellOff size={14} />}
      <span className="notify-text">
        <span>Desktop notifications</span>
        <small>{HINT[state]}</small>
      </span>
      <span className="notify-state">{on ? "On" : "Off"}</span>
    </button>
  );
}
