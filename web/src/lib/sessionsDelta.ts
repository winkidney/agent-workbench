import type { Session } from "../sidebar/tree";

// The session list as a poll gets it (internal/workbench/sessionlist.go):
// the whole list (full), or what changed since the list this page had,
// named by its token. count is how many sessions the list has.
export type SessionsDelta = {
  token: string;
  count: number;
  full?: boolean;
  changed?: Session[];
  removed?: string[];
};

const newestFirst = (a: Session, b: Session) =>
  a.updated < b.updated ? 1 : a.updated > b.updated ? -1 : 0;

// The list after a delta. Unchanged, it is the same array (nothing to draw
// again); changed, it stays newest first, as the workbench orders it.
export function mergeSessions(list: Session[], d: SessionsDelta): Session[] {
  if (d.full) return d.changed ?? [];
  const changed = d.changed ?? [];
  const removed = new Set(d.removed ?? []);
  if (!changed.length && !removed.size) return list;
  const fresh = new Map(changed.map((s) => [s.id, s]));
  const next = list
    .filter((s) => !removed.has(s.id))
    .map((s) => {
      const c = fresh.get(s.id);
      if (!c) return s;
      fresh.delete(s.id);
      return c;
    });
  next.push(...fresh.values());
  return next.sort(newestFirst);
}

// Whether the page's copy, merged, no longer matches the list (a row it
// took from the event stream that the list never had): then it asks for
// the whole list.
export const drifted = (merged: Session[], d: SessionsDelta) =>
  merged.length !== d.count;
