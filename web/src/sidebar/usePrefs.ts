import { useCallback, useEffect, useRef, useState } from "react";
import { api } from "../lib/api";
import { applyOp, emptyPrefs, type Prefs, type PrefsOp } from "./tree";

// Workbench-side pins, hidden sessions and display aliases, shared through the
// server; other browsers receive changes as `prefs` events.
export function usePrefs(paired: boolean, onError: (e: unknown) => void) {
  const [prefs, setPrefs] = useState<Prefs>(emptyPrefs);
  const errorRef = useRef(onError);
  errorRef.current = onError;
  const reload = useCallback(
    () =>
      api<Prefs>("sidebar")
        .then((p) => setPrefs({ ...emptyPrefs(), ...p }))
        .catch((e) => errorRef.current(e)),
    [],
  );
  useEffect(() => {
    if (paired) void reload();
  }, [paired, reload]);
  const update = useCallback(
    async (op: PrefsOp, id: string) => {
      setPrefs((p) => applyOp(p, op, id));
      try {
        setPrefs({
          ...emptyPrefs(),
          ...(await api<Prefs>("sidebar", "POST", { op, id })),
        });
      } catch (e) {
        errorRef.current(e);
        void reload();
      }
    },
    [reload],
  );
  const rename = useCallback(async (id: string, title: string) => {
    setPrefs((p) => {
      const aliases = { ...p.aliases };
      if (title.trim()) aliases[id] = title.trim();
      else delete aliases[id];
      return { ...p, aliases };
    });
    return api<{ native: boolean }>(
      `sessions/${encodeURIComponent(id)}/rename`,
      "POST",
      { title },
    );
  }, []);
  const receive = useCallback((p: unknown) => {
    if (p && typeof p === "object")
      setPrefs({ ...emptyPrefs(), ...(p as Prefs) });
  }, []);
  return { prefs, update, rename, receive, reload };
}
