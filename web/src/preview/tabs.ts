// Preview tabs (internal/workbench/preview_tabs.go): what a session's preview
// panel had open, the six most recently opened files and services, and its
// terminals besides (a terminal lives until it is closed). Only targets are
// kept; a tab's page loads while it is shown and is gone when it is not.

export type Preview = {
  id: string;
  session: string;
  url: string;
  entry: string;
  owned: boolean;
  // A file preview on the workbench's own address, for this page only.
  fileUrl?: string;
  // A file preview's file is text the page may edit.
  text?: boolean;
  // What a service preview shows.
  target?: string;
  command?: string;
  port?: number;
};

export type PreviewTab = {
  kind: "file" | "service" | "terminal";
  // file: relative to the session's directory.
  path?: string;
  // service: the server attached to, or the command the workbench runs.
  target?: string;
  command?: string;
  port?: number;
  preview?: string;
  // terminal: the terminal shown, and its name.
  terminal?: string;
  title?: string;
  // From the server: the file's size and time, a file gone (or a terminal
  // ended with the workbench), a service or terminal still running, an
  // ended terminal's exit status.
  stamp?: string;
  missing?: boolean;
  running?: boolean;
  code?: number;
};

export type PreviewTabs = { tabs: PreviewTab[]; active: number };

export const MAX_TABS = 6;
export const NO_TABS: PreviewTabs = { tabs: [], active: -1 };

export function sameTarget(a: PreviewTab, b: PreviewTab): boolean {
  if (a.kind !== b.kind) return false;
  if (a.kind === "terminal") return a.terminal === b.terminal;
  if (a.kind === "file") return a.path === b.path;
  if (a.command || b.command)
    return a.command === b.command && a.port === b.port;
  return a.target === b.target;
}

// Opening puts a tab first and shows it; past six files and services, the
// one opened longest ago goes. Terminals never go this way: only closing
// ends one.
export function openTab(s: PreviewTabs, tab: PreviewTab): PreviewTabs {
  const old = s.tabs.find((t) => sameTarget(t, tab));
  const rest = s.tabs.filter((t) => !sameTarget(t, tab));
  let others = 0;
  const tabs = [{ ...old, ...tab }, ...rest].filter(
    (t) => t.kind === "terminal" || ++others <= MAX_TABS,
  );
  return { tabs, active: 0 };
}

// Closing shows the tab that took its place, or the one before.
export function closeTab(s: PreviewTabs, i: number): PreviewTabs {
  const tabs = s.tabs.filter((_, j) => j !== i);
  let active = s.active;
  if (active === i) active = tabs.length ? Math.min(i, tabs.length - 1) : -1;
  else if (active > i) active--;
  return { tabs, active };
}

export function updateTab(
  s: PreviewTabs,
  i: number,
  patch: Partial<PreviewTab>,
): PreviewTabs {
  return {
    ...s,
    tabs: s.tabs.map((t, j) => (j === i ? { ...t, ...patch } : t)),
  };
}

// What the server keeps: targets, no annotations.
export function stored(s: PreviewTabs): PreviewTabs {
  return {
    active: s.active,
    tabs: s.tabs.map(
      ({ kind, path, target, command, port, preview, terminal, title }) => ({
        kind,
        path,
        target,
        command,
        port,
        preview,
        terminal,
        title,
      }),
    ),
  };
}

export function tabFromPreview(p: Preview): PreviewTab {
  if (p.command)
    return { kind: "service", command: p.command, port: p.port, preview: p.id };
  if (p.target) return { kind: "service", target: p.target, preview: p.id };
  return { kind: "file", path: p.entry.replace(/^\//, "") };
}

export function tabTitle(t: PreviewTab): string {
  if (t.kind === "terminal") return t.title || "Terminal";
  if (t.kind === "file") return (t.path ?? "").split("/").pop() || "file";
  if (t.command) return `:${t.port} ${t.command.split(/\s+/)[0] ?? ""}`.trim();
  try {
    const u = new URL(t.target ?? "");
    return u.host + (u.pathname === "/" ? "" : u.pathname);
  } catch {
    return t.target ?? "service";
  }
}

export function tabLabel(t: PreviewTab): string {
  if (t.kind === "terminal") return t.title || "Terminal";
  if (t.kind === "file") return t.path ?? "";
  return t.command ? `${t.command} (port ${t.port})` : (t.target ?? "");
}

// Widths a page is laid out at; Fit is the space there is.
export const VIEWPORTS = [
  { id: "fit", label: "Fit", width: 0 },
  { id: "desktop", label: "Desktop", width: 1280 },
  { id: "tablet", label: "Tablet", width: 768 },
  { id: "phone", label: "Phone", width: 390 },
] as const;
export type ViewportId = (typeof VIEWPORTS)[number]["id"];

// How much a page laid out at width is shrunk to fit available.
export function scaleFor(available: number, width: number): number {
  if (!width || available <= 0) return 1;
  return Math.min(1, available / width);
}
