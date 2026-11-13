import { ATTENTION_LABEL } from "../attention";
import {
  ALERT,
  DOT,
  PixelBell,
  PixelChase,
  PixelDots,
  PixelIcon,
  PixelMonitor,
  PixelPulse,
  Pixels,
  PixelSvg,
} from "./pixel";

// Agent marks in pixels, to the sidebar icons' spec (pixel.tsx): 16×16,
// ink 14 wide within the middle 14×14, centred. Claude is the figure Claude
// Code draws on its own welcome screen (" ▐▛███▜▌ / ▝▜█████▛▘ /   ▘▘ ▝▝ "),
// its quarter blocks laid out as pixels with each row doubled, as a terminal
// cell is twice as tall as wide: eyes, arms and four legs; the arms are a
// pixel shorter than there, to keep within 14. Codex is a slate prompt
// tile. Drawn at 16 or 48.
export const CLAUDE_ICON = [
  "................",
  "................",
  "................",
  "..CCCCCCCCCCCC..",
  "..CCCCCCCCCCCC..",
  "..CC.CCCCCC.CC..",
  "..CC.CCCCCC.CC..",
  ".CCCCCCCCCCCCCC.",
  ".CCCCCCCCCCCCCC.",
  "..CCCCCCCCCCCC..",
  "..CCCCCCCCCCCC..",
  "...C.C....C.C...",
  "...C.C....C.C...",
  "................",
  "................",
  "................",
];
export const CODEX_ICON = [
  "................",
  "..XXXXXXXXXXXX..",
  ".XXXXXXXXXXXXXX.",
  ".XXXXXXXXXXXXXX.",
  ".XXXXXXXXXXXXXX.",
  ".XXXooXXXXXXXXX.",
  ".XXXXooXXXXXXXX.",
  ".XXXXXooXXXXXXX.",
  ".XXXXooXXXXXXXX.",
  ".XXXooXXooooXXX.",
  ".XXXXXXXXXXXXXX.",
  ".XXXXXXXXXXXXXX.",
  ".XXXXXXXXXXXXXX.",
  ".XXXXXXXXXXXXXX.",
  "..XXXXXXXXXXXX..",
  "................",
];
const AGENT_COLORS = { C: "var(--agent-claude)", X: "var(--agent-codex)" };

export function AgentIcon({
  agent,
  size = 16,
  title,
  decorative,
}: {
  agent: string;
  // A multiple of 16 keeps the pixels sharp.
  size?: number;
  title?: string;
  // Next to a visible agent name the icon adds nothing for screen readers.
  decorative?: boolean;
}) {
  const codex = agent === "codex";
  const name = codex ? "Codex" : "Claude";
  return (
    <PixelSvg
      w={16}
      h={16}
      scale={size / 16}
      label={decorative ? undefined : (title ?? name)}
      className={"agent-icon " + (codex ? "codex" : "claude")}
    >
      <Pixels grid={codex ? CODEX_ICON : CLAUDE_ICON} palette={AGENT_COLORS} />
    </PixelSvg>
  );
}

export const statusLabel: Record<string, string> = {
  running: "Running",
  starting: "Connecting",
  waiting: "Awaiting approval",
  external: "Active elsewhere",
  external_running: "Running in another client",
  error: "Error",
  idle: "Engine loaded",
};

// What a session is doing right now; null when it is asleep or stopped. A
// session that waits for the user (attention) says so instead, loudly. All
// are pixel icons to the sidebar's spec (pixel.tsx); only work in progress
// keeps moving.
export function StatusIcon({
  status,
  attention,
  background,
}: {
  status: string;
  attention?: string;
  // Tasks or processes running outside the turns.
  background?: number;
}) {
  // A pending request always comes with its kind; "waiting" alone is the
  // moment before the kind arrives.
  const wants = attention
    ? ATTENTION_LABEL[attention]
    : status === "waiting"
      ? statusLabel.waiting
      : undefined;
  if (wants)
    return (
      // Not the kind as a class: .approval and .question style the cards.
      <span className="status-icon needs-you" title={wants}>
        <PixelBell label={wants} />
      </span>
    );
  // Between turns, work in the background: how much, then a pulse, which
  // stays at the column's edge like every other icon.
  if (background && !["running", "starting"].includes(status)) {
    const label =
      background === 1 ? "1 background task" : `${background} background tasks`;
    return (
      <span
        className="status-icon background"
        title={label}
        role="img"
        aria-label={label}
      >
        {background > 1 && <span className="bg-count">{background}</span>}
        <PixelPulse />
      </span>
    );
  }
  const label = statusLabel[status];
  if (!label) return null;
  const icon =
    status === "running" ? (
      <PixelChase label={label} />
    ) : status === "starting" ? (
      <PixelDots label={label} />
    ) : status === "external" || status === "external_running" ? (
      <PixelMonitor label={label} busy={status === "external_running"} />
    ) : status === "error" ? (
      <PixelIcon grid={ALERT} label={label} />
    ) : (
      <PixelIcon grid={DOT} label={label} />
    );
  return (
    <span className={"status-icon " + status} title={label}>
      {icon}
    </span>
  );
}
