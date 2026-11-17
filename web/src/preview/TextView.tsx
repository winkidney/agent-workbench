import { useEffect, useState } from "react";
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
// else as highlighted text. Its text lives only here.
export function TextView({
  session,
  path,
  onError,
}: {
  session: string;
  path: string;
  onError: (message: string) => void;
}) {
  const env = useEnv();
  const [text, setText] = useState<string | null>(null);
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
  if (text === null) return <p className="preview-note">Loading…</p>;
  const dir = path.split("/").slice(0, -1).join("/");
  const base = env.cwd
    ? env.cwd.replace(/\/$/, "") + (dir ? "/" + dir : "")
    : undefined;
  return (
    <div className="preview-text">
      <BaseContext.Provider value={base}>
        {MARKDOWN.test(path) ? (
          <Markdown text={text} />
        ) : (
          <Markdown text={fenced(text, languageOf(path))} />
        )}
      </BaseContext.Provider>
    </div>
  );
}
