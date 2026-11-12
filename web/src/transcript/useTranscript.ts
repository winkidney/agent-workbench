import { useCallback, useEffect, useReducer, useRef, useState } from "react";
import { text, type Agent, type WorkbenchEvent } from "./model";
import { checkAccess } from "../lib/api";
import {
  applyEvents,
  emptyState,
  follow,
  fromHistory,
  mergeBlock,
  prepend,
  reconcile,
  settled,
  type State,
} from "./reducer";

type Page = {
  events: WorkbenchEvent[];
  before?: string;
  hasMore: boolean;
  // A turn is in flight here or in another client (decided by the server).
  running?: boolean;
};

type Action =
  | { type: "reset" }
  | { type: "live"; events: WorkbenchEvent[]; agent: Agent }
  | { type: "initial"; page: State }
  | { type: "earlier"; page: State }
  | { type: "reconcile"; page: State }
  | { type: "follow"; events: WorkbenchEvent[]; agent: Agent }
  | { type: "settle" };

// Pushed while another client runs the session (see external.go).
export type ExternalUpdate = {
  session: string;
  external: boolean;
  running: boolean;
  startedAt?: string;
  status: string;
  events?: WorkbenchEvent[];
  reset?: boolean;
};

// First page: replace the live region, keeping live records the CLI has not written yet.
export function loadInitial(cur: State, page: State): State {
  const merged = reconcile(cur, page);
  if (merged) return merged;
  const blocks = new Map(page.blocks);
  const keys = page.keys.slice();
  for (const k of cur.keys) {
    const b = cur.blocks.get(k)!;
    if (blocks.has(k)) blocks.set(k, mergeBlock(blocks.get(k)!, b));
    else {
      keys.push(k);
      blocks.set(k, b);
    }
  }
  return { keys, blocks, liveStart: page.keys.length };
}

function reducer(s: State, a: Action): State {
  switch (a.type) {
    case "reset":
      return emptyState();
    case "live":
      return applyEvents(s, a.events, { agent: a.agent, history: false });
    case "initial":
      return loadInitial(s, a.page);
    case "earlier":
      return prepend(s, a.page);
    case "reconcile":
      return reconcile(s, a.page) ?? s;
    case "follow":
      return follow(s, a.events, a.agent);
    case "settle":
      return settled(s);
  }
}

async function getJSON<T>(path: string): Promise<T> {
  const r = await fetch("/api/" + path);
  checkAccess(r, path);
  if (!r.ok) throw new Error(await r.text());
  return r.json();
}

const BUSY = ["running", "waiting", "starting"];
// A turn is in flight here or in another client: unfinished tools still run.
const IN_TURN = [...BUSY, "external_running"];

export type Session = { id: string; agent: Agent; status: string };

export function useTranscript(
  session: Session | undefined,
  onError: (e: unknown) => void,
) {
  const [state, dispatch] = useReducer(reducer, undefined, emptyState);
  const [approvals, setApprovals] = useState<Map<string, WorkbenchEvent>>(
    new Map(),
  );
  const [hasMore, setHasMore] = useState(false);
  const [loading, setLoading] = useState(true);
  const [loadingEarlier, setLoadingEarlier] = useState(false);
  const [turnStarted, setTurnStarted] = useState<number | null>(null);
  const [pendingQueue, setQueue] = useState<{
    items: { id: string; text: string }[];
    paused: boolean;
  }>({
    items: [],
    paused: false,
  });
  const before = useRef<string | undefined>(undefined);
  const stateRef = useRef(state);
  stateRef.current = state;
  const queue = useRef<WorkbenchEvent[]>([]);
  const frame = useRef(0);
  const busy = useRef(false);
  // The last status seen, to tell a new turn from an approval resuming one.
  const lastStatus = useRef("");
  const timers = useRef<number[]>([]);
  const id = session?.id;
  const agent = session?.agent;
  const errorRef = useRef(onError);
  errorRef.current = onError;
  // Bumped to load the history again from the newest page.
  const [generation, setGeneration] = useState(0);

  const page = useCallback(
    (cursor?: string, limit = 200) =>
      getJSON<Page>(
        `history?session=${encodeURIComponent(id!)}&limit=${limit}` +
          (cursor ? "&before=" + encodeURIComponent(cursor) : ""),
      ),
    [id],
  );

  useEffect(() => {
    if (!id || !agent) return;
    dispatch({ type: "reset" });
    setApprovals(new Map());
    setHasMore(false);
    setLoading(true);
    setTurnStarted(null);
    setQueue({ items: [], paused: false });
    before.current = undefined;
    busy.current = BUSY.includes(session?.status ?? "");
    lastStatus.current = session?.status ?? "";
    let cancelled = false;
    page()
      .then((p) => {
        if (cancelled) return;
        dispatch({
          type: "initial",
          page: fromHistory(
            p.events,
            agent,
            // The list status can be seconds old; the server reads the record.
            typeof p.running === "boolean"
              ? p.running
              : IN_TURN.includes(session?.status ?? ""),
          ),
        });
        before.current = p.before;
        setHasMore(p.hasMore);
      })
      .catch((e) => !cancelled && errorRef.current(e))
      .finally(() => !cancelled && setLoading(false));
    // Approvals raised before this page subscribed are not in the event stream.
    getJSON<{
      // As the approval events have them (planResume and the like too).
      pending: { id: string; request: unknown; [key: string]: unknown }[];
      turnStartedAt?: string;
      queue?: { id: string; text: string }[];
      queuePaused?: boolean;
    }>(`sessions/${encodeURIComponent(id)}/live`)
      .then((live) => {
        if (cancelled) return;
        if (live.turnStartedAt) setTurnStarted(Date.parse(live.turnStartedAt));
        if (live.queue?.length)
          setQueue({ items: live.queue, paused: !!live.queuePaused });
        if (!live.pending.length) return;
        setApprovals((old) => {
          const next = new Map(old);
          for (const p of live.pending)
            next.set(p.id, {
              seq: 0,
              session: id,
              kind: "approval",
              time: "",
              data: { ...p },
            });
          return next;
        });
      })
      .catch(() => {});
    return () => {
      cancelled = true;
      cancelAnimationFrame(frame.current);
      queue.current = [];
      timers.current.forEach(clearTimeout);
      timers.current = [];
    };
  }, [id, agent, generation]);

  // After a turn the CLI's own record replaces the live rendering.
  const scheduleReconcile = useCallback(() => {
    if (!agent) return;
    const attempt = (n: number) => {
      // A queued follow-up may already be running; its own end will reconcile.
      if (busy.current) return;
      page()
        .then((p) => {
          const tail = fromHistory(p.events, agent, false);
          if (reconcile(stateRef.current, tail))
            dispatch({ type: "reconcile", page: tail });
          else if (n < 2)
            timers.current.push(
              window.setTimeout(() => attempt(n + 1), 1500 * (n + 1)),
            );
        })
        .catch(() => {});
    };
    timers.current.push(window.setTimeout(() => attempt(0), 700));
  }, [agent, page]);

  const flush = useCallback(() => {
    frame.current = 0;
    const events = queue.current;
    queue.current = [];
    if (!events.length || !agent) return;
    dispatch({ type: "live", events, agent });
    let approvalsChanged = false;
    const next = new Map(approvals);
    for (const e of events) {
      if (e.kind === "approval") {
        next.set(text(e.data.id), e);
        approvalsChanged = true;
      } else if (
        e.kind === "approval_resolved" ||
        e.kind === "approval_cancelled"
      ) {
        approvalsChanged = next.delete(text(e.data.id)) || approvalsChanged;
      } else if (e.kind === "queue") {
        setQueue({
          items: Array.isArray(e.data.items)
            ? (e.data.items as { id: string; text: string }[])
            : [],
          paused: e.data.paused === true,
        });
      } else if (e.kind === "status") {
        const status = text(e.data.status);
        // A turn starts when running begins, including after the engine
        // (and Codex's MCP servers) finished starting.
        if (status === "running" && lastStatus.current !== "running")
          setTurnStarted(Date.parse(e.time) || Date.now());
        if (status !== "waiting") lastStatus.current = status;
        if (["idle", "sleeping", "stopped", "error"].includes(status)) {
          if (next.size) approvalsChanged = true;
          next.clear();
          setTurnStarted(null);
          if (busy.current) scheduleReconcile();
        }
        // The engine is gone: nothing it ran is still running, and a
        // question it was waiting on becomes answerable later.
        if (["sleeping", "stopped", "error"].includes(status))
          dispatch({ type: "settle" });
        busy.current = BUSY.includes(status);
      }
    }
    if (approvalsChanged) setApprovals(next);
  }, [agent, approvals, scheduleReconcile]);
  const flushRef = useRef(flush);
  flushRef.current = flush;

  const push = useCallback((e: WorkbenchEvent) => {
    queue.current.push(e);
    if (!frame.current)
      frame.current = requestAnimationFrame(() => flushRef.current());
  }, []);

  // Records another client wrote, read from its native record by the server.
  const external = useCallback(
    (u: ExternalUpdate) => {
      if (!agent || u.session !== id) return;
      setTurnStarted(
        u.running && u.startedAt ? Date.parse(u.startedAt) || null : null,
      );
      if (u.reset) {
        // A rewound branch or a gap: start over from the newest page.
        setGeneration((g) => g + 1);
        return;
      }
      if (u.events?.length)
        dispatch({ type: "follow", events: u.events, agent });
      if (!u.running) dispatch({ type: "settle" });
    },
    [agent, id],
  );

  const loadEarlier = useCallback(() => {
    if (!agent || !before.current || loadingEarlier) return;
    setLoadingEarlier(true);
    page(before.current)
      .then((p) => {
        dispatch({
          type: "earlier",
          page: fromHistory(p.events, agent, false),
        });
        before.current = p.before;
        setHasMore(p.hasMore);
      })
      .catch((e) => errorRef.current(e))
      .finally(() => setLoadingEarlier(false));
  }, [agent, page, loadingEarlier]);

  return {
    state,
    approvals,
    hasMore,
    loading,
    loadingEarlier,
    loadEarlier,
    push,
    external,
    turnStarted,
    queue: pendingQueue,
  };
}
