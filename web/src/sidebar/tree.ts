// Sidebar view model: pinned, active and a project → session → subsession
// tree, derived from native sessions plus workbench-only preferences.

export type Session = {
  workspace?: string;
  parentId?: string;
  readOnly?: boolean;
  // Archived in the native client (Codex); read-only until unarchived.
  archived?: boolean;
  source?: string;
  id: string;
  agent: "claude" | "codex";
  cwd: string;
  title: string;
  nativeId: string;
  status: string;
  // What the session waits for from the user: approval, question, plan.
  attention?: string;
  // How many tasks or processes run outside its turns.
  background?: number;
  updated: string;
  // project | automation | temporary | chat, decided by the launch directory.
  origin?: string;
  // Subprojects (directory names under the launch directory) the session touched.
  related?: string[];
};

export type Prefs = {
  pinnedSessions: string[];
  pinnedWorkspaces: string[];
  hidden: string[];
  aliases: Record<string, string>;
};

export const emptyPrefs = (): Prefs => ({
  pinnedSessions: [],
  pinnedWorkspaces: [],
  hidden: [],
  aliases: {},
});

export type PrefsOp =
  | "pinSession"
  | "unpinSession"
  | "pinWorkspace"
  | "unpinWorkspace"
  | "hide"
  | "unhide";

// Mirrors SidebarPrefs.UpdatePrefs on the server for optimistic updates.
export function applyOp(p: Prefs, op: PrefsOp, id: string): Prefs {
  const add = (l: string[]) => (l.includes(id) ? l : [...l, id]);
  const drop = (l: string[]) => l.filter((x) => x !== id);
  switch (op) {
    case "pinSession":
      return {
        ...p,
        pinnedSessions: add(p.pinnedSessions),
        hidden: drop(p.hidden),
      };
    case "unpinSession":
      return { ...p, pinnedSessions: drop(p.pinnedSessions) };
    case "pinWorkspace":
      return { ...p, pinnedWorkspaces: add(p.pinnedWorkspaces) };
    case "unpinWorkspace":
      return { ...p, pinnedWorkspaces: drop(p.pinnedWorkspaces) };
    case "hide":
      return {
        ...p,
        hidden: add(p.hidden),
        pinnedSessions: drop(p.pinnedSessions),
      };
    case "unhide":
      return { ...p, hidden: drop(p.hidden) };
  }
}

export const ACTIVE = [
  "running",
  "waiting",
  "starting",
  "idle",
  "external",
  "external_running",
  "error",
];
const BUSY = ["running", "waiting", "starting"];

export const namespaceOf = (s: Session) => s.workspace || s.cwd;

// Shortest unique trailing path label among all projects.
export function workspaceName(path: string, paths: string[]) {
  const parts = path.split("/").filter(Boolean);
  for (let n = 1; n <= parts.length; n++) {
    const label = parts.slice(-n).join("/");
    if (paths.filter((p) => p === path || p.endsWith("/" + label)).length === 1)
      return label;
  }
  return path || "No workspace";
}

export const displayTitle = (s: Session, p: Prefs) =>
  p.aliases[s.id] || s.title || s.nativeId;

export function relativeTime(iso: string, now: number): string {
  const t = Date.parse(iso);
  if (!t) return "";
  const m = Math.max(0, Math.round((now - t) / 60000));
  if (m < 1) return "now";
  if (m < 60) return `${m}m`;
  const h = Math.round(m / 60);
  if (h < 24) return `${h}h`;
  const d = Math.round(h / 24);
  if (d < 7) return `${d}d`;
  if (d < 30) return `${Math.round(d / 7)}w`;
  return new Date(t).toLocaleDateString(undefined, {
    month: "short",
    day: "numeric",
  });
}

export type Node = {
  session: Session;
  title: string;
  hidden: boolean;
  archived: boolean;
  children: Node[];
};

// One row in a project: a child folder or a session, ordered together by activity.
export type Entry =
  | { kind: "project"; project: Project; latest: string }
  | { kind: "session"; node: Node; latest: string };

export type Project = {
  path: string;
  name: string;
  pinned: boolean;
  nodes: Node[];
  children: Project[];
  // Child folders and own sessions in display order: pinned folders, then newest activity.
  entries: Entry[];
  // Sessions launched elsewhere whose tool calls touched this project.
  related: Node[];
  total: number;
  latest: string;
};

export type OtherGroups = {
  chats: Node[];
  automated: Node[];
  temporary: Node[];
};

export type SidebarModel = {
  pinned: Node[];
  active: Node[];
  projects: Project[];
  other: OtherGroups;
  hiddenCount: number;
  archivedCount: number;
  searching: boolean;
};

const isActive = (s: Session) => ACTIVE.includes(s.status) || !!s.attention;
// A subsession at work, or waiting for the user.
const atWork = (s: Session) =>
  [...BUSY, "external_running"].includes(s.status) || !!s.attention;

// Whether a session or one of its subsessions waits for the user.
export const needsYou = (n: Node): boolean =>
  !!n.session.attention || n.children.some(needsYou);

// Sessions waiting for the user come first, then those at work.
const activeRank = (s: Session) =>
  s.attention
    ? -1
    : BUSY.includes(s.status) || s.status === "external_running"
      ? 0
      : s.status === "external"
        ? 2
        : 1;
const originOf = (s: Session) => s.origin || "project";
const within = (path: string, root: string) =>
  path.startsWith(root.replace(/\/$/, "") + "/");

export function buildSidebar(
  sessions: Session[],
  prefs: Prefs,
  opts: {
    query?: string;
    showHidden?: boolean;
    showArchived?: boolean;
    workspaces?: string[];
  } = {},
): SidebarModel {
  const hidden = new Set(prefs.hidden);
  const tokens = (opts.query ?? "").toLowerCase().split(/\s+/).filter(Boolean);
  const ids = new Set(sessions.map((s) => s.id));
  // Hidden and archived sessions appear only on request, everywhere: in
  // projects, Pinned, Active and search. Their subsessions go with them.
  const visible = (s: Session) =>
    (opts.showHidden || !hidden.has(s.id)) &&
    (opts.showArchived || !s.archived);
  const byUpdated = (a: Node, b: Node) =>
    b.session.updated.localeCompare(a.session.updated);

  // Session tree: children attach to an existing parent, orphans become roots.
  const nodes = new Map<string, Node>();
  for (const s of sessions)
    if (visible(s))
      nodes.set(s.id, {
        session: s,
        title: displayTitle(s, prefs),
        hidden: hidden.has(s.id),
        archived: !!s.archived,
        children: [],
      });
  const roots: Node[] = [];
  for (const n of nodes.values()) {
    const pid = n.session.parentId;
    if (pid && ids.has(pid)) nodes.get(pid)?.children.push(n);
    else roots.push(n);
  }

  // Project paths: launch directories of project sessions plus registered roots.
  const projectRoots = roots.filter((n) => originOf(n.session) === "project");
  const paths = [
    ...new Set([
      ...(opts.workspaces ?? []),
      ...projectRoots.map((n) => namespaceOf(n.session)),
    ]),
  ].filter(Boolean);
  const parentOf = new Map<string, string | undefined>();
  const link = () => {
    paths.sort((a, b) => a.length - b.length || a.localeCompare(b));
    parentOf.clear();
    for (const p of paths) {
      let parent: string | undefined;
      for (const q of paths)
        if (q !== p && within(p, q) && (!parent || q.length > parent.length))
          parent = q;
      parentOf.set(p, parent);
    }
  };
  link();
  const implicit = new Set<string>();
  // Siblings sharing a directory prefix get an implicit folder for it, so deep
  // run directories collapse like a file explorer instead of listing flat.
  for (let changed = true; changed;) {
    changed = false;
    const groups = new Map<string, string[]>();
    for (const p of paths) {
      const parent = parentOf.get(p);
      if (!parent) continue;
      const first = p.slice(parent.replace(/\/$/, "").length + 1).split("/")[0];
      const dir = parent.replace(/\/$/, "") + "/" + first;
      if (dir === p) continue;
      groups.set(dir, [...(groups.get(dir) ?? []), p]);
    }
    for (const [dir, members] of groups)
      if (members.length > 1 && !paths.includes(dir)) {
        paths.push(dir);
        implicit.add(dir);
        changed = true;
      }
    if (changed) link();
  }
  const tops = paths.filter((p) => !parentOf.get(p));
  const nameOf = (p: string) => {
    const parent = parentOf.get(p);
    return parent
      ? p.slice(parent.replace(/\/$/, "").length + 1)
      : workspaceName(p, tops);
  };
  const names = new Map(paths.map((p) => [p, nameOf(p)]));
  const projectLabel = (s: Session) => {
    const ns = namespaceOf(s);
    return names.get(ns) ?? ns.split("/").filter(Boolean).slice(-2).join("/");
  };
  const matches = (s: Session) => {
    if (!tokens.length) return true;
    const hay =
      `${displayTitle(s, prefs)} ${projectLabel(s)} ${namespaceOf(s)} ${s.agent} ${(s.related ?? []).join(" ")}`.toLowerCase();
    return tokens.every((t) => hay.includes(t));
  };
  // While searching keep a root when it or any child matches.
  const filter = (n: Node): Node | null => {
    const kids = n.children
      .map(filter)
      .filter((x): x is Node => !!x)
      .sort(byUpdated);
    if (!tokens.length || matches(n.session) || kids.length)
      return { ...n, children: kids };
    return null;
  };

  const own = new Map<string, Node[]>(paths.map((p) => [p, []]));
  const related = new Map<string, Node[]>(paths.map((p) => [p, []]));
  for (const r of projectRoots) {
    const kept = filter(r);
    if (!kept) continue;
    const ns = namespaceOf(r.session);
    own.get(ns)?.push(kept);
    for (const name of r.session.related ?? []) {
      const p = ns.replace(/\/$/, "") + "/" + name;
      related.get(p)?.push({ ...kept, children: [] });
    }
  }
  const pinnedWs = prefs.pinnedWorkspaces;
  // A session is as recent as its newest subsession.
  const activity = (n: Node) =>
    n.children.reduce(
      (v, c) => (c.session.updated > v ? c.session.updated : v),
      n.session.updated,
    );
  const pinRank = (e: Entry) =>
    e.kind === "project" && e.project.pinned
      ? pinnedWs.indexOf(e.project.path)
      : Number.MAX_SAFE_INTEGER;
  const label = (e: Entry) =>
    e.kind === "project" ? e.project.name : e.node.title;
  const compare = (a: Entry, b: Entry) => {
    const pa = pinRank(a);
    const pb = pinRank(b);
    if (pa !== pb) return pa < pb ? -1 : 1;
    if (!a.latest !== !b.latest) return a.latest ? -1 : 1;
    return b.latest.localeCompare(a.latest) || label(a).localeCompare(label(b));
  };
  const build = (path: string): Project | null => {
    const children = paths
      .filter((p) => parentOf.get(p) === path)
      .map(build)
      .filter((x): x is Project => !!x);
    const list = (own.get(path) ?? []).sort((a, b) =>
      activity(b).localeCompare(activity(a)),
    );
    const rel = (related.get(path) ?? []).sort(byUpdated);
    const pinned = pinnedWs.includes(path);
    // A folder with nothing but related sessions adds no information: those
    // sessions already appear in the project they were launched from.
    if (!list.length && !children.length && rel.length && !pinned) return null;
    const entries: Entry[] = [
      ...children.map((project): Entry => ({
        kind: "project",
        project,
        latest: project.latest,
      })),
      ...list.map((node): Entry => ({
        kind: "session",
        node,
        latest: activity(node),
      })),
    ].sort(compare);
    const latest = entries.reduce((v, e) => (e.latest > v ? e.latest : v), "");
    // An implicit folder holding a single folder collapses into one chain.
    if (
      implicit.has(path) &&
      !list.length &&
      !rel.length &&
      children.length === 1
    )
      return {
        ...children[0],
        name: (names.get(path) ?? path) + "/" + children[0].name,
      };
    const project: Project = {
      path,
      name: names.get(path) ?? path,
      pinned,
      nodes: list,
      children: entries.flatMap((e) =>
        e.kind === "project" ? [e.project] : [],
      ),
      entries,
      related: rel,
      total: list.length + children.reduce((n, c) => n + c.total, 0),
      latest,
    };
    if (tokens.length) {
      const named = tokens.every((t) =>
        `${project.name} ${path}`.toLowerCase().includes(t),
      );
      if (!named && !list.length && !children.length && !rel.length)
        return null;
    }
    return project;
  };
  const projects = tops
    .map(build)
    .filter((x): x is Project => !!x)
    .map((project): Entry => ({
      kind: "project",
      project,
      latest: project.latest,
    }))
    .sort(compare)
    .flatMap((e) => (e.kind === "project" ? [e.project] : []));

  const other: OtherGroups = { chats: [], automated: [], temporary: [] };
  for (const r of roots) {
    const o = originOf(r.session);
    if (o === "project") continue;
    const kept = filter(r);
    if (!kept) continue;
    (o === "automation"
      ? other.automated
      : o === "temporary"
        ? other.temporary
        : other.chats
    ).push(kept);
  }
  other.chats.sort(byUpdated);
  other.automated.sort(byUpdated);
  other.temporary.sort(byUpdated);

  const flat = [...nodes.values()].filter((n) => matches(n.session));
  // Pinned sessions keep their subsessions under them, as in projects.
  const pinned = prefs.pinnedSessions
    .map((id) => flat.find((n) => n.session.id === id))
    .filter((n): n is Node => !!n);
  // Subsessions (subagents) stay under their parent here too: a session is
  // listed when it is active or one of its subsessions is at work or needs
  // you, with those subsessions under it, ranked by the most urgent of them.
  // Subagents done and idle stay in the project tree only.
  const family = (n: Node, sub = false): Node | null => {
    const kids = n.children
      .map((c) => family(c, true))
      .filter((x): x is Node => !!x);
    const listed = sub ? atWork(n.session) : isActive(n.session);
    if (!kids.length && !(listed && matches(n.session))) return null;
    return { ...n, children: kids };
  };
  const rankOf = (n: Node): number =>
    Math.min(
      isActive(n.session) ? activeRank(n.session) : 9,
      ...n.children.map(rankOf),
    );
  const latestOf = (n: Node): string =>
    n.children
      .map(latestOf)
      .reduce((a, b) => (b > a ? b : a), n.session.updated);
  const active = roots
    .map((n) => family(n))
    .filter((n): n is Node => !!n)
    .sort(
      (a, b) => rankOf(a) - rankOf(b) || latestOf(b).localeCompare(latestOf(a)),
    );
  return {
    pinned,
    active,
    projects,
    other,
    hiddenCount: sessions.filter((s) => hidden.has(s.id)).length,
    archivedCount: sessions.filter((s) => s.archived).length,
    searching: tokens.length > 0,
  };
}

// Short label for a session's launch directory in flat lists.
export const shortPath = (p: string) => {
  const parts = p.split("/").filter(Boolean);
  return parts.length > 2 ? "…/" + parts.slice(-2).join("/") : p;
};

export function resumeCommand(s: Session) {
  const cwd = `'${s.cwd.replace(/'/g, `'\\''`)}'`;
  return s.agent === "codex"
    ? `cd ${cwd} && codex resume ${s.nativeId}`
    : `cd ${cwd} && claude --resume ${s.nativeId}`;
}
