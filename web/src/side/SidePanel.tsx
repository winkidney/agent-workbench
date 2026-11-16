import { useEffect, useState } from "react";
import { ArrowUp, MessagesSquare, Plus, RefreshCw, X } from "lucide-react";
import { api, checkAccess } from "../lib/api";
import { newlineKey } from "../lib/newline";
import { randomUUID } from "../lib/uuid";
import type { Agent, WorkbenchEvent } from "../transcript/model";
import {
  applyEvents,
  emptyState,
  prepend,
  fromHistory,
  type State,
} from "../transcript/reducer";
import { Timeline } from "../transcript/Timeline";
import { ApprovalCard } from "../approval/ApprovalCard";
import { Button } from "../components/ui/button";
import { Dialog, DialogContent, DialogTitle } from "../components/ui/dialog";

export type SideChat = {
  id: string;
  parent: string;
  agent: Agent;
  status: string;
  created: string;
  error?: string;
};
const terminal = (s: string) => s === "closed" || s === "interrupted";
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
export function SidePanel({
  parent,
  focus,
  onDismiss,
}: {
  parent: string;
  focus?: string;
  onDismiss: () => void;
}) {
  const [list, setList] = useState<SideChat[]>([]);
  const [selected, setSelected] = useState(focus ?? "");
  const [error, setError] = useState("");
  const [ask, setAsk] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  const report = (e: unknown) =>
    setError(e instanceof Error ? e.message : String(e));
  const refresh = () => {
    setRefreshing(true);
    return api<SideChat[]>(`sessions/${encodeURIComponent(parent)}/side-chats`)
      .then((xs) => {
        setList(xs);
        if (!selected)
          setSelected(xs.find((x) => !terminal(x.status))?.id ?? "");
      })
      .finally(() => setRefreshing(false));
  };
  const create = () => {
    api<SideChat>(`sessions/${encodeURIComponent(parent)}/side-chats`, "POST")
      .then((x) => {
        setList((xs) => [x, ...xs.filter((v) => v.id !== x.id)]);
        setSelected(x.id);
      })
      .catch(report);
  };
  useEffect(() => {
    let live = true;
    api<SideChat[]>(`sessions/${encodeURIComponent(parent)}/side-chats`)
      .then((xs) => {
        if (live) {
          setList(xs);
          setSelected(focus ?? xs.find((x) => !terminal(x.status))?.id ?? "");
        }
      })
      .catch(report);
    return () => {
      live = false;
    };
  }, [parent, focus]);
  const closePanel = () => {
    const active = list.find((x) => x.id === selected);
    if (active && !terminal(active.status)) setAsk(true);
    else onDismiss();
  };
  useEffect(() => {
    const escape = (event: KeyboardEvent) => {
      if (event.key !== "Escape" || event.isComposing || ask) return;
      event.preventDefault();
      event.stopPropagation();
      closePanel();
    };
    window.addEventListener("keydown", escape, true);
    return () => window.removeEventListener("keydown", escape, true);
  }, [ask, list, selected]);
  return (
    <aside
      className="side-chat"
      role="dialog"
      aria-label="Side chat"
      onKeyDown={(e) => {
        if (e.key === "Escape") {
          e.preventDefault();
          e.stopPropagation();
          closePanel();
        }
      }}
    >
      <header className="side-chat-header">
        <MessagesSquare size={15} aria-hidden />
        <h3>Side chat</h3>
        <Button
          variant="ghost"
          size="icon"
          aria-label="New side chat"
          title="New side chat"
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
        <Button
          variant="ghost"
          size="icon"
          aria-label="Close side chat"
          title="Close side chat (Esc)"
          onClick={closePanel}
        >
          <X size={17} />
        </Button>
      </header>
      <div className="side-chat-nav">
        <select
          aria-label="Side chat history"
          value={selected}
          onChange={(e) => setSelected(e.target.value)}
        >
          <option value="">History</option>
          {list.map((x) => (
            <option value={x.id} key={x.id}>
              {new Date(x.created).toLocaleString()} ·{" "}
              {statusText[x.status] ?? x.status}
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
          onClosed={() => {
            setSelected("");
            onDismiss();
          }}
          onStatus={(x) =>
            setList((xs) => xs.map((v) => (v.id === x.id ? x : v)))
          }
        />
      ) : (
        <div className="artifact-empty side-chat-empty">
          <MessagesSquare size={28} />
          <p>Ask alongside the main task, or open a read-only record.</p>
        </div>
      )}
    </aside>
  );
}
function SideView({
  id,
  ask,
  setAsk,
  onClosed,
  onStatus,
}: {
  id: string;
  ask: boolean;
  setAsk: (v: boolean) => void;
  onClosed: () => void;
  onStatus: (v: SideChat) => void;
}) {
  const [side, setSide] = useState<SideChat>();
  const [state, setState] = useState<State>(emptyState);
  const [pending, setPending] = useState<WorkbenchEvent[]>([]);
  const [before, setBefore] = useState(0);
  const [text, setText] = useState("");
  const [error, setError] = useState("");
  const [sending, setSending] = useState(false);
  const [requestID, setRequestID] = useState(randomUUID);
  const [requestedClose, setRequestedClose] = useState(false);
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
    if (requestedClose && side?.status === "closed") onClosed();
  }, [side?.status, requestedClose]);
  const close = async () => {
    setAsk(false);
    setRequestedClose(true);
    try {
      await api(`${path(id)}/close`, "POST", { confirmed: true });
      setSide((v) => (v ? { ...v, status: "closing" } : v));
    } catch (e) {
      setRequestedClose(false);
      report(e);
    }
  };
  const send = async () => {
    if (!text.trim() || sending) return;
    setSending(true);
    setError("");
    try {
      await api(`${path(id)}/send`, "POST", { text, requestId: randomUUID() });
      setText("");
      setRequestID(randomUUID());
    } catch (e) {
      report(e);
    } finally {
      setSending(false);
    }
  };
  return (
    <>
      {side && (
        <div className="side-chat-status" role="status">
          <span className={"status-pill " + pill(side.status)}>
            {statusText[side.status] ?? side.status}
          </span>
          <span>
            {terminal(side.status) ? "Read-only record" : "Main task continues"}
          </span>
        </div>
      )}
      {(error || side?.error) && (
        <div className="error-banner" role="alert">
          {error || side?.error}
        </div>
      )}
      <Timeline
        state={state}
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
                onInterrupt={() => setAsk(true)}
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
        !terminal(side.status) && (
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
                placeholder="Ask a side question…"
                value={text}
                onChange={(e) => {
                  setText(e.target.value);
                  setRequestID(randomUUID());
                }}
                onKeyDown={(e) => {
                  // Same keys as the main composer: Enter sends.
                  if (newlineKey(e)) return;
                  if (e.key === "Enter" && !e.nativeEvent.isComposing) {
                    e.preventDefault();
                    void send();
                  }
                }}
                disabled={side.status !== "idle" || sending}
              />
              <div className="composer-bottom">
                <div className="composer-actions">
                  <Button
                    size="icon"
                    type="submit"
                    className="composer-send"
                    aria-label="Send side question"
                    title="Send (Enter)"
                    disabled={side.status !== "idle" || sending || !text.trim()}
                  >
                    <ArrowUp size={16} />
                  </Button>
                </div>
              </div>
            </form>
            <div className="composer-hint">↵ send · ⇧↵ / Alt↵ new line</div>
          </div>
        )
      )}
      <Dialog open={ask} onOpenChange={setAsk}>
        <DialogContent className="modal modal-content confirm-dialog">
          <DialogTitle asChild>
            <h2>Close side chat?</h2>
          </DialogTitle>
          <p>
            Stop this side chat and release its resources? Its transcript will
            remain available as read-only history. The main chat will continue.
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
