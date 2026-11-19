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

// A background run in the repository of assistants and the commit it made
// (internal/workbench/assistant_background.go, ADR 0011).
export type AssistantChange = {
  id: string;
  assistant: string;
  kind: "ingest" | "routine";
  title: string;
  session?: string;
  source?: string;
  run?: string;
  started: string;
  finished?: string;
  status: "running" | "done" | "failed" | "undone";
  commit?: string;
  files?: string[];
  summary?: string;
  error?: string;
};

// The latest changes, read again when one starts or ends ("assistant"
// events, App.tsx).
export function useChanges(enabled: boolean) {
  const [list, setList] = useState<AssistantChange[]>([]);
  const load = useCallback(async () => {
    try {
      setList(await api<AssistantChange[]>("assistants/changes"));
    } catch {
      // Kept as it was.
    }
  }, []);
  useEffect(() => {
    if (!enabled) {
      setList([]);
      return;
    }
    void load();
    const changed = () => void load();
    window.addEventListener("workbench:changes", changed);
    return () => window.removeEventListener("workbench:changes", changed);
  }, [enabled, load]);
  return list;
}

export const changesChanged = () =>
  window.dispatchEvent(new CustomEvent("workbench:changes"));

// What a change did, in a few words (its title is shown beside it).
export function changeText(c: AssistantChange): string {
  const memory = c.kind === "ingest";
  switch (c.status) {
    case "running":
      return memory ? "Updating memory…" : "Running…";
    case "failed":
      return `${memory ? "Memory update failed" : "Failed"}${c.error ? `: ${c.error}` : ""}`;
    case "undone":
      return memory ? "Memory update undone" : "Undone";
    case "done":
      if (!c.files?.length)
        return memory ? "Nothing new to remember" : "Nothing changed";
      if (memory) return `Memory updated${c.summary ? `: ${c.summary}` : ""}`;
      return c.summary || "Done";
  }
}

// The latest change from a conversation, for its banner.
export function latestFor(
  list: AssistantChange[],
  session: string | undefined,
): AssistantChange | undefined {
  if (!session) return undefined;
  return list.find((c) => c.kind === "ingest" && c.session === session);
}
