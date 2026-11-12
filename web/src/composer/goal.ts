import { useEffect, useState } from "react";
import { api } from "../lib/api";
import { formatDuration } from "../transcript/model";
import { compact } from "./StatusLine";

// A session's /goal, from the server's telemetry (internal/workbench/goal.go).
export type Goal = {
  objective: string;
  // Codex: active, paused, blocked, usageLimited, budgetLimited, complete.
  // Claude: active, met, failed.
  status: string;
  tokensUsed?: number;
  tokenBudget?: number;
  seconds?: number;
  checks?: number;
  lastReason?: string;
  updated: string;
};

// The words of each CLI's own goal status.
const LABELS: Record<string, string> = {
  paused: "Goal paused",
  blocked: "Goal stalled",
  usageLimited: "Goal hit usage limits",
  budgetLimited: "Goal reached its budget",
  complete: "Goal achieved",
  met: "Goal met",
  failed: "Goal can't be met",
};
export const goalLabel = (agent: string, status: string) =>
  status === "active"
    ? agent === "codex"
      ? "Pursuing goal"
      : "Goal active"
    : (LABELS[status] ?? `Goal ${status}`);

export const goalDone = (status: string) =>
  status === "complete" || status === "met" || status === "failed";

export const goalTone = (status: string) =>
  status === "active" || status === "complete" || status === "met"
    ? "ok"
    : status === "failed"
      ? "bad"
      : status === "paused"
        ? "idle"
        : "warn";

// Time, checks and tokens, as the CLIs summarize a goal.
export function goalMeta(g: Goal): string {
  const parts: string[] = [];
  if (g.checks)
    parts.push(`${g.checks} ${g.checks === 1 ? "check" : "checks"}`);
  if (g.seconds && g.seconds >= 1) parts.push(formatDuration(g.seconds * 1000));
  if (g.tokensUsed || g.tokenBudget)
    parts.push(
      `${compact(g.tokensUsed ?? 0)}${g.tokenBudget ? ` / ${compact(g.tokenBudget)}` : ""} tokens`,
    );
  return parts.join(" · ");
}

// What "/goal …" from the composer needs first (Codex, as its TUI does):
// "/goal edit" alone puts the current objective in the composer to edit,
// and a new objective over an unfinished goal asks before replacing it.
export type GoalIntent =
  { kind: "prefill"; text: string } | { kind: "confirm"; args: string } | null;
export function goalIntent(
  agent: string,
  args: string,
  goal: Goal | null | undefined,
): GoalIntent {
  if (agent !== "codex") return null;
  const a = args.trim();
  if (/^edit$/i.test(a))
    return goal
      ? { kind: "prefill", text: `/goal edit ${goal.objective}` }
      : null;
  if (!a || /^(clear|pause|resume)$/i.test(a) || /^edit\s/i.test(a))
    return null;
  return goal && !goalDone(goal.status) ? { kind: "confirm", args: a } : null;
}

const same = (a: Goal | null, b: Goal | null) =>
  JSON.stringify(a) === JSON.stringify(b);

// The session's goal: read once, then from each telemetry update.
export function useGoal(sessionId: string | undefined): Goal | null {
  const [state, setState] = useState<{ id?: string; goal: Goal | null }>({
    goal: null,
  });
  useEffect(() => {
    if (!sessionId) return;
    let live = true;
    const set = (goal: Goal | null) =>
      setState((s) =>
        s.id === sessionId && same(s.goal, goal) ? s : { id: sessionId, goal },
      );
    const read = () =>
      api<{ goal?: Goal }>(
        `sessions/${encodeURIComponent(sessionId)}/telemetry`,
      )
        .then((v) => live && set(v.goal ?? null))
        .catch(() => {});
    void read();
    const update = (e: Event) => {
      const v = (e as CustomEvent).detail;
      if (v.session === sessionId) set(v.data?.goal ?? null);
    };
    const reread = (e: Event) => {
      if ((e as CustomEvent).detail?.session === sessionId) void read();
    };
    window.addEventListener("workbench:telemetry", update);
    window.addEventListener("workbench:goal", reread);
    return () => {
      live = false;
      window.removeEventListener("workbench:telemetry", update);
      window.removeEventListener("workbench:goal", reread);
    };
  }, [sessionId]);
  return state.id === sessionId ? state.goal : null;
}

// A /goal command finished: read the goal now rather than with the next
// telemetry update (at most one a second).
export function goalChanged(session: string) {
  window.dispatchEvent(
    new CustomEvent("workbench:goal", { detail: { session } }),
  );
}

// Fills the composer of a session (the goal bar's Edit).
export function compose(session: string, text: string) {
  window.dispatchEvent(
    new CustomEvent("workbench:compose", { detail: { session, text } }),
  );
}
