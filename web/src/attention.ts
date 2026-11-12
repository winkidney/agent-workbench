import { useEffect, useRef, useState } from "react";

// Sessions that wait for the user (an approval, a question, a plan to decide
// on; computed by the server, internal/workbench/attention.go): counted in
// the tab title, and announced by a desktop notification while the page is
// in the background.

export type Attention = "approval" | "question" | "plan";
type Waiting = {
  id: string;
  agent: string;
  title: string;
  attention?: string;
};

export const ATTENTION_LABEL: Record<string, string> = {
  approval: "Needs your approval",
  question: "Has a question for you",
  plan: "Plan awaits your decision",
};
const ATTENTION_PHRASE: Record<string, string> = {
  approval: "needs your approval",
  question: "has a question for you",
  plan: "has a plan for you to decide on",
};

export const needsYou = (s: { attention?: string }) =>
  !!s.attention && s.attention in ATTENTION_LABEL;

export const titleWith = (base: string, count: number) =>
  count > 0 ? `(${count}) ${base}` : base;

// The sessions to announce: those that now wait for something they did not
// wait for before. The first list a page sees is taken as known.
export function newlyWaiting<S extends Waiting>(
  before: Map<string, string> | null,
  sessions: S[],
): S[] {
  if (!before) return [];
  return sessions.filter(
    (s) => needsYou(s) && before.get(s.id) !== s.attention,
  );
}

export function notificationFor(s: Waiting): { title: string; body: string } {
  const agent = s.agent === "codex" ? "Codex" : "Claude";
  return {
    title: `${agent} ${ATTENTION_PHRASE[s.attention ?? ""] ?? "needs you"}`,
    body: s.title,
  };
}

// Desktop notifications, per browser. They need a secure page (https or
// localhost) and the browser's permission, asked for when turned on.
const NOTIFY_KEY = "workbench:notify";
export type NotifyState =
  "unsupported" | "blocked" | "off" | "on" | "needs-permission";

const supported = () =>
  typeof window !== "undefined" &&
  "Notification" in window &&
  window.isSecureContext;

function wanted(): boolean {
  try {
    return localStorage.getItem(NOTIFY_KEY) !== "off";
  } catch {
    return true;
  }
}

export function notifyState(): NotifyState {
  if (!supported()) return "unsupported";
  if (Notification.permission === "denied") return "blocked";
  if (!wanted()) return "off";
  return Notification.permission === "granted" ? "on" : "needs-permission";
}

export async function setNotify(on: boolean): Promise<NotifyState> {
  try {
    localStorage.setItem(NOTIFY_KEY, on ? "on" : "off");
  } catch {
    // Remembered for this page view only.
  }
  if (on && supported() && Notification.permission === "default")
    await Notification.requestPermission().catch(() => "default");
  window.dispatchEvent(new CustomEvent("workbench:notify"));
  return notifyState();
}

export function useNotifyState(): NotifyState {
  const [state, setState] = useState(notifyState);
  useEffect(() => {
    const update = () => setState(notifyState());
    window.addEventListener("workbench:notify", update);
    window.addEventListener("focus", update);
    return () => {
      window.removeEventListener("workbench:notify", update);
      window.removeEventListener("focus", update);
    };
  }, []);
  return state;
}

const background = () =>
  document.visibilityState !== "visible" || !document.hasFocus();

// Keeps the tab title and the notifications in step with the sessions.
export function useAttention<S extends Waiting>(
  sessions: S[],
  ready: boolean,
  onOpen: (id: string) => void,
): S[] {
  const waiting = sessions.filter(needsYou);
  const base = useRef(document.title.replace(/^\(\d+\) /, ""));
  const known = useRef<Map<string, string> | null>(null);
  const open = useRef(onOpen);
  open.current = onOpen;
  useEffect(() => {
    document.title = titleWith(base.current, waiting.length);
  }, [waiting.length]);
  useEffect(() => {
    if (!ready) return;
    const fresh = newlyWaiting(known.current, sessions);
    known.current = new Map(
      sessions.filter(needsYou).map((s) => [s.id, s.attention!]),
    );
    if (!fresh.length || notifyState() !== "on" || !background()) return;
    for (const s of fresh) {
      const { title, body } = notificationFor(s);
      try {
        const n = new Notification(title, { body, tag: "workbench:" + s.id });
        n.onclick = () => {
          window.focus();
          open.current(s.id);
          n.close();
        };
      } catch {
        // Some browsers only notify from a service worker.
      }
    }
  }, [sessions, ready]);
  return waiting;
}
