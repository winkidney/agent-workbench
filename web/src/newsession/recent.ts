import {
  displayTitle,
  namespaceOf,
  workspaceName,
  type Prefs,
  type Session,
} from "../sidebar/tree";

export type RecentProject = {
  path: string;
  name: string;
  latest: string;
  pinned: boolean;
  sample?: string;
};

// Launch directories to offer for a new session: pinned projects first, then
// by most recent activity. Automation, temporary and chat directories are left out.
export function recentProjects(
  sessions: Session[],
  prefs: Prefs,
  workspaces: string[] = [],
): RecentProject[] {
  const latest = new Map<string, string>();
  const sample = new Map<string, string>();
  for (const s of sessions) {
    if ((s.origin || "project") !== "project") continue;
    const p = namespaceOf(s);
    if (!p) continue;
    if (s.updated > (latest.get(p) ?? "")) {
      latest.set(p, s.updated);
      if (!s.parentId) sample.set(p, displayTitle(s, prefs));
    }
  }
  for (const w of workspaces) if (!latest.has(w)) latest.set(w, "");
  for (const w of prefs.pinnedWorkspaces) if (!latest.has(w)) latest.set(w, "");
  const paths = [...latest.keys()];
  const pins = prefs.pinnedWorkspaces;
  return paths
    .map((path) => ({
      path,
      name: workspaceName(path, paths),
      latest: latest.get(path) ?? "",
      pinned: pins.includes(path),
      sample: sample.get(path),
    }))
    .sort((a, b) => {
      if (a.pinned !== b.pinned) return a.pinned ? -1 : 1;
      if (a.pinned) return pins.indexOf(a.path) - pins.indexOf(b.path);
      return b.latest.localeCompare(a.latest) || a.name.localeCompare(b.name);
    });
}

export function filterRecent(list: RecentProject[], query: string) {
  const tokens = query.toLowerCase().split(/\s+/).filter(Boolean);
  if (!tokens.length) return list;
  return list.filter((p) =>
    tokens.every((t) => `${p.name} ${p.path}`.toLowerCase().includes(t)),
  );
}

export const isPathInput = (q: string) => /^(\/|~(\/|$))/.test(q.trim());

// Split a typed path into the directory to list and the name prefix to match,
// like shell completion: "~/work/ag" → ["~/work", "ag"].
export function completionTarget(input: string): {
  dir: string;
  prefix: string;
} {
  const q = input.trim();
  if (q === "~") return { dir: "~", prefix: "" };
  const i = q.lastIndexOf("/");
  const dir = i <= 0 ? (q.startsWith("~") ? "~" : "/") : q.slice(0, i);
  return { dir: dir || "/", prefix: q.slice(i + 1) };
}

// Show paths under home as ~/…
export function tildePath(path: string, home?: string) {
  if (!home) return path;
  if (path === home) return "~";
  return path.startsWith(home + "/") ? "~" + path.slice(home.length) : path;
}
