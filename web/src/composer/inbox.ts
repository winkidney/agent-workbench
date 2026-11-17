import { useEffect, useRef } from "react";

// The message being written, for images handed over from elsewhere in the
// page (the media viewer's "Paste into message"). One composer is open at a
// time; the one mounted last takes them, and none while it is read only.
export type Inbox = {
  add: (files: File[]) => void;
  // Puts file in place of the attachment key (falls back to adding).
  replace: (key: string, file: File) => void;
};

let slot: { get: () => Inbox } | null = null;

export const composerInbox = (): Inbox | null => slot?.get() ?? null;

export function useComposerInbox(inbox: Inbox | null) {
  const latest = useRef(inbox);
  latest.current = inbox;
  const on = !!inbox;
  useEffect(() => {
    if (!on) return;
    const mine = { get: () => latest.current! };
    slot = mine;
    return () => {
      if (slot === mine) slot = null;
    };
  }, [on]);
}
