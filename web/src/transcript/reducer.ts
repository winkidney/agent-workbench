import { applyClaude } from "./claude";
import { applyCodex } from "./codex";
import {
  list,
  record,
  text,
  type Agent,
  type Block,
  type Tool,
  type WorkbenchEvent,
} from "./model";

export type State = {
  keys: string[];
  blocks: Map<string, Block>;
  // Index where live (non-history) blocks begin; reconciliation replaces from here.
  liveStart: number;
};

export const emptyState = (): State => ({
  keys: [],
  blocks: new Map(),
  liveStart: 0,
});

const defined = <T extends object>(v: T): Partial<T> =>
  Object.fromEntries(
    Object.entries(v).filter(([, x]) => x !== undefined),
  ) as Partial<T>;

export function mergeBlock(old: Block, next: Block): Block {
  const merged: Block = { ...old, ...defined(next) };
  if (old.tool && next.tool) {
    const tool: Tool = { ...old.tool, ...defined(next.tool) };
    if (next.tool.placeholder && !old.tool.placeholder) {
      // A result-only placeholder must not erase the real call's identity.
      tool.name = old.tool.name;
      tool.input = old.tool.input;
      tool.nativeType = old.tool.nativeType;
      tool.raw = old.tool.raw;
    }
    if (!(next.tool.placeholder && old.tool.placeholder))
      delete tool.placeholder;
    // A late tool_use must not reset a result that already arrived.
    if (old.tool.output !== undefined && next.tool.output === undefined)
      tool.output = old.tool.output;
    if (old.tool.status !== "running" && next.tool.status === "running")
      tool.status = old.tool.status;
    merged.tool = tool;
  }
  return merged;
}

// A subagent's steps show as lines under the row that started it: its
// latest CHILD_TOOLS calls, each with only what a line shows (name, a short
// input, status), as the server sends history (subagents.go).
export const CHILD_TOOLS = 200;
const CHILD_TEXT = 1024;
const CHILD_OUTPUT = 300;

function slimValue(v: unknown, depth = 0): unknown {
  if (typeof v === "string")
    return v.length > CHILD_TEXT ? v.slice(0, CHILD_TEXT) + "…" : v;
  if (depth > 3 || v === null || typeof v !== "object") return v;
  if (Array.isArray(v))
    return v.slice(0, 20).map((e) => slimValue(e, depth + 1));
  return Object.fromEntries(
    Object.entries(v as Record<string, unknown>)
      .slice(0, 30)
      .map(([k, e]) => [k, slimValue(e, depth + 1)]),
  );
}

export function slimChild(t: Tool): Tool {
  const kept: Tool = {
    name: t.name,
    status: t.status,
    input: slimValue(t.input),
  };
  for (const k of [
    "exitCode",
    "durationMs",
    "nativeType",
    "note",
    "placeholder",
    "agentId",
    "threadId",
  ] as const)
    if (t[k] !== undefined) (kept as Record<string, unknown>)[k] = t[k];
  if (t.output !== undefined) kept.output = t.output.slice(0, CHILD_OUTPUT);
  return kept;
}

// Copy-on-write working set for one batch of adapter updates.
export class Draft {
  keys: string[];
  blocks: Map<string, Block>;
  liveStart: number;
  // The record being applied ran here; blocks it produces inherit it.
  cwd?: string;
  // Rows that gained subagent lines in this batch (see capChildren).
  private grew = new Set<string>();
  constructor(s: State) {
    this.keys = s.keys.slice();
    this.blocks = new Map(s.blocks);
    this.liveStart = s.liveStart;
  }
  get(key: string) {
    return this.blocks.get(key);
  }
  upsert(block: Block) {
    if (this.cwd && block.cwd === undefined)
      block = { ...block, cwd: this.cwd };
    const old = this.blocks.get(block.key);
    const parent = block.parent ?? old?.parent;
    if (parent && block.tool) block = { ...block, tool: slimChild(block.tool) };
    if (old) {
      const merged = mergeBlock(old, block);
      this.blocks.set(
        block.key,
        parent && merged.tool
          ? { ...merged, tool: slimChild(merged.tool) }
          : merged,
      );
    } else {
      this.keys.push(block.key);
      this.blocks.set(block.key, block);
      if (parent) this.grew.add(parent);
    }
  }
  patch(key: string, change: Partial<Block>) {
    const old = this.blocks.get(key);
    if (old) this.blocks.set(key, mergeBlock(old, { ...old, ...change }));
  }
  patchTool(key: string, change: Partial<Tool>) {
    const old = this.blocks.get(key);
    if (old?.tool) {
      const tool = { ...old.tool, ...defined(change) };
      this.blocks.set(key, {
        ...old,
        tool: old.parent ? slimChild(tool) : tool,
      });
    }
  }
  // Replace a provisional block (stream / local echo) in place with its native record.
  replace(oldKey: string, block: Block) {
    const at = this.keys.indexOf(oldKey);
    if (at < 0) return this.upsert(block);
    const old = this.blocks.get(oldKey)!;
    this.blocks.delete(oldKey);
    const dup = this.keys.indexOf(block.key);
    if (dup >= 0 && dup !== at) {
      this.keys.splice(dup, 1);
      if (dup < at) {
        this.keys[at - 1] = block.key;
      } else this.keys[at] = block.key;
    } else this.keys[at] = block.key;
    this.blocks.set(block.key, { ...old, ...defined(block), streaming: false });
  }
  remove(key: string) {
    const at = this.keys.indexOf(key);
    if (at < 0) return;
    this.keys.splice(at, 1);
    this.blocks.delete(key);
    if (at < this.liveStart) this.liveStart--;
  }
  find(fn: (b: Block) => boolean, fromEnd = true): Block | undefined {
    if (fromEnd) {
      for (let i = this.keys.length - 1; i >= 0; i--) {
        const b = this.blocks.get(this.keys[i])!;
        if (fn(b)) return b;
      }
    } else
      for (const k of this.keys) {
        const b = this.blocks.get(k)!;
        if (fn(b)) return b;
      }
    return undefined;
  }
  // End-of-turn cleanup: nothing keeps spinning after the engine went idle.
  settle(status: "interrupted" | "ok" = "interrupted") {
    for (const k of this.keys) {
      const b = this.blocks.get(k)!;
      if (b.streaming) this.blocks.set(k, { ...b, streaming: false });
      if (b.tool?.status === "running")
        this.blocks.set(k, { ...b, tool: { ...b.tool, status } });
    }
  }
  // Keeps each row's latest CHILD_TOOLS subagent lines; the ones dropped
  // count as earlier on the row.
  private capChildren() {
    if (!this.grew.size) return;
    const kids = new Map<string, string[]>();
    for (const k of this.keys) {
      const p = this.blocks.get(k)?.parent;
      if (p && this.grew.has(p)) {
        const list = kids.get(p) ?? [];
        list.push(k);
        kids.set(p, list);
      }
    }
    this.grew.clear();
    const drop = new Set<string>();
    for (const [p, list] of kids) {
      if (list.length <= CHILD_TOOLS) continue;
      const gone = list.slice(0, list.length - CHILD_TOOLS);
      gone.forEach((k) => drop.add(k));
      const row = this.blocks.get(p);
      if (row)
        this.blocks.set(p, {
          ...row,
          earlier: (row.earlier ?? 0) + gone.length,
        });
    }
    if (!drop.size) return;
    let before = 0;
    for (let i = 0; i < this.liveStart && i < this.keys.length; i++)
      if (drop.has(this.keys[i])) before++;
    this.keys = this.keys.filter((k) => !drop.has(k));
    drop.forEach((k) => this.blocks.delete(k));
    this.liveStart -= before;
  }
  state(): State {
    this.capChildren();
    return { keys: this.keys, blocks: this.blocks, liveStart: this.liveStart };
  }
}

export type Context = { agent: Agent; history: boolean; seq: number };

export function errorText(data: Record<string, unknown>): string {
  const message =
    text(data.message) ||
    text(record(data.error).message) ||
    text(data.result) ||
    list(data.errors).map(text).join("\n");
  return message || JSON.stringify(data).slice(0, 400);
}

export function applyEvent(d: Draft, e: WorkbenchEvent, ctx: Context) {
  const data = record(e.data);
  switch (e.kind) {
    case "user":
      d.upsert({
        key: "local:" + e.seq,
        kind: "user",
        text: text(data.text),
        local: true,
        steered: data.steered === true || undefined,
        // Claude holds a steer until its next step.
        steerId: text(data.uuid) || undefined,
        waiting: data.uuid ? true : undefined,
        canSendNow: data.sendNow === true || undefined,
        time: e.time,
      });
      return;
    case "steer": {
      const shown = d.find((b) => b.steerId === text(data.uuid));
      if (!shown) return;
      // Cancelled steers go back to the queue.
      if (data.state === "cancelled") d.remove(shown.key);
      else if (data.state !== "queued") d.upsert({ ...shown, waiting: false });
      return;
    }
    case "retract": {
      // A steer that did not reach the turn (it waits in the queue instead).
      const shown = d.find(
        (b) =>
          b.local === true &&
          b.kind === "user" &&
          !!b.steered &&
          b.text === text(data.text),
      );
      if (shown) d.remove(shown.key);
      return;
    }
    case "delta": {
      const channel = data.channel === "thinking" ? "thinking" : "assistant";
      const item = text(data.item);
      const key = !item
        ? (d.find((b) => !!b.streaming && b.kind === channel)?.key ??
          "delta:" + e.seq)
        : ctx.agent === "claude"
          ? "s:" + item
          : item;
      const old = d.get(key);
      d.upsert({
        key,
        kind: old?.kind ?? channel,
        text: (old?.text ?? "") + text(data.text),
        streaming: true,
        parent: text(data.parent) || undefined,
      });
      return;
    }
    case "notice":
      // A workbench message, such as a command result.
      d.upsert({
        key: "notice:" + e.seq,
        kind: "notice",
        text: text(data.text),
        tone: data.tone === "warn" ? "warn" : undefined,
        local: true,
        time: e.time,
      });
      return;
    case "error":
      d.upsert({
        key: "error:" + e.seq,
        kind: "error",
        text: errorText(data),
        local: true,
        time: e.time,
      });
      return;
    case "status":
      if (["idle", "sleeping", "stopped", "error"].includes(text(data.status)))
        d.settle();
      return;
    case "native":
      if (data.truncated === true) {
        d.upsert({
          key: "native:" + e.seq,
          kind: "notice",
          text: `Large native record omitted (${data.originalBytes} bytes). Full content remains in native history.`,
          local: true,
        });
        return;
      }
      // Claude writes cwd on every record; Codex on command items.
      d.cwd =
        text(data.cwd) ||
        text(record(record(data.params).item).cwd) ||
        undefined;
      if (ctx.agent === "claude") applyClaude(d, data, ctx);
      else applyCodex(d, data, ctx);
      d.cwd = undefined;
      return;
  }
}

export function applyEvents(
  s: State,
  events: WorkbenchEvent[],
  ctx: Omit<Context, "seq">,
): State {
  if (!events.length) return s;
  const d = new Draft(s);
  for (const e of events) applyEvent(d, e, { ...ctx, seq: e.seq });
  return d.state();
}

// Build blocks from a chronological native history page.
let historySeq = 0;
export function fromHistory(
  events: WorkbenchEvent[],
  agent: Agent,
  running: boolean,
): State {
  const d = new Draft(emptyState());
  for (const e of events) {
    const seq = --historySeq;
    applyEvent(d, { ...e, seq }, { agent, history: true, seq });
  }
  // A tool without a result in history was interrupted, unless the turn is still live.
  if (!running) d.settle("interrupted");
  const s = d.state();
  return { ...s, liveStart: s.keys.length };
}

// Prepend an older history page; overlapping keys are merged, not duplicated.
export function prepend(s: State, older: State): State {
  const keys = older.keys.filter((k) => !s.blocks.has(k));
  const blocks = new Map(s.blocks);
  for (const k of older.keys) {
    const cur = blocks.get(k);
    const old = older.blocks.get(k)!;
    blocks.set(k, cur ? mergeBlock(old, cur) : old);
  }
  return {
    keys: [...keys, ...s.keys],
    blocks,
    liveStart: s.liveStart + keys.length,
  };
}

// Show a message as sent right away, before the engine has started and the
// server echoes it.
export function withSending(s: State, text: string, key: string): State {
  const blocks = new Map(s.blocks);
  blocks.set(key, { key, kind: "user", text, local: true, sending: true });
  return { ...s, keys: [...s.keys, key], blocks };
}

// Apply records another client wrote since the last update onto the current
// blocks, as history: known keys update in place, new ones append, and an
// adapter can retract what a record supersedes.
export function follow(
  s: State,
  events: WorkbenchEvent[],
  agent: Agent,
): State {
  const d = new Draft(s);
  for (const e of events) {
    const seq = --historySeq;
    applyEvent(d, { ...e, seq }, { agent, history: true, seq });
  }
  const next = d.state();
  return { ...next, liveStart: next.keys.length };
}

// Nothing keeps spinning once another client's turn has ended.
export function settled(s: State): State {
  const d = new Draft(s);
  d.settle("interrupted");
  return d.state();
}

// Replace the live region with the native history tail written by the CLI.
// Returns null when the tail does not yet contain the latest live record.
export function reconcile(s: State, tail: State): State | null {
  const live = s.keys.slice(s.liveStart);
  const lastNative = [...live].reverse().find((k) => {
    const b = s.blocks.get(k)!;
    return !b.local && !b.streaming && !k.startsWith("s:");
  });
  if (lastNative && !tail.blocks.has(lastNative)) return null;
  const prefix = s.keys.slice(0, s.liveStart);
  const blocks = new Map<string, Block>();
  for (const k of prefix) blocks.set(k, s.blocks.get(k)!);
  const keys = prefix.slice();
  const overlaps = tail.keys.some((k) => blocks.has(k));
  if (!overlaps && prefix.length) {
    // The tail is newer than everything loaded: start over from the tail.
    keys.length = 0;
    blocks.clear();
  }
  for (const k of tail.keys) {
    const b = tail.blocks.get(k)!;
    if (blocks.has(k)) blocks.set(k, mergeBlock(blocks.get(k)!, b));
    else {
      keys.push(k);
      blocks.set(k, b);
    }
  }
  // Keep workbench-only notices and errors next to the record they followed.
  for (let i = 0; i < live.length; i++) {
    const b = s.blocks.get(live[i])!;
    if (tail.blocks.has(b.key)) continue;
    if (!b.streaming && (!b.local || b.kind === "user")) continue;
    let at = keys.length;
    for (let j = i - 1; j >= 0; j--) {
      const p = keys.indexOf(live[j]);
      if (p >= 0) {
        at = p + 1;
        break;
      }
    }
    keys.splice(at, 0, b.key);
    blocks.set(b.key, b);
  }
  return { keys, blocks, liveStart: keys.length };
}
