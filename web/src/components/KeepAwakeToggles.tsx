import { Lock, LockOpen, Moon, Sun } from "lucide-react";
import type { ReactNode } from "react";
import { usePreferences, type AwakeItem } from "../composer/suggestion";

// Keeping the computer the workbench runs on awake (server keepawake.go):
// two switches, off unless turned on. While one is on and the workbench
// runs, that computer does not sleep, or does not lock its screen when idle,
// so agents keep a display for browsers and screenshots.

// The line under a switch: what the computer holds now.
export function awakeHint(
  on: boolean,
  item: AwakeItem | undefined,
  offText: string,
): string {
  if (!on) return offText;
  if (item?.error) return item.error;
  if (item?.on) return item.via ? `On · via ${item.via}` : "On";
  return "Taking effect…";
}

const ONLY_HERE = "Applies to the computer Agent Workbench runs on.";

export function KeepAwakeToggles() {
  const { prefs, error, update } = usePreferences();
  const sleep = prefs?.preventSleep ?? false;
  const lock = prefs?.preventLock ?? false;
  const row = (
    label: string,
    title: string,
    on: boolean,
    item: AwakeItem | undefined,
    offText: string,
    icon: ReactNode,
    toggle: () => void,
  ) => {
    const hint = error || (!prefs ? "Loading…" : awakeHint(on, item, offText));
    const problem = !!error || (on && !!item?.error);
    return (
      <button
        type="button"
        className="notify-toggle"
        role="switch"
        aria-checked={on}
        disabled={!prefs}
        title={`${title} ${ONLY_HERE}`}
        onClick={toggle}
      >
        {icon}
        <span className="notify-text">
          <span>{label}</span>
          <small className={problem ? "awake-problem" : undefined}>
            {hint}
          </small>
        </span>
        <span className="notify-state">{on ? "On" : "Off"}</span>
      </button>
    );
  };
  return (
    <>
      {row(
        "Prevent system sleep",
        "While Agent Workbench runs, this computer does not go to sleep.",
        sleep,
        prefs?.awake?.sleep,
        "This computer sleeps as its settings say",
        sleep ? <Sun size={14} /> : <Moon size={14} />,
        () => void update({ preventSleep: !sleep }),
      )}
      {row(
        "Prevent screen lock",
        "While Agent Workbench runs, this computer's screen does not lock or turn off when idle, so agents keep a display.",
        lock,
        prefs?.awake?.lock,
        "The screen locks as this computer's settings say",
        lock ? <LockOpen size={14} /> : <Lock size={14} />,
        () => void update({ preventLock: !lock }),
      )}
    </>
  );
}
