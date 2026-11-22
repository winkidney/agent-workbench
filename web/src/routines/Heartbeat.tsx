import { useState, type CSSProperties, type SyntheticEvent } from "react";
import {
  firstLine,
  heartbeatSummary,
  runDuration,
  runKind,
  runResult,
  shortTime,
  type RoutineRun,
} from "./when";

// A routine's runs as a heartbeat: one bar per run, oldest on the left. Most
// runs have nothing to say (a short grey bar); the ones that sent something,
// failed or did not run stand out. Hovering or focusing a bar says what that
// run did; the full size picks a run to show its output (RoutinesTab.tsx).

// The lines a bar's tip shows.
export function runTip(run: RoutineRun): string[] {
  const lines = [
    [shortTime(run.started), runDuration(run)].filter(Boolean).join(" · "),
    runResult(run),
  ];
  if (run.exit !== undefined && run.exit !== 0) lines.push(`Exit ${run.exit}`);
  // A command that printed says its first line in the result already.
  const out = firstLine(run.output);
  if (out && !lines[1].startsWith("Printed: ")) lines.push(out);
  return lines;
}

export function Heartbeat({
  runs,
  compact = false,
  selected,
  onSelect,
  onOpen,
}: {
  // Newest first, as the server lists them.
  runs: RoutineRun[];
  // Inline in a routine's row: small bars, the whole strip one button.
  compact?: boolean;
  selected?: string;
  // Full size: a bar was picked.
  onSelect?: (run: RoutineRun) => void;
  // Compact: the strip was clicked (without it the strip only shows).
  onOpen?: () => void;
}) {
  const [tip, setTip] = useState<{ run: RoutineRun; at: CSSProperties } | null>(
    null,
  );
  if (!runs.length) return null;
  const order = [...runs].reverse();
  const summary = heartbeatSummary(runs);
  // The tip starts under its bar and opens away from the strip's nearer
  // edge; a compact strip sits at a row's start, so its tips open rightwards.
  const show = (run: RoutineRun, i: number, e: SyntheticEvent<HTMLElement>) => {
    const bar = e.currentTarget;
    const wrap = bar.closest(".heartbeat-wrap");
    const x = bar.offsetLeft;
    const at: CSSProperties =
      compact || !wrap || i < order.length / 2
        ? { left: Math.max(0, x - 8) }
        : { right: Math.max(0, wrap.clientWidth - x - bar.offsetWidth - 8) };
    setTip({ run, at });
  };
  const hide = () => setTip(null);
  const bars = order.map((run, i) => {
    const kind = runKind(run);
    const cls = `heartbeat-bar ${kind}${run.id === selected ? " selected" : ""}`;
    if (compact)
      return (
        <span
          key={run.id}
          className={cls}
          onMouseEnter={(e) => show(run, i, e)}
          onMouseLeave={hide}
        />
      );
    return (
      <button
        key={run.id}
        type="button"
        className={cls}
        aria-label={`${shortTime(run.started)} · ${runResult(run)}`}
        aria-pressed={run.id === selected}
        onMouseEnter={(e) => show(run, i, e)}
        onMouseLeave={hide}
        onFocus={(e) => show(run, i, e)}
        onBlur={hide}
        onClick={() => onSelect?.(run)}
      />
    );
  });
  const tipBox = tip && (
    <span className="heartbeat-tip" role="tooltip" style={tip.at}>
      {runTip(tip.run).map((line, i) => (
        <span key={i} className={i === 0 ? "heartbeat-tip-time" : undefined}>
          {line}
        </span>
      ))}
    </span>
  );
  if (compact)
    return (
      <span className="heartbeat-wrap">
        {onOpen ? (
          <button
            type="button"
            className="heartbeat compact"
            aria-label={`${summary}. Show the runs`}
            onClick={onOpen}
          >
            {bars}
          </button>
        ) : (
          <span className="heartbeat compact" role="img" aria-label={summary}>
            {bars}
          </span>
        )}
        {tipBox}
      </span>
    );
  return (
    <div className="heartbeat-wrap">
      <div className="heartbeat" role="group" aria-label={summary}>
        {bars}
      </div>
      {tipBox}
    </div>
  );
}
