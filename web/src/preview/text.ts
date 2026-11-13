import { api, checkAccess } from "../lib/api";

// Editing a text file a preview shows (internal/workbench/textfile.go): it
// is read with its stamp, and saved only over that same version unless the
// page says to overwrite.

const url = (session: string, path: string) =>
  `text?session=${encodeURIComponent(session)}&path=${encodeURIComponent(path)}`;

export const loadText = (session: string, path: string) =>
  api<{ text: string; stamp: string }>(url(session, path));

// The file changed on disk since it was read.
export class Conflict extends Error {
  constructor() {
    super("changed on disk");
  }
}

// Saves text as the file's whole content; its new stamp. Without a stamp it
// overwrites whatever is there.
export async function saveText(
  session: string,
  path: string,
  text: string,
  stamp?: string,
): Promise<string> {
  const r = await fetch(
    "/api/" +
      url(session, path) +
      (stamp ? "&stamp=" + encodeURIComponent(stamp) : ""),
    {
      method: "PUT",
      headers: { "Content-Type": "text/plain; charset=utf-8" },
      body: text,
    },
  );
  checkAccess(r, "text");
  if (r.status === 409) throw new Conflict();
  if (!r.ok) throw new Error((await r.text()).trim() || r.statusText);
  return (await r.json()).stamp;
}

// The size in a stamp ("size:time").
export const stampBytes = (stamp: string) => Number(stamp.split(":")[0]) || 0;

// A text box holds lines ending in \n alone: the file's own line ends are
// put back on save.
export function eolOf(text: string): "\n" | "\r\n" {
  let crlf = 0;
  let lf = 0;
  for (let i = text.indexOf("\n"); i >= 0; i = text.indexOf("\n", i + 1))
    if (text.charCodeAt(i - 1) === 13) crlf++;
    else lf++;
  return crlf > lf ? "\r\n" : "\n";
}

export const asLines = (text: string) => text.replace(/\r\n?/g, "\n");

export const toDisk = (value: string, eol: "\n" | "\r\n") =>
  eol === "\n" ? value : value.replace(/\n/g, "\r\n");

// What Tab indents by: a tab where lines are indented with tabs, else the
// least indentation of 2 or more spaces the file uses (2 when it has none).
export function indentUnit(text: string): string {
  let tabs = 0;
  let spaced = 0;
  let least = 0;
  let seen = 0;
  for (const m of text.matchAll(/^([ \t]+)\S/gm)) {
    const lead = m[1];
    if (lead[0] === "\t") tabs++;
    else {
      spaced++;
      if (lead.length >= 2 && (!least || lead.length < least))
        least = lead.length;
    }
    if (++seen >= 2000) break;
  }
  if (tabs > spaced) return "\t";
  return " ".repeat(least ? Math.min(least, 8) : 2);
}

// Indents (or outdents) every line the selection touches. A selection that
// ends at the start of a line leaves that line alone. Returns the range to
// replace, its new text, and the selection after.
export function shiftLines(
  value: string,
  start: number,
  end: number,
  unit: string,
  out: boolean,
) {
  const from = value.lastIndexOf("\n", start - 1) + 1;
  const last = end > start && value[end - 1] === "\n" ? end - 1 : end;
  let to = value.indexOf("\n", last);
  if (to < 0) to = value.length;
  const width = unit === "\t" ? 4 : unit.length;
  const strip = new RegExp(`^(\\t| {1,${width}})`);
  const lines = value.slice(from, to).split("\n");
  const changed = lines.map((l) =>
    out ? l.replace(strip, "") : l ? unit + l : l,
  );
  const text = changed.join("\n");
  const first = changed[0].length - lines[0].length;
  const newStart = Math.max(from, start + first);
  const newEnd = Math.max(newStart, end + text.length - (to - from));
  return { from, to, text, start: newStart, end: newEnd };
}

// The line and column (from 1) of a position.
export function lineCol(value: string, pos: number) {
  let line = 1;
  let last = -1;
  for (
    let i = value.indexOf("\n");
    i >= 0 && i < pos;
    i = value.indexOf("\n", i + 1)
  ) {
    line++;
    last = i;
  }
  return { line, col: pos - last };
}
