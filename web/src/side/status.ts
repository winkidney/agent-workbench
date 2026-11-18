import type { State } from "../transcript/reducer";

export type SideStatus = {
  id: string;
  status: string;
  created: string;
  phase?: string;
};

// Closed and interrupted side chats are read-only records.
export const terminal = (s: string) => s === "closed" || s === "interrupted";

// The side chat still holding resources, if any: at most one per session.
export function liveSide<T extends SideStatus>(list: T[]): T | undefined {
  return list.find((x) => !terminal(x.status));
}

// What a running side question waits on, as the status line says it.
export function sideVerb(agent: string, phase?: string): string {
  const name = agent === "codex" ? "Codex" : "Claude";
  if (phase === "waking") return `Waking ${name}`;
  return agent === "codex" ? "Codex is working" : "Waiting for Claude's answer";
}

// Whether a message sent now steers the running answer: Codex takes it into
// its turn once it answers; Claude answers a side question in one piece.
export function canSteer(agent: string, status?: string, phase?: string) {
  return agent === "codex" && status === "running" && phase !== "waking";
}

// Whether the server's copy of a message sent from the page has arrived:
// a user block with the same text after the blocks that were there.
export function echoed(state: State, from: number, text: string): boolean {
  return state.keys.slice(from).some((k) => {
    const b = state.blocks.get(k)!;
    return b.kind === "user" && !b.sending && b.text?.trim() === text.trim();
  });
}

// The question a stop gives back to ask again: the last message that asked,
// not one steered into its answer.
export function lastQuestion(state: State): string {
  for (let i = state.keys.length - 1; i >= 0; i--) {
    const b = state.blocks.get(state.keys[i])!;
    if (b.kind === "user" && !b.steered && b.text?.trim()) return b.text;
  }
  return "";
}
