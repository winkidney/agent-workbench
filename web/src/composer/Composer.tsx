import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { ArrowUp, ListPlus, Square } from "lucide-react";
import { Button } from "../components/ui/button";
import {
  AgentSelectors,
  ContextRing,
  UsageDetails,
  useAgentControls,
} from "../AgentControls";
import { CommandMenu } from "./CommandMenu";
import {
  complete,
  items as menuItems,
  route,
  trigger,
  type Catalog,
  type Item,
  type Route,
} from "./commands";
import { goalIntent, type Goal } from "./goal";

export type SendMode = "send" | "steer" | "queue";

type ComposerSession = {
  id: string;
  agent: string;
  cwd: string;
  readOnly?: boolean;
  archived?: boolean;
  status: string;
  source?: string;
};

// Unsent drafts survive switching sessions.
const drafts = new Map<string, string>();

// Command and skill catalogs per session, refreshed after a minute.
const catalogs = new Map<string, { at: number; value: Promise<Catalog> }>();
function catalogFor(id: string): Promise<Catalog> {
  const hit = catalogs.get(id);
  if (hit && Date.now() - hit.at < 60_000) return hit.value;
  const value = fetch(`/api/sessions/${encodeURIComponent(id)}/commands`).then(
    async (r) => {
      if (!r.ok) throw new Error(await r.text());
      return r.json() as Promise<Catalog>;
    },
  );
  value.catch(() => catalogs.delete(id));
  catalogs.set(id, { at: Date.now(), value });
  return value;
}

export function Composer({
  session,
  running,
  disabled,
  goal,
  inputRef,
  onSend,
  onCommand,
  onAction,
  onInterrupt,
  onBackground,
}: {
  session: ComposerSession;
  running: boolean;
  disabled: boolean;
  // The session's /goal, for "/goal edit" and replacing a goal.
  goal?: Goal | null;
  inputRef: React.RefObject<HTMLTextAreaElement | null>;
  // send: a new turn; steer: into the running turn; queue: after it.
  onSend: (text: string, how: SendMode) => Promise<unknown>;
  // A workbench (or Codex) "/" command.
  onCommand: (name: string, args: string) => Promise<unknown>;
  // A command the page carries out, such as /new.
  onAction: (action: string, args: string) => void;
  onInterrupt: () => void;
  // Ctrl+B: move what the turn waits on to the background (Claude).
  onBackground?: () => void;
}) {
  const ctl = useAgentControls({ session });
  const [prompt, setPromptState] = useState(() => drafts.get(session.id) ?? "");
  const setPrompt = (v: string) => {
    drafts.set(session.id, v);
    setPromptState(v);
  };
  const local = useRef<HTMLTextAreaElement>(null);
  const ref = inputRef ?? local;
  // "/" and "$" menu: the token at the caret decides what is listed.
  const [caret, setCaret] = useState(0);
  const [catalog, setCatalog] = useState<Catalog | null>(null);
  const [active, setActive] = useState(0);
  const [dismissed, setDismissed] = useState(false);
  const wrap = useRef<HTMLDivElement>(null);
  const t = trigger(prompt, caret, session.agent);
  useEffect(() => {
    setCatalog(null);
  }, [session.id]);
  useEffect(() => {
    if (!t || catalog) return;
    let live = true;
    catalogFor(session.id)
      .then((c) => live && setCatalog(c))
      .catch(() => {});
    return () => {
      live = false;
    };
  }, [t?.kind, catalog, session.id]);
  const shown: Item[] = t && catalog ? menuItems(catalog, t) : [];
  const open = !!t && !dismissed && !session.readOnly;
  useEffect(() => setActive(0), [t?.kind, t?.query]);
  const syncCaret = (el: HTMLTextAreaElement) => setCaret(el.selectionStart);
  const pick = (item: Item) => {
    if (!t) return;
    const next = complete(prompt, t, item.name);
    setPrompt(next.text);
    setCaret(next.caret);
    requestAnimationFrame(() => {
      ref.current?.focus();
      ref.current?.setSelectionRange(next.caret, next.caret);
    });
  };
  const startWith = (text: string) => {
    setPrompt(text);
    setCaret(text.length);
    setDismissed(false);
    requestAnimationFrame(() => {
      ref.current?.focus();
      ref.current?.setSelectionRange(text.length, text.length);
    });
  };
  // A new Codex objective over an unfinished goal waits for this answer.
  const [replace, setReplace] = useState<{ args: string; text: string } | null>(
    null,
  );
  useEffect(() => {
    const fill = (e: Event) => {
      const v = (e as CustomEvent).detail;
      if (v.session === session.id) startWith(v.text);
    };
    window.addEventListener("workbench:compose", fill);
    return () => window.removeEventListener("workbench:compose", fill);
  }, [session.id]);
  // Grow with the content up to 40% of the viewport.
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height =
      Math.min(el.scrollHeight, Math.round(innerHeight * 0.4)) + "px";
  }, [prompt, ref]);
  const readOnly = !!session.readOnly || !!session.archived;
  // During a turn Enter steers; the Queue button keeps it for after.
  const submit = async (queue = false) => {
    const text = prompt;
    if (!text.trim() || disabled || readOnly) return;
    let r: Route = { kind: "send" };
    const trimmed = text.trim();
    if (trimmed.startsWith("/") && !trimmed.startsWith("//")) {
      const cat = await catalogFor(session.id).catch(() => null);
      r = cat
        ? route(text, cat, session.agent)
        : route(text, { commands: [], skills: [] }, session.agent);
    }
    if (r.kind === "command" && r.name === "goal") {
      const intent = goalIntent(session.agent, r.args, goal);
      if (intent?.kind === "prefill") return startWith(intent.text);
      if (intent?.kind === "confirm") {
        setPrompt("");
        return setReplace({ args: intent.args, text });
      }
    }
    setReplace(null);
    setPrompt("");
    if (r.kind === "action") {
      if (r.action === "skills") return startWith("$");
      if (r.action === "status")
        return wrap.current
          ?.querySelector(".usage-details")
          ?.setAttribute("open", "");
      return onAction(r.action, r.args);
    }
    const done =
      r.kind === "command"
        ? onCommand(r.name, r.args)
        : onSend(text, !running ? "send" : queue ? "queue" : "steer");
    done.catch(() => setPrompt(text));
  };
  return (
    <div className="composer-wrap" ref={wrap}>
      {open && t && (
        <CommandMenu
          kind={t.kind}
          items={shown}
          active={active}
          loading={!catalog}
          onPick={pick}
          onHover={setActive}
        />
      )}
      {replace && goal && (
        <div
          className="goal-replace"
          role="alertdialog"
          aria-label="Replace goal"
        >
          <span className="goal-replace-text">
            Replace the current goal <q>{goal.objective}</q>?
          </span>
          <span className="goal-replace-actions">
            <button
              type="button"
              className="goal-action primary"
              autoFocus
              onClick={() => {
                const { args, text } = replace;
                setReplace(null);
                onCommand("goal", args).catch(() => setPrompt(text));
              }}
            >
              Replace goal
            </button>
            <button
              type="button"
              className="goal-action"
              onClick={() => {
                // Nothing is lost: the new objective goes back to the composer.
                startWith(replace.text);
                setReplace(null);
              }}
            >
              Keep current
            </button>
          </span>
        </div>
      )}
      <form
        className={"composer" + (readOnly ? " read-only" : "")}
        onSubmit={(e) => {
          e.preventDefault();
          void submit();
        }}
      >
        <textarea
          ref={ref}
          rows={1}
          aria-label="Task"
          disabled={readOnly}
          placeholder={
            session.source === "subagent"
              ? "Subagent transcript · read only; continue in the parent session"
              : session.archived
                ? "Archived · unarchive to continue"
                : readOnly
                  ? "Active in another client · read only"
                  : running
                    ? "Steer this turn — Enter sends it now"
                    : "Describe a task or continue this conversation…"
          }
          value={prompt}
          onChange={(e) => {
            setPrompt(e.target.value);
            syncCaret(e.target);
            setDismissed(false);
          }}
          onSelect={(e) => syncCaret(e.currentTarget)}
          onKeyDown={(e) => {
            if (open && !e.nativeEvent.isComposing) {
              if (e.key === "ArrowDown" || e.key === "ArrowUp") {
                e.preventDefault();
                const n = shown.length || 1;
                setActive((a) => (a + (e.key === "ArrowDown" ? 1 : n - 1)) % n);
                return;
              }
              if ((e.key === "Enter" || e.key === "Tab") && shown[active]) {
                // Pick, unless the token already names the item exactly.
                const exact = t?.query === shown[active].name;
                if (!(exact && e.key === "Enter")) {
                  e.preventDefault();
                  pick(shown[active]);
                  return;
                }
              }
              if (e.key === "Escape") {
                // Close the menu instead of interrupting the turn.
                e.preventDefault();
                setDismissed(true);
                return;
              }
            }
            if (
              onBackground &&
              e.ctrlKey &&
              !e.altKey &&
              !e.metaKey &&
              e.key.toLowerCase() === "b"
            ) {
              e.preventDefault();
              onBackground();
              return;
            }
            if (
              e.key === "Enter" &&
              !e.shiftKey &&
              !e.nativeEvent.isComposing
            ) {
              e.preventDefault();
              void submit();
            }
          }}
        />
        <div className="composer-bottom">
          <AgentSelectors ctl={ctl} session={session} />
          <div className="composer-actions">
            <ContextRing ctl={ctl} />
            {running && (
              <Button
                size="icon"
                type="button"
                variant="outline"
                className="composer-stop"
                aria-label="Interrupt turn"
                title="Interrupt (Esc)"
                disabled={readOnly || session.status === "stopping"}
                onClick={onInterrupt}
              >
                <Square size={11} fill="currentColor" />
              </Button>
            )}
            {running && (
              <Button
                size="icon"
                type="button"
                variant="outline"
                className="composer-queue"
                aria-label="Queue message"
                title="Queue: send when this turn ends"
                disabled={
                  disabled ||
                  readOnly ||
                  !prompt.trim() ||
                  session.status === "stopping"
                }
                onClick={() => void submit(true)}
              >
                <ListPlus size={15} />
              </Button>
            )}
            <Button
              size="icon"
              type="submit"
              className="composer-send"
              aria-label={
                running ? "Steer: send into this turn" : "Send message"
              }
              title={
                running
                  ? "Steer: send into this turn now (Enter)"
                  : "Send (Enter)"
              }
              disabled={
                disabled ||
                readOnly ||
                !prompt.trim() ||
                session.status === "stopping"
              }
            >
              <ArrowUp size={16} />
            </Button>
          </div>
        </div>
      </form>
      <div className="composer-hint">
        <UsageDetails
          ctl={ctl}
          session={session}
          hint={
            running
              ? "↵ steer · queue button sends after this turn · esc interrupt"
              : "↵ send · ⇧↵ new line"
          }
        />
      </div>
    </div>
  );
}
