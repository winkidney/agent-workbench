import { useEffect, useState } from "react";
import { api } from "../lib/api";
import { formatDuration } from "../transcript/model";
import { unwrapShell } from "../transcript/summarize";

// What a session runs outside its turns (internal/workbench/background.go):
// Claude's background shells, monitors and subagents, Codex's background
// terminals, or other processes the engine left running.
export type BackgroundTask = {
  id: string;
  source: "claude" | "codex" | "process";
  kind: string;
  description: string;
  // running, or how it ended: completed, failed, stopped, ended.
  status: string;
  started?: string;
  ended?: string;
  summary?: string;
  progress?: string;
  output?: boolean;
  pid?: number;
  cpu?: number;
  rssKb?: number;
};

export const KIND_LABEL: Record<string, string> = {
  shell: "shell",
  monitor: "monitor",
  agent: "agent",
  mcp: "MCP task",
  workflow: "workflow",
  terminal: "terminal",
  process: "process",
  task: "task",
};

export const running = (tasks: BackgroundTask[]) =>
  tasks.filter((t) => t.status === "running");

// The command as typed, without the shell wrapper Codex adds.
export const taskTitle = (t: BackgroundTask) =>
  (t.kind === "terminal" ? unwrapShell(t.description) : t.description) ||
  KIND_LABEL[t.kind] ||
  t.kind;

export function elapsed(t: BackgroundTask, now: number): string {
  const start = t.started ? Date.parse(t.started) : NaN;
  if (Number.isNaN(start)) return "";
  const end = t.ended ? Date.parse(t.ended) : now;
  return formatDuration(Math.max(0, end - start));
}

const ENDED: Record<string, string> = {
  completed: "done",
  failed: "failed",
  stopped: "stopped",
  ended: "ended",
};

// The second column of a row: progress while running, how it ended after.
export function taskState(t: BackgroundTask): string {
  if (t.status !== "running") return ENDED[t.status] ?? t.status;
  const parts: string[] = [];
  if (t.progress) parts.push(t.progress);
  if (t.cpu != null && t.cpu > 0) parts.push(`${t.cpu.toFixed(0)}% CPU`);
  if (t.rssKb) parts.push(memory(t.rssKb));
  return parts.join(" · ") || "running";
}

export function memory(kb: number): string {
  if (kb < 1024) return `${Math.round(kb)} KB`;
  if (kb < 1024 * 1024) return `${Math.round(kb / 1024)} MB`;
  return `${(kb / 1024 / 1024).toFixed(1)} GB`;
}

// One line for the collapsed tray: how many run, and the first of them.
export function summary(tasks: BackgroundTask[], now: number): string {
  const live = running(tasks);
  if (!live.length) {
    const last = tasks[tasks.length - 1];
    return last
      ? `Background ${KIND_LABEL[last.kind] ?? last.kind} ${ENDED[last.status] ?? last.status}`
      : "";
  }
  const head =
    live.length === 1 ? "1 background task" : `${live.length} background tasks`;
  const first = live[0];
  const time = elapsed(first, now);
  return `${head} · ${taskTitle(first)}${time ? " " + time : ""}`;
}

const same = (a: BackgroundTask[], b: BackgroundTask[]) =>
  JSON.stringify(a) === JSON.stringify(b);

// The session's background work: read once, then from each telemetry update.
export function useBackground(sessionId: string | undefined): BackgroundTask[] {
  const [state, setState] = useState<{
    id?: string;
    tasks: BackgroundTask[];
  }>({ tasks: [] });
  useEffect(() => {
    if (!sessionId) return;
    let live = true;
    const set = (tasks: BackgroundTask[]) =>
      setState((s) =>
        s.id === sessionId && same(s.tasks, tasks)
          ? s
          : { id: sessionId, tasks },
      );
    const read = () =>
      api<{ background?: BackgroundTask[] }>(
        `sessions/${encodeURIComponent(sessionId)}/telemetry`,
      )
        .then((v) => live && set(v.background ?? []))
        .catch(() => {});
    void read();
    const update = (e: Event) => {
      const v = (e as CustomEvent).detail;
      if (v.session === sessionId) set(v.data?.background ?? []);
    };
    const reread = (e: Event) => {
      if ((e as CustomEvent).detail?.session === sessionId) void read();
    };
    window.addEventListener("workbench:telemetry", update);
    window.addEventListener("workbench:background", reread);
    return () => {
      live = false;
      window.removeEventListener("workbench:telemetry", update);
      window.removeEventListener("workbench:background", reread);
    };
  }, [sessionId]);
  return state.id === sessionId ? state.tasks : [];
}

export function backgroundChanged(session: string) {
  window.dispatchEvent(
    new CustomEvent("workbench:background", { detail: { session } }),
  );
}

// The Claude tool a foreground turn waits on that can move to the
// background (a Bash command or a subagent), by its tool_use id.
export function backgroundable(
  blocks: {
    key: string;
    kind: string;
    tool?: { name: string; status: string };
  }[],
): string | undefined {
  for (let i = blocks.length - 1; i >= 0; i--) {
    const b = blocks[i];
    if (b.kind === "user") return undefined;
    if (
      b.kind === "tool" &&
      b.tool?.status === "running" &&
      ["Bash", "Agent", "Task"].includes(b.tool.name)
    )
      return b.key;
  }
  return undefined;
}
