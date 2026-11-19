// Routines: messages and commands the workbench runs on a schedule
// (internal/workbench/routine.go, ADR 0009). Types and the pure parts of
// the form.
import type { Options } from "../AgentControls";

export type When =
  | { kind: "once"; at: string }
  | { kind: "daily"; time: string; days?: number[] }
  | { kind: "interval"; minutes: number };

export type RoutineTarget =
  | { kind: "session"; session: string }
  | { kind: "assistant"; assistant: string; background?: boolean }
  | { kind: "new"; agent: "claude" | "codex"; cwd: string; options?: Options };

export type RoutineCommand = {
  run: string;
  dir: string;
  timeoutMin?: number;
  notifyOnOutput?: boolean;
};

export type RoutineRun = {
  id: string;
  routine: string;
  name: string;
  due: string;
  started: string;
  finished?: string;
  status: "running" | "sent" | "queued" | "done" | "failed" | "skipped";
  session?: string;
  exit?: number;
  output?: string;
  error?: string;
  notify?: boolean;
};

export type Routine = {
  id: string;
  name: string;
  message?: string;
  enabled: boolean;
  when: When;
  target?: RoutineTarget;
  command?: RoutineCommand;
  createdBy: string;
  created: string;
  next?: string;
  last?: RoutineRun;
};

export const DAYS = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];

const two = (n: number) => String(n).padStart(2, "0");

// "Every 2 hours", "Daily at 09:00", "Mon, Fri at 17:00", "Once on …".
export function describeWhen(w: When): string {
  switch (w.kind) {
    case "once":
      return "Once, " + shortTime(w.at);
    case "daily": {
      const days = [...(w.days ?? [])].sort((a, b) => a - b);
      if (!days.length || days.length === 7) return `Daily at ${w.time}`;
      if (days.join() === "1,2,3,4,5") return `Weekdays at ${w.time}`;
      return `${days.map((d) => DAYS[d]).join(", ")} at ${w.time}`;
    }
    case "interval": {
      const m = w.minutes;
      if (m % (24 * 60) === 0) return plural(m / (24 * 60), "day");
      if (m % 60 === 0) return plural(m / 60, "hour");
      return plural(m, "minute");
    }
  }
}

const plural = (n: number, unit: string) =>
  n === 1 ? `Every ${unit}` : `Every ${n} ${unit}s`;

// A time as the list shows it: today's by the clock, others with the date.
export function shortTime(iso: string, now = new Date()): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  const clock = `${two(d.getHours())}:${two(d.getMinutes())}`;
  if (d.toDateString() === now.toDateString()) return `today ${clock}`;
  const tomorrow = new Date(now);
  tomorrow.setDate(now.getDate() + 1);
  if (d.toDateString() === tomorrow.toDateString()) return `tomorrow ${clock}`;
  return `${d.getFullYear()}-${two(d.getMonth() + 1)}-${two(d.getDate())} ${clock}`;
}

// The value of a datetime-local input for a time, and back.
export function localInput(d: Date): string {
  return `${d.getFullYear()}-${two(d.getMonth() + 1)}-${two(d.getDate())}T${two(d.getHours())}:${two(d.getMinutes())}`;
}
export function fromLocalInput(v: string): string | undefined {
  const d = new Date(v);
  return Number.isNaN(d.getTime()) ? undefined : d.toISOString();
}

// The form's state: every field, whichever kind is chosen.
export type Draft = {
  id?: string;
  name: string;
  what: "message" | "command";
  message: string;
  run: string;
  dir: string;
  timeoutMin: number;
  notifyOnOutput: boolean;
  // command only: keep the output, or hand it to the target.
  handOff: boolean;
  target: "session" | "new" | "assistant";
  session?: string;
  // The assistant's folder name (lib/assistants.ts).
  assistant: string;
  agent: "claude" | "codex";
  cwd: string;
  whenKind: When["kind"];
  at: string;
  time: string;
  days: number[];
  every: number;
  unit: "minutes" | "hours" | "days";
  enabled: boolean;
};

export function newDraft(session?: string, cwd = "", now = new Date()): Draft {
  const later = new Date(now.getTime() + 60 * 60 * 1000);
  later.setMinutes(0, 0, 0);
  return {
    name: "",
    what: "message",
    message: "",
    run: "",
    dir: cwd,
    timeoutMin: 10,
    notifyOnOutput: false,
    handOff: false,
    target: session ? "session" : "new",
    session,
    assistant: "",
    agent: "claude",
    cwd,
    whenKind: "daily",
    at: localInput(later),
    time: "09:00",
    days: [],
    every: 1,
    unit: "hours",
    enabled: true,
  };
}

export function draftOf(r: Routine): Draft {
  const d = newDraft(
    r.target?.kind === "session" ? r.target.session : undefined,
  );
  d.id = r.id;
  d.name = r.name;
  d.enabled = r.enabled;
  d.message = r.message ?? "";
  if (r.command) {
    d.what = "command";
    d.run = r.command.run;
    d.dir = r.command.dir;
    d.timeoutMin = r.command.timeoutMin ?? 10;
    d.notifyOnOutput = !!r.command.notifyOnOutput;
    d.handOff = !!r.target;
  }
  if (r.target?.kind === "assistant") {
    d.target = "assistant";
    d.assistant = r.target.assistant;
  }
  if (r.target?.kind === "new") {
    d.target = "new";
    d.agent = r.target.agent;
    d.cwd = r.target.cwd;
  }
  d.whenKind = r.when.kind;
  if (r.when.kind === "once") d.at = localInput(new Date(r.when.at));
  if (r.when.kind === "daily") {
    d.time = r.when.time;
    d.days = r.when.days ?? [];
  }
  if (r.when.kind === "interval") {
    const m = r.when.minutes;
    if (m % (24 * 60) === 0) [d.every, d.unit] = [m / (24 * 60), "days"];
    else if (m % 60 === 0) [d.every, d.unit] = [m / 60, "hours"];
    else [d.every, d.unit] = [m, "minutes"];
  }
  return d;
}

const UNIT = { minutes: 1, hours: 60, days: 24 * 60 };

// What the server takes, or why the form cannot be sent yet.
export function routineOf(
  d: Draft,
): { routine: Partial<Routine> } | { error: string } {
  let when: When;
  if (d.whenKind === "once") {
    const at = fromLocalInput(d.at);
    if (!at) return { error: "Choose when to run it." };
    when = { kind: "once", at };
  } else if (d.whenKind === "daily") {
    if (!/^\d\d:\d\d$/.test(d.time)) return { error: "Choose a time." };
    when = { kind: "daily", time: d.time, days: d.days };
  } else {
    const minutes = Math.round(d.every * UNIT[d.unit]);
    if (!(minutes >= 5)) return { error: "Run it at most every 5 minutes." };
    when = { kind: "interval", minutes };
  }
  const sends = d.what === "message" || d.handOff;
  let target: RoutineTarget | undefined;
  if (sends) {
    if (d.target === "session") {
      if (!d.session) return { error: "Choose a session." };
      target = { kind: "session", session: d.session };
    } else if (d.target === "assistant") {
      if (!d.assistant) return { error: "Choose an assistant." };
      target = { kind: "assistant", assistant: d.assistant };
    } else {
      if (!d.cwd.trim()) return { error: "Choose a folder for the session." };
      target = { kind: "new", agent: d.agent, cwd: d.cwd };
    }
  }
  if (d.what === "message" && !d.message.trim())
    return { error: "Write the message to send." };
  let command: RoutineCommand | undefined;
  if (d.what === "command") {
    if (!d.run.trim()) return { error: "Write the command to run." };
    if (!d.dir.trim()) return { error: "Choose the folder it runs in." };
    command = {
      run: d.run.trim(),
      dir: d.dir,
      timeoutMin: d.timeoutMin,
      notifyOnOutput: d.notifyOnOutput,
    };
  }
  return {
    routine: {
      name: d.name.trim(),
      enabled: d.enabled,
      when,
      target,
      command,
      message: sends ? d.message : "",
    },
  };
}

// A message a routine sent starts with "[Routine: name]" and a blank line
// (routineText in routine.go); the transcript shows that as a label.
export function routineLabel(
  text: string,
): { name: string; rest: string } | undefined {
  const m = /^\[Routine: ([^\]\n]*)\]\n\n?/.exec(text);
  if (!m) return undefined;
  return { name: m[1], rest: text.slice(m[0].length) };
}

// What a run did, in a few words.
export function runSummary(run: RoutineRun): string {
  switch (run.status) {
    case "running":
      return "Running…";
    case "sent":
      return "Sent; the agent is working";
    case "queued":
      return "Waiting for the session";
    case "done":
      return run.exit !== undefined && !run.session
        ? "Finished"
        : "Finished; the agent answered";
    case "failed":
      return "Failed" + (run.error ? `: ${run.error}` : "");
    case "skipped":
      return "Skipped" + (run.error ? `: ${run.error}` : "");
  }
}
