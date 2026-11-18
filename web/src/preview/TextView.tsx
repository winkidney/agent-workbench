import { useEffect, useRef, useState, type CSSProperties } from "react";
import { Markdown } from "../transcript/Markdown";
import { BaseContext, MARKDOWN, useEnv } from "../transcript/media";
import { loadText } from "./text";

// Highlighter names for extensions it does not know as they are; the rest
// are used as written (highlight.js knows ts, py, go, rs, sh, yml, …).
const LANGS: Record<string, string> = {
  jsonl: "json",
  ndjson: "json",
  ipynb: "json",
  mjs: "js",
  cjs: "js",
  vue: "xml",
  svelte: "xml",
  kts: "kotlin",
  gd: "python",
  gdshader: "glsl",
  shader: "glsl",
  wgsl: "rust",
  tscn: "ini",
  tres: "ini",
  env: "bash",
  properties: "ini",
  sum: "",
  mod: "",
  lock: "",
  txt: "",
  log: "",
  csv: "",
  tsv: "",
};

export function languageOf(path: string): string {
  const name = path.split("/").pop() ?? "";
  if (/^makefile$/i.test(name)) return "makefile";
  if (/^dockerfile$/i.test(name)) return "dockerfile";
  if (/^\.env/i.test(name)) return "bash";
  const ext = /\.([\w]+)$/.exec(name)?.[1]?.toLowerCase();
  if (!ext) return "";
  return ext in LANGS ? LANGS[ext] : ext;
}

// The file as one fenced block, longer than any backtick run in it.
export function fenced(text: string, lang: string): string {
  const longest = Math.max(
    2,
    ...Array.from(text.matchAll(/`+/g), (m) => m[0].length),
  );
  const fence = "`".repeat(longest + 1);
  return `${fence}${lang}\n${text}${text.endsWith("\n") ? "" : "\n"}${fence}`;
}

// A text file shown in the preview panel: Markdown drawn as the
// conversation draws it (links and images relative to the file), anything
// else as highlighted text with numbered lines. A link's lines (line to
// lineEnd) are marked and scrolled to, again on each opening (jump); a
// Markdown file opened at a line shows its source, since what is drawn has
// no lines to point at. Its text lives only here.
export function TextView({
  session,
  path,
  line,
  lineEnd,
  jump,
  onError,
}: {
  session: string;
  path: string;
  line?: number;
  lineEnd?: number;
  jump?: number;
  onError: (message: string) => void;
}) {
  const env = useEnv();
  const [text, setText] = useState<string | null>(null);
  const box = useRef<HTMLDivElement>(null);
  useEffect(() => {
    let live = true;
    loadText(session, path)
      .then((r) => live && setText(r.text))
      .catch(
        (e) => live && onError(e instanceof Error ? e.message : String(e)),
      );
    return () => {
      live = false;
    };
  }, [session, path]);
  const drawn = MARKDOWN.test(path) && !line;
  const count = text === null || drawn ? 0 : lineCount(text);
  useEffect(() => {
    if (!line || !count) return;
    const n = Math.min(line, count);
    box.current
      ?.querySelector(`[data-line="${n}"]`)
      ?.scrollIntoView({ block: "center" });
  }, [text, line, jump, count]);
  if (text === null) return <p className="preview-note">Loading…</p>;
  const dir = path.split("/").slice(0, -1).join("/");
  const base = env.cwd
    ? env.cwd.replace(/\/$/, "") + (dir ? "/" + dir : "")
    : undefined;
  return (
    <div
      ref={box}
      className="preview-text"
      style={
        count
          ? ({ "--gutter": `${String(count).length}ch` } as CSSProperties)
          : undefined
      }
    >
      <BaseContext.Provider value={base}>
        {drawn ? (
          <Markdown text={text} />
        ) : (
          <Markdown
            text={fenced(text, languageOf(path))}
            lines={{ line, end: lineEnd }}
          />
        )}
      </BaseContext.Provider>
    </div>
  );
}

// The number of lines a code block shows for the text (a last line break
// ends the last line rather than starting another).
export const lineCount = (text: string) =>
  text.replace(/\n$/, "").split("\n").length;
