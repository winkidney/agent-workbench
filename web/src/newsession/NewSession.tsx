import { useLayoutEffect, useMemo, useRef, useState } from "react";
import { ArrowUp, Loader2 } from "lucide-react";
import { Button } from "../components/ui/button";
import { NewSessionOptions, type Options } from "../AgentControls";
import { api } from "../lib/api";
import { randomUUID } from "../lib/uuid";
import { newlineKey } from "../lib/newline";
import { shortPath, type Prefs, type Session } from "../sidebar/tree";
import { ProjectPicker } from "./ProjectPicker";
import { AgentIcon } from "../components/AgentIcon";
import { recentProjects } from "./recent";
import { pastedImages, useAttachments } from "../composer/attachments";
import { useComposerInbox } from "../composer/inbox";
import {
  AddFiles,
  AttachmentStrip,
  useFileDrop,
} from "../composer/AttachmentStrip";
import type { ViewItem } from "../transcript/media";
import {
  assistantsChanged,
  MAIN_STARTERS,
  type Assistant,
} from "../lib/assistants";
import { OPTION_KEYS } from "../agentOptions";
import { QuickStart } from "../assistants/QuickStart";
import { AssistantHome } from "../assistants/lazy";

type Agent = "claude" | "codex";
const STORE = "workbench-new";

function remembered(): { agent?: Agent; cwd?: string } {
  try {
    return JSON.parse(localStorage.getItem(STORE) || "{}");
  } catch {
    return {};
  }
}

// Start a session in place, like a new chat: type the first message, pick the
// project and agent, and press Enter.
export function NewSession({
  sessions,
  workspaces,
  prefs,
  initialCwd,
  assistant,
  continues,
  onCreated,
  onView,
}: {
  sessions: Session[];
  workspaces: string[];
  prefs: Prefs;
  initialCwd?: string;
  // A message for an assistant: its folder and agent are its own, and the
  // server decides where it goes (lib/assistants.ts).
  assistant?: Assistant;
  // The title of the session this new topic continues from.
  continues?: string;
  onCreated: (s: Session) => void;
  // Opens added images in the viewer.
  onView?: (items: ViewItem[], index: number) => void;
}) {
  const recent = useMemo(
    () => recentProjects(sessions, prefs, workspaces),
    [sessions, prefs, workspaces],
  );
  const saved = remembered();
  const [chosenAgent, setAgent] = useState<Agent>(saved.agent ?? "claude");
  const [chosenCwd, setCwd] = useState(
    initialCwd || saved.cwd || recent[0]?.path || "",
  );
  const agent: Agent = (assistant?.agent || chosenAgent) as Agent;
  const cwd = assistant?.path ?? chosenCwd;
  const [options, setOptions] = useState<Options>({});
  // An assistant's settings as they were loaded, to offer saving changes
  // as its own (ADR 0017).
  const [started, setStarted] = useState<Options | null>(null);
  const [savedDefault, setSavedDefault] = useState(false);
  const changed =
    !!assistant &&
    !!started &&
    OPTION_KEYS.some((k) => (options[k] ?? "") !== (started[k] ?? ""));
  const saveAsDefault = async () => {
    if (!assistant || !started) return;
    const agentKey = agent as "claude" | "codex";
    const table = { ...(assistant.options?.[agentKey] ?? {}) };
    for (const k of OPTION_KEYS)
      if ((options[k] ?? "") !== (started[k] ?? ""))
        table[k] = options[k] ?? "";
    try {
      await api(`assistants/${assistant.dir}`, "PATCH", {
        options: { [agentKey]: table },
      });
      setStarted(options);
      setSavedDefault(true);
      assistantsChanged();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  };
  const [text, setText] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const input = useRef<HTMLTextAreaElement>(null);
  const images = useAttachments();
  // The media viewer's "Paste into message" lands here.
  useComposerInbox(images);
  const drop = useFileDrop(!busy, images.add);
  const sendable =
    (!!text.trim() || images.ready.length > 0) && !images.uploading;
  useLayoutEffect(() => {
    const el = input.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height =
      Math.min(el.scrollHeight, Math.round(innerHeight * 0.4)) + "px";
  }, [text]);
  const remember = (next: { agent?: Agent; cwd?: string }) => {
    try {
      localStorage.setItem(STORE, JSON.stringify({ ...remembered(), ...next }));
    } catch {}
  };
  const project = recent.find((p) => p.path === cwd);
  const submit = async () => {
    if (!sendable || !cwd || busy) return;
    setBusy(true);
    setError("");
    try {
      if (assistant) {
        onCreated(
          await api<Session>(`assistants/${assistant.dir}/message`, "POST", {
            text,
            images: images.images.map((a) => a.id),
            files: images.files.map((a) => a.id),
            options,
          }),
        );
        return;
      }
      remember({ agent, cwd });
      const s = await api<Session>("sessions", "POST", {
        agent,
        cwd,
        initialMessage: text,
        images: images.images.map((a) => a.id),
        files: images.files.map((a) => a.id),
        options,
        requestID: randomUUID(),
      });
      onCreated(s);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
      setBusy(false);
    }
  };
  const agentName = agent === "claude" ? "Claude Code" : "Codex";
  return (
    <div className="new-session">
      <div className="new-hero">
        {assistant ? (
          <>
            <p>
              {assistant.sessions === "continuous"
                ? continues
                  ? `A fresh conversation · picks up from ${continues}`
                  : "A fresh conversation"
                : "A new request"}
              {" · "}
              {agentName}
            </p>
          </>
        ) : (
          <>
            <h1>What should we work on?</h1>
            <p>
              {project?.name ??
                (cwd
                  ? cwd.split("/").filter(Boolean).pop()
                  : "Choose a project")}
              {" · "}
              {agentName}
            </p>
          </>
        )}
      </div>
      {/* The assistant's home card (ADR 0015); its examples fill the box. */}
      {assistant && (
        <AssistantHome
          assistant={assistant}
          onTry={(t) => {
            setText(t);
            requestAnimationFrame(() => {
              input.current?.focus();
              input.current?.setSelectionRange(t.length, t.length);
            });
          }}
        />
      )}
      {assistant?.dir === "main" && !assistant.examples?.length && !text && (
        <QuickStart
          starters={MAIN_STARTERS}
          onPick={(t) => {
            setText(t);
            requestAnimationFrame(() => {
              input.current?.focus();
              input.current?.setSelectionRange(t.length, t.length);
            });
          }}
        />
      )}
      <form
        className={
          "composer new-composer" +
          (busy ? " busy" : "") +
          (drop.over ? " dropping" : "")
        }
        onSubmit={(e) => {
          e.preventDefault();
          void submit();
        }}
        {...drop.handlers}
      >
        <AttachmentStrip
          items={images.items}
          notice={images.notice}
          onRemove={images.remove}
          onView={onView}
        />
        <textarea
          ref={input}
          rows={2}
          autoFocus
          aria-label="First message"
          placeholder={`Describe a task for ${agentName}…`}
          value={text}
          disabled={busy}
          onChange={(e) => setText(e.target.value)}
          onPaste={(e) => {
            const files = pastedImages(e.clipboardData);
            if (!files.length) return;
            e.preventDefault();
            images.add(files);
          }}
          onKeyDown={(e) => {
            if (newlineKey(e)) return;
            if (e.key === "Enter" && !e.nativeEvent.isComposing) {
              e.preventDefault();
              void submit();
            }
          }}
        />
        <div className="composer-bottom">
          <AddFiles disabled={busy} onFiles={images.add} />
          {!assistant && (
            <div className="agent-switch" role="radiogroup" aria-label="Agent">
              {(["claude", "codex"] as Agent[]).map((a) => (
                <button
                  type="button"
                  role="radio"
                  aria-checked={agent === a}
                  key={a}
                  className={agent === a ? "on" : ""}
                  disabled={busy}
                  onClick={() => {
                    setAgent(a);
                    setOptions({});
                    remember({ agent: a });
                  }}
                >
                  <AgentIcon agent={a} size={16} decorative />
                  {a === "claude" ? "Claude" : "Codex"}
                </button>
              ))}
            </div>
          )}
          {assistant ? (
            <NewSessionOptions
              agent={agent}
              cwd={cwd}
              value={options}
              onChange={(o) => {
                setOptions(o);
                setSavedDefault(false);
              }}
              defaultsFrom={`assistants/${assistant.dir}/options`}
              onDefaults={setStarted}
            />
          ) : (
            <NewSessionOptions
              agent={agent}
              cwd={cwd}
              value={options}
              onChange={setOptions}
            />
          )}
          {changed && (
            <button
              type="button"
              className="new-save-default"
              disabled={busy}
              title={`Use these settings for ${assistant?.name}'s new sessions`}
              onClick={() => void saveAsDefault()}
            >
              Save as {assistant?.name}'s default
            </button>
          )}
          {savedDefault && !changed && (
            <span className="new-hint" role="status">
              Saved as default
            </span>
          )}
          <div className="composer-actions">
            <Button
              size="icon"
              type="submit"
              className="composer-send"
              aria-label="Start session"
              title="Start (Enter)"
              disabled={busy || !sendable || !cwd}
            >
              {busy ? (
                <Loader2 size={15} className="spin" />
              ) : (
                <ArrowUp size={16} />
              )}
            </Button>
          </div>
        </div>
      </form>
      <div className="new-meta">
        {assistant ? (
          <span className="new-hint" title={assistant.path}>
            {shortPath(assistant.path)}
          </span>
        ) : (
          <ProjectPicker
            value={cwd}
            recent={recent}
            onChange={(p) => {
              setCwd(p);
              remember({ cwd: p });
            }}
          />
        )}
        <span className="new-hint">
          {busy ? `Starting ${agentName}…` : "↵ start · ⇧↵ / Alt↵ new line"}
        </span>
      </div>
      {error && (
        <p role="alert" className="error-text new-error">
          {error}
        </p>
      )}
    </div>
  );
}
