// Files added to a message stay on the workbench computer; the message tells
// the agent where, a line for each after the text (uploads.go):
//   [File (12 KB): /path/to/name.pdf]
// The transcript shows those lines as the files.
const LINE = /^\[File \(([^)\n]+)\): (.+)\]$/;

export type SentFile = { name: string; size: string; path: string };

export function splitFiles(text: string): { text: string; files: SentFile[] } {
  const lines = text.split("\n");
  const files: SentFile[] = [];
  for (let m; lines.length && (m = LINE.exec(lines[lines.length - 1]));) {
    files.unshift({
      size: m[1],
      path: m[2],
      name: m[2].split(/[\\/]/).pop() || m[2],
    });
    lines.pop();
  }
  return files.length
    ? { text: lines.join("\n").trimEnd(), files }
    : { text, files };
}
