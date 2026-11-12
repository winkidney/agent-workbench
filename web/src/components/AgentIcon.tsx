import { AlertTriangle, BellRing, Hand, Loader2, Monitor } from "lucide-react";
import { ATTENTION_LABEL } from "../attention";

const RAYS = Array.from({ length: 12 }, (_, i) => {
  const a = (i * Math.PI) / 6;
  const r = i % 2 ? 5.4 : 7;
  return {
    x1: 8 + Math.cos(a) * 1.6,
    y1: 8 + Math.sin(a) * 1.6,
    x2: 8 + Math.cos(a) * r,
    y2: 8 + Math.sin(a) * r,
  };
});

// Generic agent marks (not vendor logos): a coral starburst for Claude and a
// terminal prompt tile for Codex.
export function AgentIcon({
  agent,
  size = 16,
  title,
  decorative,
}: {
  agent: string;
  size?: number;
  title?: string;
  // Next to a visible agent name the icon adds nothing for screen readers.
  decorative?: boolean;
}) {
  const name = agent === "codex" ? "Codex" : "Claude";
  return (
    <svg
      className={"agent-icon " + (agent === "codex" ? "codex" : "claude")}
      width={size}
      height={size}
      viewBox="0 0 16 16"
      role={decorative ? undefined : "img"}
      aria-hidden={decorative || undefined}
      aria-label={decorative ? undefined : (title ?? name)}
    >
      {!decorative && <title>{title ?? name}</title>}
      {agent === "codex" ? (
        <>
          <rect
            x="1"
            y="1"
            width="14"
            height="14"
            rx="3.6"
            className="agent-fill"
          />
          <path d="M4.4 5.4 7 8l-2.6 2.6" className="agent-glyph" />
          <path d="M8.6 10.6h3" className="agent-glyph" />
        </>
      ) : (
        RAYS.map((r, i) => <line key={i} {...r} className="agent-ray" />)
      )}
    </svg>
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
  const icon =
    status === "running" ||
    status === "starting" ||
    status === "external_running" ? (
      <Loader2 {...common} className="spin" />
    ) : status === "waiting" ? (
      <Hand {...common} />
    ) : status === "external" ? (
      <Monitor {...common} />
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
