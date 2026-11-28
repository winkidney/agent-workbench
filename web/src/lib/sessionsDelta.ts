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

// Whether an event names a session this page's list does not have yet: a
// routine's run started it (an assistant's background runs make none). The
// list is read again at once instead of at the next poll.
export function namesUnknownSession(
  kind: string,
  data: unknown,
  has: (id: string) => boolean,
): boolean {
  if (kind !== "routine") return false;
  const id = (data as { run?: { session?: string } } | null)?.run?.session;
  return !!id && !has(id);
}

// The list with a session's state from the event stream: only under its
// native identity (a new session's temporary id changes once its agent
// names it). An event's time can be older than the row's (the time the
// session was attached), or missing: the row keeps the newer, and a session
// just started takes now, since with none it would sort last and fold out of
// sight.
export function withStatus(
  list: Session[],
  s: Session,
  now = new Date().toISOString(),
): Session[] {
  if (!s.nativeId || s.id !== `${s.agent}:${s.nativeId}`) return list;
  const had = list.find((x) => x.id === s.id)?.updated ?? "";
  const updated = [s.updated, had].filter(Boolean).sort().at(-1) || now;
  return [{ ...s, updated }, ...list.filter((x) => x.id !== s.id)];
}
