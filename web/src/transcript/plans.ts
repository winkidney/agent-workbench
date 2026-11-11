import type { Block } from "./model";

// What Codex's own client sends for the choices after a plan (codex-rs
// tui/src/chatwidget/plan_implementation.rs).
export const IMPLEMENT_PLAN = "Implement the plan.";
const FRESH_PREFIX =
  "A previous agent produced the plan below to accomplish the user's task. " +
  "Implement the plan in a fresh context. Treat the plan as the source of " +
  "user intent, re-read files as needed, and carry the work through " +
  "implementation and verification.";

// The first message of a new session that implements the plan alone.
export function freshPlanMessage(plan: string): string {
  return `${FRESH_PREFIX}\n\n${plan}`;
}

// The key of the plan Codex proposed at the end of its last plan-mode turn,
// while the user has not answered it: the turn completed and no message was
// sent since. Codex's proposed plans are plan blocks without steps; step
// lists are its progress updates.
export function pendingPlan(
  blocks: Pick<Block, "key" | "kind" | "text" | "steps" | "streaming">[],
): string | undefined {
  let key: string | undefined;
  let completed = false;
  for (const b of blocks) {
    if (b.kind === "plan" && !b.steps && !b.streaming && b.text?.trim()) {
      key = b.key;
      completed = false;
    } else if (b.kind === "user") {
      key = undefined;
    } else if (b.kind === "turn" && key) {
      completed = b.text === "completed";
    }
  }
  return key && completed ? key : undefined;
}
