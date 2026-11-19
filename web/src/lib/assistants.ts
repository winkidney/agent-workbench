// Assistants (internal/workbench/assistant.go, ADR 0010): folders in one
// repository, each with an assistant.toml.
import { useCallback, useEffect, useState } from "react";
import { api } from "./api";

export type Assistant = {
  dir: string;
  path: string;
  name: string;
  agent: "claude" | "codex" | "";
  sessions: "per-task" | "continuous" | "";
  memory: "state" | "wiki" | "";
  routines: number;
  error?: string;
  current?: string;
  previous?: string[];
};

export type Assistants = { root: string; assistants: Assistant[] };

// What opening an assistant shows: its ongoing topic, or a new message
// (fresh), with the session it continues from.
export type Opened = {
  assistant: Assistant;
  session?: string;
  fresh?: boolean;
  previous?: string;
};

// The assistants, read again when the server says they changed
// ("assistants" events, App.tsx) or Settings changed them.
export function useAssistants(enabled: boolean) {
  const [value, setValue] = useState<Assistants>({ root: "", assistants: [] });
  const load = useCallback(async () => {
    try {
      setValue(await api<Assistants>("assistants"));
    } catch {
      // Kept as it was; the next change reads it again.
    }
  }, []);
  useEffect(() => {
    if (!enabled) return;
    void load();
    const changed = () => void load();
    window.addEventListener("workbench:assistants", changed);
    return () => window.removeEventListener("workbench:assistants", changed);
  }, [enabled, load]);
  return value;
}

export const assistantsChanged = () =>
  window.dispatchEvent(new CustomEvent("workbench:assistants"));

// The assistant a session belongs to: the one whose folder it started in.
export function assistantOf(
  list: Assistant[],
  cwd: string | undefined,
): Assistant | undefined {
  if (!cwd) return undefined;
  return list.find((a) => a.path === cwd);
}

// A folder name for a name, as the server makes it ("" when none).
export function folderFor(name: string): string {
  return name
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "");
}
