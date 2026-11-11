// Display heuristics for tool rows. The generic path (salient argument +
// output preview) handles any tool; the enhancers below only refine wording
// for well-known tools and may be removed without breaking rendering.
import { lineDiff, parseUnified, type DiffLine } from "./diff";
import { list, record, text, type Tool } from "./model";
import { stripAnsi } from "./ansi";

const SALIENT = [
  "command",
  "cmd",
  "file_path",
  "path",
  "notebook_path",
  "paths",
  "pattern",
  "url",
  "query",
  "description",
  "prompt",
  "skill",
  "subject",
  "name",
];

export function salient(input: unknown): string {
  if (typeof input === "string") return input;
  const r = record(input);
  for (const k of SALIENT) {
    const v = r[k];
    if (typeof v === "string" && v.trim()) return v;
    if (Array.isArray(v) && v.length && v.every((x) => typeof x === "string"))
      return v.length > 1 ? `${v[0]} +${v.length - 1}` : String(v[0]);
  }
  for (const v of Object.values(r))
    if (typeof v === "string" && v.trim() && v.length <= 300) return v;
  return "";
}

// Codex runs commands through a login shell; show the inner command.
export function unwrapShell(command: string): string {
  const m = /^(?:\/\S*\/)?(?:ba|z)?sh\s+-l?c\s+(['"])([\s\S]*)\1$/.exec(
    command.trim(),
  );
  return m ? m[2] : command;
}

export function relative(path: string, cwd?: string): string {
  if (cwd && path.startsWith(cwd.replace(/\/$/, "") + "/"))
    return path.slice(cwd.replace(/\/$/, "").length + 1);
  return path;
}

const oneLine = (s: string, max = 120) => {
  const first = s.split("\n").find((l) => l.trim()) ?? "";
  const more = s.trim().includes("\n") ? " …" : "";
  const t = first.trim().replace(/\s+/g, " ");
  return t.length > max ? t.slice(0, max - 1) + "…" : t + more;
};

function codexAction(t: Tool): { name: string; arg: string } | undefined {
  const actions = list(record(t.input).actions).map(record);
  if (!actions.length || t.nativeType !== "commandExecution") return undefined;
  const kinds = new Set(actions.map((a) => text(a.type)));
  if (kinds.size !== 1) return undefined;
  const a = actions[0];
  switch (text(a.type)) {
    case "read":
      return {
        name: "Read",
        arg: actions.map((x) => text(x.name || x.path)).join(", "),
      };
    case "listFiles":
      return { name: "List", arg: text(a.path) || "." };
    case "search":
      return {
        name: "Search",
        arg: [text(a.query), text(a.path)].filter(Boolean).join(" in "),
      };
  }
  return undefined;
}

export function toolTitle(
  t: Tool,
  cwd?: string,
): { name: string; arg: string } {
  const action = codexAction(t);
  if (action)
    return { name: action.name, arg: oneLine(relative(action.arg, cwd)) };
  let name = t.name;
  const mcp = /^mcp__(.+?)__(.+)$/.exec(name);
  if (mcp) name = `${mcp[1]} · ${mcp[2]}`;
  if (/^todowrite$/i.test(name)) return { name: "Update Todos", arg: "" };
  let arg = salient(t.input);
  if (t.nativeType === "commandExecution" || /^(bash|shell)$/i.test(name))
    arg = unwrapShell(arg);
  return { name, arg: oneLine(relative(arg, cwd)) };
}

const lineCount = (s?: string) =>
  s ? s.replace(/\n$/, "").split("\n").length : 0;

export function toolDiff(t: Tool): DiffLine[] | undefined {
  if (t.diff) return parseUnified(t.diff);
  const input = record(t.input);
  if (
    typeof input.old_string === "string" &&
    typeof input.new_string === "string"
  )
    return lineDiff(input.old_string, input.new_string);
  if (Array.isArray(input.edits)) {
    const edits = input.edits.map(record);
    if (edits.every((e) => typeof e.old_string === "string"))
      return edits.flatMap((e, i) => [
        ...(i ? [{ op: "@" as const, text: "…" }] : []),
        ...lineDiff(text(e.old_string), text(e.new_string)),
      ]);
  }
  if (/^write$/i.test(t.name) && typeof input.content === "string")
    return lineDiff("", input.content);
  return undefined;
}

export type Summary = {
  lines: string[];
  tone?: "error" | "muted";
  more?: number;
};

export function toolSummary(t: Tool, diff?: DiffLine[]): Summary {
  switch (t.status) {
    case "running":
      return {
        lines: [/^(bash|shell)$/i.test(t.name) ? "Running…" : "…"],
        tone: "muted",
      };
    case "declined":
      return { lines: ["Declined"], tone: "muted" };
    case "interrupted":
      return { lines: ["Interrupted"], tone: "muted" };
  }
  const out = stripAnsi(t.output ?? "").replace(/\s+$/, "");
  if (t.status === "failed") {
    const head = out.split("\n").filter((l) => l.trim());
    const prefix = t.exitCode !== undefined ? [`Exit ${t.exitCode}`] : [];
    return {
      lines: [...prefix, ...head.slice(0, 2)].map((l) => oneLine(l, 160)),
      tone: "error",
      more: Math.max(0, head.length - 2),
    };
  }
  if (diff?.length) {
    let add = 0;
    let del = 0;
    for (const l of diff) {
      if (l.op === "+") add++;
      else if (l.op === "-") del++;
    }
    return { lines: [`+${add} −${del}`] };
  }
  const name = toolTitle(t).name;
  const n = lineCount(out);
  if (/^read$/i.test(name) && n)
    return { lines: [`Read ${n} line${n === 1 ? "" : "s"}`] };
  if (/^(grep|glob|list|search|ls)$/i.test(name))
    return { lines: [n ? `${n} result${n === 1 ? "" : "s"}` : "No results"] };
  const images = t.media?.length ?? 0;
  if (!out)
    return {
      lines: [images ? `${images} image${images > 1 ? "s" : ""}` : "Done"],
      tone: "muted",
    };
  const shown = out.split("\n").filter((l) => l.trim());
  return {
    lines: shown.slice(0, 3).map((l) => oneLine(l, 160)),
    more: Math.max(0, shown.length - 3),
  };
}
