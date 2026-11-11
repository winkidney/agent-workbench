// ANSI escape handling for tool output. Tools usually run without a
// terminal, but some still emit colors (test runners, Playwright call logs).

export type AnsiStyle = {
  fg?: string;
  bg?: string;
  bold?: boolean;
  dim?: boolean;
  italic?: boolean;
  underline?: boolean;
};
export type Segment = { text: string; style: AnsiStyle };

// CSI (ESC [ params final), OSC (ESC ] … BEL or ESC \) and two-byte escapes.
const ESCAPES =
  /\u001b(?:\[([0-9;:?]*)([@-~])|\][^\u0007\u001b]*(?:\u0007|\u001b\\)?|[@-Z\\-_])/g;

export const hasAnsi = (text: string) => text.includes("\u001b");

export function stripAnsi(text: string): string {
  return hasAnsi(text) ? text.replace(ESCAPES, "") : text;
}

// The 16 base colors come from the page palette (--ansi-N); the 256-color
// cube and grays are fixed, as in xterm.
const CUBE = [0, 95, 135, 175, 215, 255];
function color256(n: number): string | undefined {
  if (!Number.isInteger(n) || n < 0 || n > 255) return undefined;
  if (n < 16) return `var(--ansi-${n})`;
  if (n < 232) {
    const i = n - 16;
    return `rgb(${CUBE[Math.floor(i / 36)]},${CUBE[Math.floor(i / 6) % 6]},${CUBE[i % 6]})`;
  }
  const v = 8 + (n - 232) * 10;
  return `rgb(${v},${v},${v})`;
}

function applySgr(current: AnsiStyle, params: string): AnsiStyle {
  let s: AnsiStyle = { ...current };
  const codes = (params === "" ? "0" : params)
    .split(/[;:]/)
    .map((x) => (x === "" ? 0 : Number(x)));
  for (let i = 0; i < codes.length; i++) {
    const c = codes[i];
    if (c === 0) s = {};
    else if (c === 1) s.bold = true;
    else if (c === 2) s.dim = true;
    else if (c === 3) s.italic = true;
    else if (c === 4) s.underline = true;
    else if (c === 22) {
      delete s.bold;
      delete s.dim;
    } else if (c === 23) delete s.italic;
    else if (c === 24) delete s.underline;
    else if (c >= 30 && c <= 37) s.fg = `var(--ansi-${c - 30})`;
    else if (c >= 90 && c <= 97) s.fg = `var(--ansi-${c - 90 + 8})`;
    else if (c === 39) delete s.fg;
    else if (c >= 40 && c <= 47) s.bg = `var(--ansi-${c - 40})`;
    else if (c >= 100 && c <= 107) s.bg = `var(--ansi-${c - 100 + 8})`;
    else if (c === 49) delete s.bg;
    else if (c === 38 || c === 48) {
      let color: string | undefined;
      if (codes[i + 1] === 5) {
        color = color256(codes[i + 2]);
        i += 2;
      } else if (codes[i + 1] === 2) {
        const [r, g, b] = codes.slice(i + 2, i + 5);
        if ([r, g, b].every((v) => Number.isInteger(v) && v >= 0 && v <= 255))
          color = `rgb(${r},${g},${b})`;
        i += 4;
      }
      if (color) s[c === 38 ? "fg" : "bg"] = color;
    }
  }
  return s;
}

const sameStyle = (a: AnsiStyle, b: AnsiStyle) =>
  a.fg === b.fg &&
  a.bg === b.bg &&
  !!a.bold === !!b.bold &&
  !!a.dim === !!b.dim &&
  !!a.italic === !!b.italic &&
  !!a.underline === !!b.underline;

// Split text into styled runs. Only SGR (… m) changes the style; cursor
// movement, OSC titles and other escapes are dropped.
export function parseAnsi(text: string): Segment[] {
  if (!hasAnsi(text)) return text ? [{ text, style: {} }] : [];
  const out: Segment[] = [];
  let style: AnsiStyle = {};
  let last = 0;
  const push = (t: string) => {
    if (!t) return;
    const prev = out[out.length - 1];
    if (prev && sameStyle(prev.style, style)) prev.text += t;
    else out.push({ text: t, style: { ...style } });
  };
  for (const m of text.matchAll(ESCAPES)) {
    push(text.slice(last, m.index));
    last = m.index! + m[0].length;
    if (m[2] === "m" && !m[1]?.startsWith("?")) style = applySgr(style, m[1]);
  }
  push(text.slice(last));
  return out;
}
