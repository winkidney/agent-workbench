// Protocol-neutral transcript blocks. Adapters map native Claude / Codex
// records to these; anything unrecognised becomes a generic tool or notice
// block so a CLI upgrade never hides content.
import type { AskQuestion } from "./questions";

export type WorkbenchEvent = {
  seq: number;
  session: string;
  kind: string;
  data: Record<string, unknown>;
  time: string;
};

export type ToolStatus =
  "running" | "ok" | "failed" | "declined" | "interrupted";

export type Media =
  | { kind: "data"; src: string; mime?: string }
  | { kind: "path"; path: string }
  | { kind: "url"; url: string }
  // Left out of the record sent to the page; upload: the workbench's copy,
  // when it was added there.
  | { kind: "omitted"; bytes?: number; upload?: string };

export type StepStatus = "pending" | "in_progress" | "completed";
export type Step = { text: string; status: StepStatus };

export type Tool = {
  name: string;
  input?: unknown;
  status: ToolStatus;
  output?: string;
  exitCode?: number;
  diff?: string;
  media?: Media[];
  durationMs?: number;
  // Created from a result whose tool_use is on an older history page.
  placeholder?: boolean;
  // Native protocol identity, used by display enhancers only.
  nativeType?: string;
  // A running tool's latest progress message (Codex MCP tools).
  progress?: string;
  // Why a tool did not run (Claude's auto mode blocked it).
  note?: string;
  // The subagent a call started, for its transcript: Claude's agent id, or
  // Codex's subagent thread.
  agentId?: string;
  threadId?: string;
  raw?: unknown;
  result?: unknown;
};

export type BlockKind =
  | "user"
  | "assistant"
  | "thinking"
  | "tool"
  | "plan"
  | "notice"
  | "error"
  | "turn";

export type Block = {
  key: string;
  kind: BlockKind;
  text?: string;
  streaming?: boolean;
  // Workbench-only blocks that native history will never contain.
  local?: boolean;
  // Parent tool key for subagent activity.
  parent?: string;
  // A subagent's row: its tool calls not kept (reducer.ts, subagents.go),
  // and whether there may be more than that (some were never read).
  earlier?: number;
  earlierMore?: boolean;
  tool?: Tool;
  steps?: Step[];
  media?: Media[];
  tone?: "info" | "warn";
  // Harness events: a short label and the full original text.
  label?: string;
  detail?: string;
  durationMs?: number;
  time?: string;
  // Where the agent was when it wrote the record; relative paths in the
  // block are relative to it.
  cwd?: string;
  // A message this page sent that the server has not echoed yet.
  sending?: boolean;
  // Notice text is Markdown (local command output).
  markdown?: boolean;
  // Codex: questions the agent asked to be answered in place (questions.ts).
  questions?: AskQuestion[];
  // A message sent into a turn that was already running (steering).
  steered?: boolean;
  // Claude: the steer's id while it waits for the turn's next step, and
  // whether the CLI can take it in now (send-now).
  steerId?: string;
  waiting?: boolean;
  canSendNow?: boolean;
};

export type Agent = "claude" | "codex";

export const record = (v: unknown): Record<string, unknown> =>
  v && typeof v === "object" && !Array.isArray(v)
    ? (v as Record<string, unknown>)
    : {};
export const list = (v: unknown): unknown[] => (Array.isArray(v) ? v : []);
export const text = (v: unknown): string =>
  typeof v === "string" ? v : v == null ? "" : String(v);

export function formatDuration(ms: number) {
  const s = Math.round(ms / 1000);
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m ${s % 60}s`;
  return `${Math.floor(m / 60)}h ${m % 60}m`;
}
