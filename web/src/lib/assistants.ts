// Assistants (internal/workbench/assistant.go, ADR 0010): folders in one
// repository, each with an assistant.toml.
import { useCallback, useEffect, useRef, useState } from "react";
import { api } from "./api";
import type { Options } from "../agentOptions";

export type Assistant = {
  dir: string;
  path: string;
  name: string;
  agent: "claude" | "codex" | "";
  sessions: "per-task" | "continuous" | "";
  memory: "state" | "wiki" | "";
  routines: number;
  // Turned on; a turned-off one is under Show more and its routines wait.
  enabled: boolean;
  // Its home card (assistant.toml; ADR 0015), and whether it is set up.
  description?: string;
  capabilities?: string[];
  examples?: string[];
  // What it needs from the user (a signed-in CLI, an MCP server, a token).
  requires?: string[];
  ready: boolean;
  // Its own session settings by agent (assistant.toml [options.<agent>],
  // ADR 0017); what one leaves out comes from Settings → Agents.
  options?: Partial<Record<"claude" | "codex", Options>>;
  error?: string;
  current?: string;
  previous?: string[];
  // Its icon (ADR 0030): a built-in drawing's name, or "file" for the image
  // in its folder, read at iconVersion (its time, to load it again).
  icon?: string;
  iconVersion?: number;
  // The agents its author made it for; none means both (ADR 0031).
  agents?: Agent[];
  // The catalog it was installed from, and its definition files the user
  // changed or lacks.
  catalog?: {
    url: string;
    path: string;
    commit: string;
    changed?: string[];
    missing?: string[];
    update?: CatalogUpdate;
  };
};

// What updating an installed assistant from its catalog does (ADR 0031).
export type CatalogUpdate = {
  dir: string;
  catalog: string;
  from: string;
  commit?: string;
  applied?: boolean;
  files?: {
    rel: string;
    result:
      "update" | "merge" | "add" | "same" | "delete" | "kept" | "conflict";
    runs?: boolean;
    base?: string;
    theirs?: string;
  }[];
};

export const UPDATE_RESULT: Record<string, string> = {
  update: "the catalog's new version",
  merge: "the catalog's change merged into yours",
  add: "new",
  same: "new, already the same here",
  delete: "removed",
  kept: "removed there, yours kept",
  conflict: "changed there and here: choose",
};

type Agent = "claude" | "codex";

// An assistant a catalog offers (ADR 0031).
export type CatalogEntry = {
  catalog: string;
  path: string;
  commit: string;
  name: string;
  description?: string;
  capabilities?: string[];
  requires?: string[];
  agent: Agent;
  agents?: Agent[];
  icon?: string;
  installed?: string;
  codexWarning?: string;
};

// What binding an older repository to a catalog does to one assistant.
export type MigrationItem = {
  dir: string;
  catalog?: string;
  fromCatalog?: string[];
  changed?: string[];
  added?: string[];
  settings?: string[];
  skipped?: string;
};

// The agents an assistant can be installed for: its author's choice, or
// both.
export const agentsOf = (e: { agents?: Agent[] }): Agent[] =>
  e.agents?.length ? e.agents : ["claude", "codex"];

// A catalog's address as people say it: owner/name for GitHub.
export const catalogName = (url: string) =>
  url
    .replace(/^https:\/\/github\.com\//, "")
    .replace(/^file:\/\//, "")
    .replace(/\.git$/, "");

export type Assistants = {
  root: string;
  assistants: Assistant[];
  // The agent setting up would choose ("" when neither is installed).
  defaultAgent?: "claude" | "codex" | "";
  // The page has heard from the server (until then root is unknown).
  loaded?: boolean;
  // The language assistants write to the user in.
  language?: Language;
  // The backup of the repository to its remote (sync.go).
  sync?: SyncState;
  // Moss's files the user changed that have a newer shipped version.
  mossUpdates?: string[];
  // Which agents' programs are installed.
  installed?: { claude?: boolean; codex?: boolean };
};

// Asks App.tsx to open an assistant's settings (assistants/AssistantSettings).
export const ASSISTANT_SETTINGS_EVENT = "workbench:assistant-settings";
export const openAssistantSettings = (dir: string) =>
  window.dispatchEvent(
    new CustomEvent(ASSISTANT_SETTINGS_EVENT, { detail: dir }),
  );

// An assistant's routines: those in its file, and those sent to it.
export function routinesOf<
  R extends {
    createdBy: string;
    target?: { kind: string; assistant?: string };
  },
>(list: R[], dir: string): R[] {
  return list.filter(
    (r) =>
      r.createdBy === "file:" + dir ||
      (r.target?.kind === "assistant" && r.target.assistant === dir),
  );
}

// Starts an assistant's set-up conversation and opens it.
export const setUpAssistant = (dir: string) =>
  api<{ session: string }>(`assistants/${dir}/setup`, "POST").then((o) =>
    openSessionLater(o.session),
  );

export type Language = "en" | "zh-Hans" | "zh-Hant";
export const LANGUAGES: [Language, string][] = [
  ["en", "English"],
  ["zh-Hans", "简体中文"],
  ["zh-Hant", "繁體中文"],
];

export type SyncState = {
  remote?: string;
  // Commits not on the remote yet; -1 before the first push.
  ahead: number;
  pushed?: string;
  error?: string;
};

// Asks App.tsx to show a session (from Settings, after a welcome or a new
// assistant's first message).
export const OPEN_SESSION_EVENT = "workbench:open-session";
export const openSessionLater = (id: string) =>
  window.dispatchEvent(new CustomEvent(OPEN_SESSION_EVENT, { detail: id }));

// Asks App.tsx to open the routine dialog, aimed at the personal assistant
// when there is one.
export const NEW_ROUTINE_EVENT = "workbench:new-routine";
export const newRoutine = (assistant = "main") =>
  window.dispatchEvent(
    new CustomEvent(NEW_ROUTINE_EVENT, { detail: assistant }),
  );

// The folders in their new order after moving one to where another is
// (before it, or after when moving down); the personal assistant stays first.
export function moved(dirs: string[], dir: string, to: string): string[] {
  if (dir === to || dir === "main" || !dirs.includes(dir)) return dirs;
  const from = dirs.indexOf(dir);
  const rest = dirs.filter((d) => d !== dir);
  let at = rest.indexOf(to);
  if (at < 0) return dirs;
  if (from < dirs.indexOf(to)) at += 1;
  rest.splice(at, 0, dir);
  return ["main", ...rest.filter((d) => d !== "main")].filter(
    (d) => d !== "main" || dirs.includes("main"),
  );
}

// Keeps the order the user chose (PATCH settings).
export const saveOrder = (dirs: string[]) =>
  api("assistants/settings", "PATCH", {
    order: dirs.filter((d) => d !== "main"),
  }).then(assistantsChanged);

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
  // The first load of the page reads the files again (they are not
  // watched); later ones use what the server has.
  const first = useRef(true);
  const load = useCallback(async () => {
    try {
      const fresh = first.current;
      first.current = false;
      setValue({
        ...(await (fresh
          ? api<Assistants>("assistants/reload", "POST")
          : api<Assistants>("assistants"))),
        loaded: true,
      });
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

// Reads the assistant.toml files again: after editing them by hand.
export const reloadAssistants = () =>
  api("assistants/reload", "POST").then(assistantsChanged);

// Asks App.tsx to show the set-up dialog (assistants/SetUpDialog.tsx): the
// first use of assistants before they are set up.
export const SET_UP_EVENT = "workbench:set-up-assistants";
export const setUpAssistants = () =>
  window.dispatchEvent(new CustomEvent(SET_UP_EVENT));

// What the personal assistant is called before the user names it.
export const MAIN_NAME = "Moss";

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

// Ways to start with the personal assistant (assistants/QuickStart.tsx):
// each puts the start of a message in the composer, to finish and send.
// A start either fills in text, or (action) opens something.
export type Starter = { label: string; text: string; action?: "routine" };
export const MAIN_STARTERS: Starter[] = [
  { label: "Remind me", text: "Remind me in 2 hours to " },
  { label: "Schedule a routine", text: "", action: "routine" },
  {
    label: "New assistant",
    text: "I'd like an assistant for a job I keep doing: ",
  },
  {
    label: "My assistants",
    text: "Which assistants do I have, and what does each do?",
  },
  {
    label: "What can you do?",
    text: "What can you do for me? Give me a few examples.",
  },
];

// Carries out a start: opens what it names, or hands its text on.
export function pickStarter(s: Starter, fill: (text: string) => void) {
  if (s.action === "routine") newRoutine();
  else fill(s.text);
}

// The assistant a routine belongs to: the one whose file has it, or the
// one it is sent to; any other routine is Moss's.
export const routineOwner = (r: {
  createdBy: string;
  target?: { kind: string; assistant?: string };
}) =>
  r.createdBy.startsWith("file:")
    ? r.createdBy.slice(5)
    : r.target?.kind === "assistant" && r.target.assistant
      ? r.target.assistant
      : "main";

// Routines grouped by the assistant they belong to (routineOwner). Groups
// follow the assistants' order, Moss first.
export function groupRoutines<
  R extends {
    createdBy: string;
    target?: { kind: string; assistant?: string };
  },
>(
  list: R[],
  assistants: Pick<Assistant, "dir" | "name">[],
): { dir: string; name: string; items: R[] }[] {
  const groups = new Map<string, R[]>();
  for (const r of list) {
    const d = routineOwner(r);
    groups.set(d, [...(groups.get(d) ?? []), r]);
  }
  const order = [
    "main",
    ...assistants.map((a) => a.dir).filter((d) => d !== "main"),
  ];
  const dirs = [...groups.keys()].sort((a, b) => {
    const ia = order.indexOf(a),
      ib = order.indexOf(b);
    return (ia < 0 ? 1e6 : ia) - (ib < 0 ? 1e6 : ib) || a.localeCompare(b);
  });
  return dirs.map((dir) => ({
    dir,
    name:
      assistants.find((a) => a.dir === dir)?.name ??
      (dir === "main" ? MAIN_NAME : dir),
    items: groups.get(dir)!,
  }));
}
