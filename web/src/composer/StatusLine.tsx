import { useEffect, useState } from "react";
import { formatDuration } from "../transcript/Timeline";
import { toolTitle } from "../transcript/summarize";
import type { State } from "../transcript/reducer";

// What the agent is doing right now, derived from the newest live block.
export function activity(state: State, status: string): string {
  if (status === "waiting") return "Awaiting approval";
  if (status === "starting") return "Starting engine";
  if (status === "stopping") return "Stopping";
  for (
    let i = state.keys.length - 1;
    i >= Math.max(0, state.keys.length - 30);
    i--
  ) {
    const b = state.blocks.get(state.keys[i])!;
    if (b.tool?.status === "running") {
      const { name, arg } = toolTitle(b.tool);
      return `Running ${name}${arg ? `(${arg.length > 48 ? arg.slice(0, 47) + "…" : arg})` : ""}`;
    }
    if (b.streaming) return b.kind === "thinking" ? "Thinking" : "Responding";
    if (b.kind === "user") break;
  }
  return "Working";
}

// What the turn is doing between visible output, from the server's telemetry.
const PHASE: Record<string, string> = {
  thinking: "Thinking",
  responding: "Responding",
  tool: "Running a tool",
  requesting: "Waiting for the model",
  compacting: "Compacting conversation",
};
// A specific verb (a running tool, streaming text) wins over the phase;
// waiting for MCP servers is shown whatever the local guess.
export const statusVerb = (
  verb: string,
  phase: string,
  mcp?: { done: number; total: number },
) =>
  phase === "mcp" && mcp?.total
    ? `Starting MCP servers (${mcp.done}/${mcp.total})`
    : verb === "Working" && PHASE[phase]
      ? PHASE[phase]
      : verb;

export const compact = (n: number) =>
  n < 1000
    ? String(Math.round(n))
    : new Intl.NumberFormat("en", {
        notation: "compact",
        maximumFractionDigits: 1,
      })
        .format(n)
        .toLowerCase();

// The turn clock: from the send on this page when the turn followed it
// (within two minutes), else from when the turn started.
export function turnClock(
  started: number | null,
  sent: number | null,
): number | null {
  if (started && sent && sent <= started && started - sent < 120_000)
    return sent;
  return started;
}

export function StatusLine({
  sessionId,
  status,
  startedAt,
  verb,
  onBackground,
}: {
  sessionId: string;
  status: string;
  startedAt: number | null;
  verb: string;
  // Moves the command or subagent the turn waits on to the background.
  onBackground?: () => void;
}) {
  const busy = [
    "running",
    "waiting",
    "starting",
    "stopping",
    "external_running",
  ].includes(status);
  const [now, setNow] = useState(() => Date.now());
  const [live, setLive] = useState({
    speed: 0,
    tokens: 0,
    phase: "",
    mcp: { done: 0, total: 0 },
  });
  useEffect(() => {
    if (!busy) return;
    const timer = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, [busy]);
  useEffect(() => {
    const update = (e: Event) => {
      const v = (e as CustomEvent).detail;
      if (v.session === sessionId)
        setLive({
          speed: Number(v.data?.speed) || 0,
          tokens: Number(v.data?.turnTokens) || 0,
          phase: String(v.data?.phase ?? ""),
          mcp: {
            done: Number(v.data?.mcpDone) || 0,
            total: Number(v.data?.mcpTotal) || 0,
          },
        });
    };
    window.addEventListener("workbench:telemetry", update);
    return () => window.removeEventListener("workbench:telemetry", update);
  }, [sessionId]);
  if (!busy) return null;
  const shown = statusVerb(verb, live.phase, live.mcp);
  const meta = [
    startedAt ? formatDuration(Math.max(0, now - startedAt)) : null,
    status === "running" && live.tokens > 0
      ? `↓ ${compact(live.tokens)} ${Math.round(live.tokens) === 1 ? "token" : "tokens"}`
      : null,
    status === "running" && live.speed >= 1
      ? `~${live.speed.toFixed(0)} tok/s`
      : null,
    status === "waiting"
      ? "respond below"
      : status === "external_running"
        ? "in another client"
        : "esc to interrupt",
  ].filter(Boolean);
  return (
    <div className={"status-line " + status}>
      <span className="status-glyph" aria-hidden>
        {status === "waiting" ? "◆" : "✻"}
      </span>
      <span className="status-verb" role="status">
        {shown}…
      </span>
      <span className="status-meta" aria-hidden>
        ({meta.join(" · ")})
      </span>
      {onBackground && status === "running" && (
        <button
          type="button"
          className="status-background"
          title="Move it to the background and keep going (Ctrl+B)"
          onClick={onBackground}
        >
          ⌃B background
        </button>
      )}
    </div>
  );
}
