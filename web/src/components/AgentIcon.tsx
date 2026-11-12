import { AlertTriangle, BellRing, Hand } from "lucide-react";
import { ATTENTION_LABEL } from "../attention";
import { PixelChase, PixelDots, PixelMonitor, Pixels, PixelSvg } from "./pixel";

// Agent marks in pixels. Claude is the figure Claude Code draws on its own
// welcome screen (" ▐▛███▜▌ / ▝▜█████▛▘ /   ▘▘ ▝▝ "), its quarter blocks
// laid out as pixels with each row doubled, as a terminal cell is twice as
// tall as wide: eyes, arms and four legs. Codex is a slate prompt tile.
// 16×16; drawn at 16 or 48.
export const CLAUDE_ICON = [
  "................",
  "................",
  "................",
  "..CCCCCCCCCCCC..",
  "..CCCCCCCCCCCC..",
  "..CC.CCCCCC.CC..",
  "..CC.CCCCCC.CC..",
  "CCCCCCCCCCCCCCCC",
  "CCCCCCCCCCCCCCCC",
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
// session that waits for the user (attention) says so instead, loudly.
export function StatusIcon({
  status,
  attention,
  background,
  size = 13,
}: {
  status: string;
  attention?: string;
  // Tasks or processes running outside the turns.
  background?: number;
  size?: number;
}) {
  const wants = attention ? ATTENTION_LABEL[attention] : undefined;
  if (wants)
    return (
      <span className={"status-icon needs-you " + attention} title={wants}>
        <BellRing size={size} role="img" aria-label={wants} />
      </span>
    );
  // Between turns, work in the background: a ring with how much.
  if (background && !["running", "starting", "waiting"].includes(status)) {
    const label =
      background === 1 ? "1 background task" : `${background} background tasks`;
    return (
      <span
        className="status-icon background"
        title={label}
        role="img"
        aria-label={label}
      >
        <span className="bg-ring" aria-hidden />
        {background > 1 && <span className="bg-count">{background}</span>}
      </span>
    );
  }
  const label = statusLabel[status];
  if (!label) return null;
  const common = { size, role: "img", "aria-label": label } as const;
  // Pixel animations for work in progress, here or in another client.
  const icon =
    status === "running" ? (
      <PixelChase label={label} />
    ) : status === "starting" ? (
      <PixelDots label={label} />
    ) : status === "external" || status === "external_running" ? (
      <PixelMonitor label={label} busy={status === "external_running"} />
    ) : status === "waiting" ? (
      <Hand {...common} />
    ) : status === "error" ? (
      <AlertTriangle {...common} />
    ) : (
      <span className="status-idle-dot" role="img" aria-label={label} />
    );
  return (
    <span className={"status-icon " + status} title={label}>
      {icon}
    </span>
  );
}
