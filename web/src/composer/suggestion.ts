import { useEffect, useState } from "react";
import { api } from "../lib/api";

// After a turn, the composer offers what the user would likely type next
// (the server's telemetry "suggestion"; Claude Code makes it, Codex has none
// yet), as Claude Code's own prompt does: in grey in the empty prompt, Tab
// fills it in, Enter then sends it.

// The suggestion the composer shows now, if any: only in an empty prompt,
// between turns, in a session the page can write to, with no "/" menu open.
export function shownSuggestion(v: {
  suggestion?: string;
  prompt: string;
  running: boolean;
  blocked: boolean;
}): string | null {
  const text = v.suggestion?.trim();
  if (!text || v.prompt !== "" || v.running || v.blocked) return null;
  return text;
}

export type Preferences = { suggestions: boolean };

// The workbench's own settings, for every session and page.
export function usePreferences() {
  const [prefs, setPrefs] = useState<Preferences | null>(null);
  const [error, setError] = useState("");
  useEffect(() => {
    let live = true;
    api<Preferences>("preferences")
      .then((v) => live && setPrefs(v))
      .catch((e) => live && setError(e.message));
    return () => {
      live = false;
    };
  }, []);
  const update = async (next: Partial<Preferences>) => {
    setError("");
    try {
      setPrefs(await api<Preferences>("preferences", "PATCH", next));
    } catch (e) {
      setError((e as Error).message);
    }
  };
  return { prefs, error, update };
}
