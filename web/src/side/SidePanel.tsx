import { useEffect, useMemo, useState } from "react";
import {
  ArrowUp,
  ChevronsRight,
  MessagesSquare,
  Plus,
  RefreshCw,
  Square,
  X,
} from "lucide-react";
import { api, checkAccess } from "../lib/api";
import { newlineKey } from "../lib/newline";
import { randomUUID } from "../lib/uuid";
import type { Agent, WorkbenchEvent } from "../transcript/model";
import {
  applyEvents,
  emptyState,
  prepend,
  fromHistory,
  withSending,
  type State,
} from "../transcript/reducer";
import { Timeline } from "../transcript/Timeline";
import { ApprovalCard } from "../approval/ApprovalCard";
import { StatusLine } from "../composer/StatusLine";
import { Button } from "../components/ui/button";
import { Dialog, DialogContent, DialogTitle } from "../components/ui/dialog";
import {
  canSteer,
  echoed,
  lastQuestion,
  liveSide,
  sideVerb,
  terminal,
} from "./status";

export type SideChat = {
  id: string;
  parent: string;
  agent: Agent;
  status: string;
  created: string;
  error?: string;
  // While running: when the question was sent, and what it waits on.
  since?: string;
  phase?: string;
};
const path = (id: string) => `side-chats/${encodeURIComponent(id)}`;
// Worded and colored like the session status pill in the conversation header.
const statusText: Record<string, string> = {
  idle: "Idle",
  running: "Running",
  closing: "Closing",
  closed: "Closed",
  interrupted: "Interrupted",
  cleanup_failed: "Cleanup failed",
};
const pill = (s: string) => (s === "cleanup_failed" ? "error" : s);

type Page = { events: WorkbenchEvent[]; before: number; hasMore: boolean };

// The side chats of one session. It stays mounted while that session is
// open, hidden or not: hiding keeps a side chat, its stream and its draft;
// only closing it (confirmed) releases it, and its record stays in view.
export function SidePanel({
  parent,
  open,
  focus,
  onHide,
  onLive,
}: {
  parent: string;
  open: boolean;
  focus?: string;
  onHide: () => void;
  // The live side chat's status, or null when there is none.
  onLive: (status: string | null) => void;
}) {
  const [list, setList] = useState<SideChat[]>([]);
  const [selected, setSelected] = useState(focus ?? "");
  const [error, setError] = useState("");
  const [ask, setAsk] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  const report = (e: unknown) =>
    setError(e instanceof Error ? e.message : String(e));
  const listPath = `sessions/${encodeURIComponent(parent)}/side-chats`;
  const refresh = () => {
    setRefreshing(true);
    return api<SideChat[]>(listPath)
      .then((xs) => {
        setList(xs);
        if (!selected) setSelected(liveSide(xs)?.id ?? xs[0]?.id ?? "");
      })
      .finally(() => setRefreshing(false));
  };
  const create = () => {
    api<SideChat>(listPath, "POST")
      .then((x) => {
        setList((xs) => [x, ...xs.filter((v) => v.id !== x.id)]);
        setSelected(x.id);
      })
      .catch(report);
  };
  useEffect(() => {
    let live = true;
    api<SideChat[]>(listPath)
      .then((xs) => {
        if (!live) return;
        setList(xs);
        // The live one, else the newest record (one just closed).
        setSelected(focus ?? liveSide(xs)?.id ?? xs[0]?.id ?? "");
      })
      .catch(report);
    return () => {
      live = false;
    };
  }, [parent, focus]);
  const live = liveSide(list);
  useEffect(() => onLive(live?.status ?? null), [live?.status]);
  useEffect(() => () => onLive(null), []);
  const current = list.find((x) => x.id === selected);
  const closable =
    !!current && !terminal(current.status) && current.status !== "closing";
  return (
    // In the right panel, in place of the Outputs (ADR 0024). Esc hides it
    // only while focus is in it (a running answer stops first, SideView);
    // elsewhere Esc keeps interrupting the main turn.
    <aside
      className={"side-chat" + (open ? "" : " hidden")}
      aria-label="Side chat"
      aria-hidden={!open}
      onKeyDown={(e) => {
        if (
          e.key !== "Escape" ||
          e.defaultPrevented ||
          e.nativeEvent.isComposing ||
          ask
        )
          return;
        e.preventDefault();
        onHide();
      }}
    >
      <header className="panel-header">
        <MessagesSquare size={15} aria-hidden />
        <h3>Side chat</h3>
        <Button
          variant="ghost"
          size="icon"
          aria-label="New side chat"
          title={
            live ? "Close the live side chat to start another" : "New side chat"
          }
          disabled={!!live}
          onClick={create}
        >
          <Plus size={17} />
        </Button>
        <Button
          variant="ghost"
          size="icon"
          aria-label="Refresh history"
          title="Refresh history"
          disabled={refreshing}
          onClick={() => void refresh().catch(report)}
        >
          <RefreshCw size={15} className={refreshing ? "spin" : undefined} />
        </Button>
        {closable && (
          <Button
            variant="ghost"
            size="icon"
            aria-label="Close side chat"
            title="Close side chat and release it…"
            onClick={() => setAsk(true)}
          >
            <X size={17} />
          </Button>
        )}
        <Button
          variant="ghost"
          size="icon"
          aria-label="Hide side chat"
          title="Hide side chat (Esc) — it keeps going"
          onClick={onHide}
        >
          <ChevronsRight size={17} />
        </Button>
      </header>
      <div className="side-chat-nav">
        <select
          aria-label="Side chat history"
          value={selected}
          onChange={(e) => setSelected(e.target.value)}
        >
          {!list.length && <option value="">No side chats yet</option>}
          {list.map((x) => (
            <option value={x.id} key={x.id}>
              {terminal(x.status) ? "Read-only" : "Live"} ·{" "}
              {new Date(x.created).toLocaleString()}
            </option>
          ))}
        </select>
      </div>
      {error && (
        <div className="error-banner" role="alert">
          {error}
        </div>
      )}
      {selected ? (
        <SideView
          key={selected}
          id={selected}
          ask={ask}
          setAsk={setAsk}
          onStatus={(x) =>
            setList((xs) => xs.map((v) => (v.id === x.id ? x : v)))
          }
        />
      ) : (
        <div className="artifact-empty side-chat-empty">
          <MessagesSquare size={28} />
          <p>Ask alongside the main task without interrupting it.</p>
          <Button variant="outline" size="sm" onClick={create}>
            <Plus size={14} />
            New side chat
          </Button>
        </div>
      )}
    </aside>
  );
}

function SideView({
  id,
  ask,
  setAsk,
  onStatus,
}: {
  id: string;
  ask: boolean;
  setAsk: (v: boolean) => void;
  onStatus: (v: SideChat) => void;
}) {
  const [side, setSide] = useState<SideChat>();
  const [state, setState] = useState<State>(emptyState);
  const [pending, setPending] = useState<WorkbenchEvent[]>([]);
  const [before, setBefore] = useState(0);
  const [text, setText] = useState("");
  const [error, setError] = useState("");
  // A message sent from here, shown until the server's copy arrives.
  const [sending, setSending] = useState<{
    text: string;
    at: number;
    from: number;
  } | null>(null);
  const report = (e: unknown) =>
    setError(e instanceof Error ? e.message : String(e));
  useEffect(() => {
    const controller = new AbortController();
    let source: EventSource | undefined;
    let cancelled = false;
    setState(emptyState());
    setPending([]);
    const get = async <T,>(suffix: string): Promise<T> => {
      const r = await fetch(`/api/${path(id)}/${suffix}`, {
        signal: controller.signal,
      });
      checkAccess(r, suffix);
      if (!r.ok) throw Error(await r.text());
      return r.json();
    };
    Promise.all([
      get<Page>("history"),
      get<{ side: SideChat; pending: { id: string; request: unknown }[] }>(
        "live",
      ),
    ])
      .then(([page, live]) => {
        if (cancelled) return;
        setSide(live.side);
        onStatus(live.side);
        setState(
          applyEvents(emptyState(), page.events, {
            agent: live.side.agent,
            history: true,
          }),
        );
        setBefore(page.before);
        setPending(
          live.pending.map((p) => ({
            seq: 0,
            session: id,
            kind: "approval",
            data: p,
            time: "",
          })),
        );
        if (terminal(live.side.status)) return;
        source = new EventSource(
          `/api/${path(id)}/events?after=${page.events.at(-1)?.seq ?? 0}`,
        );
        source.onmessage = (e) => {
          const v = JSON.parse(e.data) as WorkbenchEvent;
          setState((s) =>
            applyEvents(s, [v], { agent: live.side.agent, history: false }),
          );
          if (v.kind === "approval")
            setPending((xs) => [
              ...xs.filter((x) => x.data.id !== v.data.id),
              v,
            ]);
          if (v.kind === "approval_resolved")
            setPending((xs) => xs.filter((x) => x.data.id !== v.data.id));
        };
        source.addEventListener("status", (e) => {
          const v = JSON.parse((e as MessageEvent).data) as SideChat;
          setSide(v);
          onStatus(v);
          if (terminal(v.status)) {
            source?.close();
            setPending([]);
          }
        });
        source.onerror = () => {
          if (!cancelled)
            setError("Side chat connection interrupted. Reconnecting…");
        };
        source.onopen = () => setError("");
      })
      .catch((e) => {
        if (!cancelled) report(e);
      });
    return () => {
      cancelled = true;
      controller.abort();
      source?.close();
    };
  }, [id]);
  useEffect(() => {
    if (sending && echoed(state, sending.from, sending.text)) setSending(null);
  }, [state, sending]);
  const shown = useMemo(
    () =>
      sending
        ? withSending(state, sending.text, "sending:" + sending.at)
        : state,
    [state, sending],
  );
  const close = async () => {
    setAsk(false);
    try {
      await api(`${path(id)}/close`, "POST", { confirmed: true });
      // The stream may already have said it is closed (it closes at once
      // when no engine runs): never go back to closing from there.
      setSide((v) =>
        v && !terminal(v.status) ? { ...v, status: "closing" } : v,
      );
    } catch (e) {
      report(e);
    }
  };
  const running = side?.status === "running";
  const agent = side?.agent ?? "claude";
  const steering = canSteer(agent, side?.status, side?.phase);
  const ready = (side?.status === "idle" || steering) && !sending;
  const send = async () => {
    if (!text.trim() || !ready) return;
    const message = { text, at: Date.now(), from: state.keys.length };
    setSending(message);
    setText("");
    setError("");
    try {
      await api(`${path(id)}/send`, "POST", { text, requestId: randomUUID() });
    } catch (e) {
      // Not sent: the text goes back to the box.
      setSending(null);
      setText((t) => t || message.text);
      report(e);
    }
  };
  // Stops the answer; the side chat stays open and the question comes back
  // to the box (unless something else is written there) to ask again.
  const stop = async () => {
    try {
      await api(`${path(id)}/interrupt`, "POST", {});
      setText((t) => (t.trim() ? t : lastQuestion(state)));
    } catch (e) {
      report(e);
    }
  };
  return (
    <>
      {side && (
        <div className="side-chat-status" role="status">
          <span className={"status-pill " + pill(side.status)}>
            {statusText[side.status] ?? side.status}
          </span>
          {terminal(side.status) && <span>Read-only record</span>}
        </div>
      )}
      {(error || side?.error) && (
        <div className="error-banner" role="alert">
          {error || side?.error}
        </div>
      )}
      <Timeline
        state={shown}
        verbose={false}
        hasMore={before > 0}
        loadingEarlier={false}
        onLoadEarlier={() => {
          api<Page>(`${path(id)}/history?before=${before}`)
            .then((p) => {
              setState((s) =>
                prepend(s, fromHistory(p.events, side!.agent, false)),
              );
              setBefore(p.before);
            })
            .catch(report);
        }}
      />
      {side &&
        !terminal(side.status) &&
        side.status !== "closing" &&
        side.status !== "cleanup_failed" &&
        pending.length > 0 && (
          <div className="approvals">
            {pending.map((e) => (
              <ApprovalCard
                key={String(e.data.id)}
                event={e}
                agent={side.agent}
                lookup={(key) => state.blocks.get(key)}
                onInterrupt={() => void stop()}
                reply={async (key, decision, _message, answers) => {
                  await api(`${path(id)}/reply`, "POST", {
                    id: key,
                    decision,
                    answers,
                  });
                  setPending((xs) => xs.filter((x) => x.data.id !== key));
                }}
              />
            ))}
          </div>
        )}
      {(running || sending || side?.status === "closing") && (
        <div className="side-chat-working">
          <StatusLine
            sessionId={id}
            status={
              side?.status === "closing"
                ? "stopping"
                : side?.phase === "waking" || !running
                  ? "starting"
                  : "running"
            }
            startedAt={
              side?.since ? Date.parse(side.since) : (sending?.at ?? null)
            }
            verb={
              side?.status === "closing"
                ? "Closing"
                : running
                  ? sideVerb(agent, side?.phase)
                  : "Sending"
            }
            hint={
              side?.status === "closing"
                ? "releasing its resources"
                : "esc stops"
            }
          />
        </div>
      )}
      {side?.status === "cleanup_failed" ? (
        <Button
          variant="outline"
          size="sm"
          className="side-chat-retry"
          onClick={() => setAsk(true)}
        >
          <RefreshCw size={14} />
          Retry cleanup
        </Button>
      ) : (
        side &&
        !terminal(side.status) &&
        side.status !== "closing" && (
          <div className="side-chat-compose">
            <form
              className="composer"
              onSubmit={(e) => {
                e.preventDefault();
                void send();
              }}
            >
              <textarea
                aria-label="Side question"
                placeholder={
                  steering
                    ? "Steer Codex's answer…"
                    : ready
                      ? "Ask a side question…"
                      : "Write your next question; stop the answer or wait for it…"
                }
                value={text}
                onChange={(e) => setText(e.target.value)}
                onKeyDown={(e) => {
                  // Like the main composer, Esc stops a running answer;
                  // otherwise it hides the panel (SidePanel).
                  if (
                    e.key === "Escape" &&
                    running &&
                    !e.nativeEvent.isComposing
                  ) {
                    e.preventDefault();
                    void stop();
                    return;
                  }
                  // Same keys as the main composer: Enter sends.
                  if (newlineKey(e)) return;
                  if (e.key === "Enter" && !e.nativeEvent.isComposing) {
                    e.preventDefault();
                    void send();
                  }
                }}
              />
              <div className="composer-bottom">
                <div className="composer-actions">
                  {running && (
                    <Button
                      size="icon"
                      type="button"
                      variant="outline"
                      className="composer-stop"
                      aria-label="Stop side answer"
                      title="Stop the answer and ask again"
                      onClick={() => void stop()}
                    >
                      <Square size={11} fill="currentColor" />
                    </Button>
                  )}
                  <Button
                    size="icon"
                    type="submit"
                    className="composer-send"
                    aria-label={
                      steering ? "Steer side answer" : "Send side question"
                    }
                    title={
                      steering
                        ? "Steer (Enter)"
                        : ready
                          ? "Send (Enter)"
                          : "Stop the answer or wait for it"
                    }
                    disabled={!ready || !text.trim()}
                  >
                    <ArrowUp size={16} />
                  </Button>
                </div>
              </div>
            </form>
            <div className="composer-hint">
              ↵ send · ⇧↵ / Alt↵ new line · esc {running ? "stops" : "hides"}
            </div>
          </div>
        )
      )}
      <Dialog open={ask} onOpenChange={setAsk}>
        <DialogContent className="modal modal-content confirm-dialog">
          <DialogTitle asChild>
            <h2>Close side chat?</h2>
          </DialogTitle>
          <p>
            Stop this side chat and release its resources? Its transcript stays
            here as a read-only record. To keep it going, hide the panel
            instead. The main chat is not affected.
          </p>
          {text && <p>The unsent draft will be discarded.</p>}
          <div className="confirm-actions">
            <Button variant="ghost" onClick={() => setAsk(false)}>
              Cancel
            </Button>
            <Button className="danger-button" onClick={() => void close()}>
              <X size={14} />
              Close and release resources
            </Button>
          </div>
        </DialogContent>
      </Dialog>
    </>
  );
}
