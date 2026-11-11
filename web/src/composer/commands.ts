// "/" commands and "$" skills in the composer. The catalog comes from the
// server (GET /api/sessions/{id}/commands): Claude's own command list plus
// workbench commands, or Codex's commands mapped to its app-server.

export type Command = {
  name: string;
  aliases?: string[];
  argumentHint?: string;
  description?: string;
  kind: "command" | "skill";
  source: "native" | "workbench";
  // Done by the page itself.
  action?: "new" | "status" | "skills";
};
export type Skill = { name: string; description?: string; path?: string };
export type Catalog = { commands: Command[]; skills: Skill[] };

export type Trigger = {
  kind: "/" | "$";
  query: string;
  // The token being typed, [start, end) in the text.
  start: number;
  end: number;
};

// "/" while the first word of the message is typed. "$" at the start of a
// word: anywhere for Codex (its TUI attaches every mention), only at the
// start of the message for Claude (it runs skills as a leading command).
export function trigger(
  text: string,
  caret: number,
  agent: string,
): Trigger | null {
  const before = text.slice(0, caret);
  const word = /[^\s]*/.exec(text.slice(caret))![0];
  const slash = /^\/([^\s/]*)$/.exec(before);
  if (slash)
    return { kind: "/", query: slash[1], start: 0, end: caret + word.length };
  const dollar = /(^|\s)\$([\w:.-]*)$/.exec(before);
  if (!dollar) return null;
  const start = caret - dollar[2].length - 1;
  if (agent !== "codex" && before.slice(0, start).trim() !== "") return null;
  return { kind: "$", query: dollar[2], start, end: caret + word.length };
}

export type Item = {
  name: string;
  hint?: string;
  description?: string;
  badge: "skill" | "command" | "workbench";
};

function rank(query: string, name: string, aliases: string[] = []) {
  const q = query.toLowerCase();
  const n = name.toLowerCase();
  if (!q) return 2;
  if (n === q) return 0;
  if (n.startsWith(q)) return 1;
  if (aliases.some((a) => a.toLowerCase().startsWith(q))) return 2;
  // Plugin skills (anthropic-skills:pdf) match on their own part too.
  if (n.split(":").pop()!.startsWith(q)) return 2;
  if (n.includes(q)) return 3;
  return -1;
}

// Matching entries, best first: exact, prefix (or alias), then substring.
export function items(catalog: Catalog, t: Trigger, limit = 60): Item[] {
  const scored: [number, number, Item][] = [];
  if (t.kind === "/")
    catalog.commands.forEach((c, i) => {
      const r = rank(t.query, c.name, c.aliases);
      if (r >= 0)
        scored.push([
          r,
          i,
          {
            name: c.name,
            hint: c.argumentHint,
            description: c.description,
            badge:
              c.kind === "skill"
                ? "skill"
                : c.source === "workbench"
                  ? "workbench"
                  : "command",
          },
        ]);
    });
  else
    catalog.skills.forEach((s, i) => {
      const r = rank(t.query, s.name);
      if (r >= 0)
        scored.push([
          r,
          i,
          { name: s.name, description: s.description, badge: "skill" },
        ]);
    });
  return scored
    .sort((a, b) => a[0] - b[0] || a[1] - b[1])
    .slice(0, limit)
    .map((s) => s[2]);
}

// The text after picking an item, and where the caret goes.
export function complete(text: string, t: Trigger, name: string) {
  const insert = t.kind + name + " ";
  const after = text.slice(t.end).replace(/^ /, "");
  return {
    text: text.slice(0, t.start) + insert + after,
    caret: t.start + insert.length,
  };
}

export type Route =
  | { kind: "send" }
  | { kind: "command"; name: string; args: string }
  | { kind: "action"; action: NonNullable<Command["action"]>; args: string };

// What sending the text does: a workbench command (or any Codex command,
// which the server maps or refuses), a page action, or a plain send
// (Claude runs its own "/" commands; "//" is a literal "/" for Codex).
export function route(text: string, catalog: Catalog, agent: string): Route {
  const trimmed = text.trim();
  if (!trimmed.startsWith("/") || trimmed.startsWith("//"))
    return { kind: "send" };
  const m = /^\/(\S+)\s*([\s\S]*)$/.exec(trimmed);
  if (!m) return { kind: "send" };
  const [, name, args] = m;
  const cmd = catalog.commands.find(
    (c) => c.name === name || c.aliases?.includes(name),
  );
  if (cmd?.source === "workbench")
    return cmd.action
      ? { kind: "action", action: cmd.action, args }
      : { kind: "command", name: cmd.name, args };
  if (agent === "codex") return { kind: "command", name, args };
  return { kind: "send" };
}
