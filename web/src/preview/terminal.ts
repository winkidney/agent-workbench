// Terminal tabs (internal/workbench/terminal.go): a shell on the workbench
// that lives until it is closed. The page only shows it: its view exists
// while its tab is shown, and attaching again replays what it showed.

// Whether a key went to a terminal: the page's own shortcuts (Esc stops the
// agent, hides the side chat) leave those to the shell.
export const inTerminal = (target: EventTarget | null) =>
  !!(target as Element | null)?.closest?.(".xterm");

export const socketUrl = (
  id: string,
  loc: Pick<Location, "protocol" | "host">,
) =>
  `${loc.protocol === "https:" ? "wss:" : "ws:"}//${loc.host}/api/terminals/${encodeURIComponent(id)}/socket`;

// What the workbench says on a socket besides output (text frames).
export type TerminalControl = { exit?: number; missing?: boolean };

export function parseControl(text: string): TerminalControl {
  try {
    const v = JSON.parse(text) as TerminalControl;
    return typeof v === "object" && v ? v : {};
  } catch {
    return {};
  }
}

// How long to wait before attaching again after the n-th lost connection.
export const retryDelay = (n: number) => Math.min(30000, 1000 * 2 ** n);

// The terminal in the theme's colors (its tokens, read when drawn).
export function terminalTheme(
  style: Pick<CSSStyleDeclaration, "getPropertyValue">,
) {
  const token = (name: string) => style.getPropertyValue(name).trim();
  const background = token("--background") || "#000";
  const foreground = token("--foreground") || "#ddd";
  return {
    background,
    foreground,
    cursor: token("--primary") || foreground,
    cursorAccent: background,
    selectionBackground: token("--accent") || "#555",
    selectionForeground: foreground,
  };
}
