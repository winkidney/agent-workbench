import { useCallback, useEffect, useRef, useState } from "react";
import { api } from "../lib/api";
import {
  NO_TABS,
  closeTab,
  openTab,
  sameTarget,
  stored,
  updateTab,
  type PreviewTab,
  type PreviewTabs,
} from "./tabs";
import { mayLeave } from "./unsaved";

// A session's preview tabs, kept on the workbench.
export function usePreviewTabs(
  session: string | undefined,
  onError: (e: unknown) => void,
) {
  const [state, setState] = useState<PreviewTabs>(NO_TABS);
  const latest = useRef(state);
  latest.current = state;
  const current = useRef(session);
  current.current = session;
  const url = (s: string) => `sessions/${encodeURIComponent(s)}/preview-tabs`;

  const refresh = useCallback(() => {
    const s = current.current;
    if (!s) return;
    api<PreviewTabs>(url(s))
      .then((v) => current.current === s && setState(v))
      .catch(() => {});
  }, []);

  useEffect(() => {
    setState(NO_TABS);
    if (session) refresh();
  }, [session, refresh]);

  const save = useCallback(
    (next: PreviewTabs) => {
      setState(next);
      latest.current = next;
      const s = current.current;
      if (!s) return;
      api<PreviewTabs>(url(s), "PUT", stored(next))
        .then((v) => current.current === s && setState(v))
        .catch(onError);
    },
    [onError],
  );

  // What would close the tab shown asks first while it edits a file with
  // changes not saved.
  const shown = () => latest.current.tabs[latest.current.active];
  return {
    state,
    refresh,
    open: (tab: PreviewTab) => {
      const now = shown();
      if (!(now && sameTarget(now, tab)) && !mayLeave()) return;
      save(openTab(latest.current, tab));
    },
    close: (i: number) => {
      if (i === latest.current.active && !mayLeave()) return;
      save(closeTab(latest.current, i));
    },
    show: (i: number) => {
      if (i === latest.current.active) return;
      if (!mayLeave()) return;
      save({ ...latest.current, active: i });
    },
    update: (i: number, patch: Partial<PreviewTab>) =>
      save(updateTab(latest.current, i, patch)),
  };
}

export type PreviewTabsApi = ReturnType<typeof usePreviewTabs>;
