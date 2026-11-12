import { NewSession } from "./newsession/NewSession";
import { AgentIcon } from "./components/AgentIcon";
import { ResizeHandle } from "./layout/ResizeHandle";
import {
  clampWidths,
  loadWidths,
  saveWidths,
  type Side,
  type Widths,
} from "./layout/widths";
import { Composer } from "./composer/Composer";

// What became of a message sent to steer (see Manager.Steer).
type SteerResult = {
  steered?: boolean;
  queued?: boolean;
  sent?: boolean;
  reason?: string;
};
import { Sidebar, statusLabels as labels } from "./sidebar/Sidebar";
import { usePrefs } from "./sidebar/usePrefs";
import { displayTitle, type Session } from "./sidebar/tree";
import { api } from "./lib/api";
import { Appearance } from "./components/Appearance";
import { NotifyToggle } from "./components/NotifyToggle";
import { SuggestToggle } from "./components/SuggestToggle";
import { useAttention } from "./attention";
import { SettingsDialog } from "./components/SettingsDialog";
import { canonicalAddress, fileFrameSandbox, previewMode } from "./lib/preview";
import { useEffect, useMemo, useRef, useState } from "react";
import { Timeline } from "./transcript/Timeline";
import { IMPLEMENT_PLAN, freshPlanMessage } from "./transcript/plans";
import type { Options } from "./agentOptions";
import { useTranscript, type ExternalUpdate } from "./transcript/useTranscript";
import {
  EnvContext,
  isMedia,
  pathItem,
  type Env,
  type ViewItem,
} from "./transcript/media";
import { MediaViewer } from "./media/MediaViewer";
import { withSending } from "./transcript/reducer";
import { ApprovalCard, type Decision } from "./approval/ApprovalCard";
import { StatusLine, activity, turnClock } from "./composer/StatusLine";
import { GoalBar } from "./composer/GoalBar";
import { goalChanged, useGoal } from "./composer/goal";
import { BackgroundTray } from "./composer/BackgroundTray";
import {
  backgroundChanged,
  backgroundable,
  useBackground,
} from "./composer/background";
import { QueueList } from "./composer/QueueList";
import { TodoPanel, latestPlan } from "./composer/TodoPanel";
import type { WorkbenchEvent } from "./transcript/model";
import {
  Square,
  Moon,
  Play,
  Folder,
  File,
  X,
  Globe,
  PanelRightClose,
  Layers,
  PanelLeft,
  MoreHorizontal,
  ChevronRight,
  RefreshCw,
  Settings,
  Archive,
  Trash2,
  ExternalLink,
  CornerLeftUp,
} from "lucide-react";
import { Button } from "./components/ui/button";
import { Input } from "./components/ui/input";

import { Dialog, DialogContent, DialogTitle } from "./components/ui/dialog";

type Event = WorkbenchEvent;
type Preview = {
  id: string;
  session: string;
  url: string;
  entry: string;
  owned: boolean;
  // A file preview on the workbench's own address, for this page only.
  fileUrl?: string;
};
type Entry = { name: string; path: string; directory: boolean; size: number };
export default function App() {
  const [workspaces, setWorkspaces] = useState<string[]>([]);
  const [left, setLeft] = useState(
    () =>
      localStorage.getItem("workbench-left") !== "false" && innerWidth > 850,
  );
  const [connectionError, setConnectionError] = useState("");
  const [authReady, setAuthReady] = useState(false);
  const [settingsOpen, setSettingsOpen] = useState(false);
  // When this page last sent a message: the turn clock starts there, not
  // when the engine has finished starting.
  const lastSent = useRef<{ session: string; at: number } | null>(null);
  // A message sent from this page, shown until the server echoes it.
  const [sending, setSending] = useState<{
    session: string;
    text: string;
    at: number;
    from: number;
  } | null>(null);
  // Images and videos open in the popup viewer.
  const [viewer, setViewer] = useState<{
    items: ViewItem[];
    index: number;
  } | null>(null);
  // Set when the server now serves a newer web build than this page runs.
  const [outdated, setOutdated] = useState(false);
  const [paired, setPaired] = useState(false),
    [code, setCode] = useState(""),
    [error, setError] = useState(""),
    [sessions, setSessions] = useState<Session[]>([]),
    [active, setActive] = useState(""),
    [busy, setBusy] = useState(false),
    [composing, setComposing] = useState<{ cwd?: string } | null>(null),
    [entries, setEntries] = useState<Entry[]>([]),
    [path, setPath] = useState(""),
    [preview, setPreview] = useState<Preview | null>(null),
    [previews, setPreviews] = useState<Preview[]>([]),
    [serviceOpen, setServiceOpen] = useState(false),
    // The Codex session waiting for the user to confirm its deletion.
    [deleting, setDeleting] = useState<Session | null>(null),
    // Why the last steer was queued instead (shown with the queue).
    [steerNote, setSteerNote] = useState(""),
    [deleteBusy, setDeleteBusy] = useState(false),
    [target, setTarget] = useState("http://127.0.0.1:5173"),
    [command, setCommand] = useState(""),
    [port, setPort] = useState("5173"),
    [panel, setPanel] = useState(
      () =>
        localStorage.getItem("workbench-right") !== "false" && innerWidth > 850,
    );
  useEffect(() => localStorage.setItem("workbench-left", String(left)), [left]);
  useEffect(
    () => localStorage.setItem("workbench-right", String(panel)),
    [panel],
  );
  useEffect(() => {
    if (!paired) return;
    const timer = setInterval(() => {
      if (document.visibilityState === "visible")
        void refresh().catch(catchError);
    }, 10000);
    return () => clearInterval(timer);
  }, [paired]);
  const selected = useRef(active);
  selected.current = active;
  const streamCursor = useRef(0);
  const current = sessions.find((s) => s.id === active);
  const [verbose, setVerbose] = useState(false);
  // Side panel widths: dragged by the handles, saved per browser.
  const [widths, setWidths] = useState<Widths>(loadWidths);
  const [viewport, setViewport] = useState(() => innerWidth);
  useEffect(() => {
    const resize = () => setViewport(innerWidth);
    window.addEventListener("resize", resize);
    return () => window.removeEventListener("resize", resize);
  }, []);
  const composerRef = useRef<HTMLTextAreaElement>(null);
  const running = ["running", "waiting", "starting"].includes(
    current?.status ?? "",
  );
  const goal = useGoal(current?.id);
  const background = useBackground(current?.id);
  // Asked before putting an engine with background work to sleep.
  const [sleepAsk, setSleepAsk] = useState(false);
  const transcript = useTranscript(
    current
      ? { id: current.id, agent: current.agent, status: current.status }
      : undefined,
    (e) => setError(e instanceof Error ? e.message : String(e)),
  );
  const pushEvent = useRef(transcript.push);
  pushEvent.current = transcript.push;
  const pageBuild = useRef<string | undefined>(undefined);
  const externalEvent = useRef(transcript.external);
  externalEvent.current = transcript.external;
  const catchError = (e: unknown) =>
    setError(e instanceof Error ? e.message : String(e));
  const sidebarPrefs = usePrefs(paired, catchError);
  const receivePrefs = useRef(sidebarPrefs.receive);
  receivePrefs.current = sidebarPrefs.receive;
  const refresh = async () => {
    setSessions(await api<Session[]>("sessions"));
    setWorkspaces(await api<string[]>("workspaces"));
    setPreviews(await api<Preview[]>("previews"));
  };
  useEffect(() => {
    const requirePairing = () => {
      setAuthReady(true);
      setSettingsOpen(false);
      setPaired(false);
      setSessions([]);
      setActive("");
      setEntries([]);
      setPreview(null);
      setPreviews([]);
      setComposing(null);
      setServiceOpen(false);
      setCode("");
      streamCursor.current = 0;
    };
    window.addEventListener("workbench:unauthorized", requirePairing);
    return () =>
      window.removeEventListener("workbench:unauthorized", requirePairing);
  }, []);
  const bootstrap = async () => {
    setAuthReady(false);
    setConnectionError("");
    setError("");
    try {
      const status = await api<{ authenticated: boolean }>("auth/status");
      if (status.authenticated) {
        const list = await api<Session[]>("sessions");
        setSessions(list);
        setPaired(true);
        if (list.length) setActive(list[0].id);
        void api<string[]>("workspaces").then(setWorkspaces).catch(catchError);
      } else setPaired(false);
    } catch (e) {
      setConnectionError(e instanceof Error ? e.message : String(e));
    } finally {
      setAuthReady(true);
    }
  };
  useEffect(() => {
    void bootstrap();
  }, []);
  const openSettings = () => setSettingsOpen(true);
  useEffect(() => {
    if (!paired) return;
    const source = new EventSource(
      "/api/events?after=" +
        streamCursor.current +
        "&session=" +
        encodeURIComponent(active),
    );
    source.addEventListener("telemetry", (e) =>
      window.dispatchEvent(
        new CustomEvent("workbench:telemetry", {
          detail: {
            session: active,
            data: JSON.parse((e as MessageEvent).data),
          },
        }),
      ),
    );
    // Each connection announces the server's web build; a restart onto a new
    // build shows up as a different one.
    source.addEventListener("hello", (e) => {
      const build = (JSON.parse((e as MessageEvent).data) as { build?: string })
        .build;
      if (!build) return;
      if (pageBuild.current === undefined) pageBuild.current = build;
      else if (pageBuild.current !== build) setOutdated(true);
    });
    // Another client is running this session: new records and run state.
    source.addEventListener("external", (e) => {
      const u = JSON.parse((e as MessageEvent).data) as ExternalUpdate;
      externalEvent.current(u);
      if (u.external)
        setSessions((old) =>
          old.map((s) =>
            s.id === u.session && s.status !== u.status
              ? { ...s, status: u.status }
              : s,
          ),
        );
      // The other client let go: the list knows the new status and access.
      else void refresh().catch(catchError);
    });
    source.onmessage = (e) => {
      const v = JSON.parse(e.data) as Event;
      streamCursor.current = v.seq;
      if (v.kind === "status") {
        setSessions((old) => {
          const s = v.data as unknown as Session;
          if (!s.nativeId || s.id !== `${s.agent}:${s.nativeId}`) return old;
          return [s, ...old.filter((x) => x.id !== s.id)];
        });
      }
      if (v.kind === "deleted") {
        forget.current(v.session);
        return;
      }
      if (v.kind === "prefs") receivePrefs.current(v.data);
      else if (v.session === selected.current) pushEvent.current(v);
    };
    source.onerror = () => {
      // EventSource hides HTTP status; probe authentication on connection failure.
      void api<Session[]>("sessions").catch(() => {});
    };
    return () => source.close();
  }, [paired, active]);
  useEffect(() => {
    if (!paired || !active) return;
    setPath("");
    setPreview(null);
  }, [active, paired]);
  // New outputs usually appear when a turn ends; refresh the file list then.
  const status = current?.status;
  const [filesVersion, setFilesVersion] = useState(0);
  const lastStatus = useRef(status);
  useEffect(() => {
    if (
      lastStatus.current &&
      lastStatus.current !== "idle" &&
      status === "idle"
    )
      setFilesVersion((v) => v + 1);
    lastStatus.current = status;
  }, [status]);
  useEffect(() => {
    if (!paired || !active) return;
    let cancelled = false;
    api<Entry[]>(
      "files?session=" + active + "&path=" + encodeURIComponent(path),
    )
      .then((v) => {
        if (!cancelled) setEntries(v);
      })
      .catch(catchError);
    return () => {
      cancelled = true;
    };
  }, [active, path, paired, filesVersion]);
  const act = async (action: string, body?: unknown) => {
    setBusy(true);
    setError("");
    try {
      await api("sessions/" + active + "/" + action, "POST", body);
      await refresh();
    } catch (e) {
      catchError(e);
      throw e;
    } finally {
      setBusy(false);
    }
  };
  const selectSession = (id: string) => {
    setComposing(null);
    setActive(id);
    // On narrow screens the sidebar overlays the conversation.
    if (innerWidth <= 850) setLeft(false);
  };
  // Sessions waiting for the user: the tab title counts them, and a desktop
  // notification announces each new one while the page is in the background.
  const titled = useMemo(
    () =>
      sessions.map((s) => ({
        id: s.id,
        agent: s.agent,
        attention: s.attention,
        title: displayTitle(s, sidebarPrefs.prefs),
      })),
    [sessions, sidebarPrefs.prefs],
  );
  const waiting = useAttention(titled, paired, (id) => selectSession(id));
  const waitingElsewhere = waiting.some((s) => s.id !== active);
  // The Claude command or subagent a running turn waits on, which Ctrl+B or
  // the status line's button moves to the background.
  const backgroundTool = useMemo(() => {
    if (current?.agent !== "claude" || current.status !== "running")
      return undefined;
    const keys = transcript.state.keys.slice(-40);
    return backgroundable(keys.map((k) => transcript.state.blocks.get(k)!));
  }, [current?.agent, current?.status, transcript.state]);
  const moveToBackground =
    current && backgroundTool
      ? () =>
          void api(
            `sessions/${encodeURIComponent(current.id)}/to-background`,
            "POST",
            { toolUseId: backgroundTool },
          )
            .then(() => backgroundChanged(current.id))
            .catch(catchError)
      : undefined;
  // New sessions start inline in the conversation area, like a new chat.
  const openNewSession = (dir?: string) => {
    setComposing({ cwd: dir });
    if (innerWidth <= 850) setLeft(false);
  };
  // A deleted session leaves the list; if it was open, a new session takes
  // its place (here or on another page).
  const forgetSession = (id: string) => {
    setSessions((old) => old.filter((s) => s.id !== id));
    if (selected.current === id) {
      const gone = sessions.find((s) => s.id === id);
      setActive("");
      setComposing({ cwd: gone?.cwd });
    }
  };
  const forget = useRef(forgetSession);
  forget.current = forgetSession;
  // "Send now" on a Claude steer that is still waiting (see Timeline).
  useEffect(() => {
    const sendNow = (e: globalThis.Event) => {
      const uuid = (e as CustomEvent<string>).detail;
      if (selected.current)
        void api(`sessions/${selected.current}/send-now`, "POST", {
          uuid,
        }).catch(catchError);
    };
    window.addEventListener("workbench:send-now", sendNow);
    return () => window.removeEventListener("workbench:send-now", sendNow);
  }, []);
  // Native Codex thread operations, for any session in the sidebar.
  const archiveSession = (s: Session, archived: boolean) =>
    void api(`sessions/${s.id}/${archived ? "archive" : "unarchive"}`, "POST")
      .then(refresh)
      .catch(catchError);
  const deleteSession = async (s: Session) => {
    setDeleteBusy(true);
    try {
      await api(`sessions/${s.id}/delete`, "POST");
      forgetSession(s.id);
      setDeleting(null);
      await refresh();
    } catch (e) {
      catchError(e);
      setDeleting(null);
    } finally {
      setDeleteBusy(false);
    }
  };
  const renameSession = (id: string, title: string) =>
    void sidebarPrefs
      .rename(id, title)
      .then((r) => {
        if (r.native) return refresh();
      })
      .catch(catchError);
  const authorize = (p: Preview, grant: string) => {
    const mode = previewMode(p, location.host);
    if (mode === "unavailable") {
      setError(
        "Web previews only open in a browser on the workbench computer, at " +
          canonicalAddress(p) +
          ". Files open here.",
      );
      return;
    }
    setPreview(p);
    setPanel(true);
    // A file on the workbench's own address loads as the frame's src.
    if (mode === "file") return;
    setTimeout(() => {
      const form = document.createElement("form");
      form.method = "POST";
      form.action = p.url + "/__authorize";
      form.target = "artifact-preview";
      const input = document.createElement("input");
      input.name = "grant";
      input.value = grant;
      form.appendChild(input);
      document.body.appendChild(form);
      form.submit();
      form.remove();
    }, 50);
  };
  const openPreview = async (body: unknown) => {
    try {
      const result = await api<{ preview: Preview; grant: string }>(
        "previews",
        "POST",
        body,
      );
      authorize(result.preview, result.grant);
      setServiceOpen(false);
      await refresh();
    } catch (e) {
      catchError(e);
    }
  };
  const approvals = transcript.approvals;
  // Sends from the composer, or answers an agent's questions: into the
  // running turn (steer), as the next message, or queued for after the turn.
  const sendMessage = async (text: string, how: "send" | "steer" | "queue") => {
    if (!current) return;
    if (how === "steer") {
      // Into the running turn; the server shows it as steered,
      // or queues it (and says why) when the turn cannot take it.
      setError("");
      try {
        const res = await api<SteerResult>(
          "sessions/" + current.id + "/send",
          "POST",
          { text, steer: true },
        );
        setSteerNote(res.queued ? (res.reason ?? "") : "");
      } catch (e) {
        catchError(e);
        throw e;
      }
      return;
    }
    const queue = how === "queue";
    // Queued messages show in the queue; others show at once.
    if (!queue) lastSent.current = { session: current.id, at: Date.now() };
    if (!queue)
      setSending({
        session: current.id,
        text,
        at: Date.now(),
        from: transcript.state.keys.length,
      });
    try {
      await act("send", { text, queue });
    } finally {
      // Delivered (the server echoed it) or failed.
      setSending(null);
    }
  };
  // Questions answered in the transcript go out as the composer would send.
  const answerRef = useRef<(text: string) => Promise<void>>(async () => {});
  answerRef.current = (text) => sendMessage(text, running ? "steer" : "send");
  // Workbench "/" commands from the composer, and the plan choices.
  const runCommand = async (name: string, args: string) => {
    if (!current) return;
    setError("");
    try {
      const res = await api<{
        session?: string;
        options?: Record<string, string>;
      }>(`sessions/${encodeURIComponent(current.id)}/command`, "POST", {
        name,
        args,
      });
      // Settings commands show in the pills right away.
      if (res.options)
        window.dispatchEvent(
          new CustomEvent("workbench:options", {
            detail: { session: current.id, options: res.options },
          }),
        );
      // /goal leaves the list as it was, and Codex starts the goal's turn
      // just after answering: a list read now could arrive after that
      // turn's "running" and put the session back to idle.
      if (name === "goal") goalChanged(current.id);
      else await refresh();
      // /fork opens the new session.
      if (res.session) selectSession(res.session);
    } catch (e) {
      catchError(e);
      throw e;
    }
  };
  // Codex's plan, implemented in a new session in the same folder with this
  // session's settings outside plan mode, as Codex's "clear context" does.
  const startFromPlan = async (plan: string) => {
    if (!current) return;
    try {
      const { options } = await api<{ options: Options }>(
        `sessions/${encodeURIComponent(current.id)}/telemetry`,
      );
      const s = await api<Session>("sessions", "POST", {
        agent: current.agent,
        cwd: current.cwd,
        initialMessage: freshPlanMessage(plan),
        options: { ...options, mode: "default" },
        requestID: crypto.randomUUID(),
      });
      setSessions((old) => [s, ...old.filter((x) => x.id !== s.id)]);
      selectSession(s.id);
      void refresh().catch(catchError);
    } catch (e) {
      catchError(e);
      throw e;
    }
  };
  const planRef = useRef({ runCommand, startFromPlan });
  planRef.current = { runCommand, startFromPlan };
  // This session's subagents in the session list: rows link to them. A key,
  // so the page re-renders only when they change, not with each list update.
  const childKey = useMemo(
    () =>
      sessions
        .filter((s) => s.parentId && s.parentId === current?.id)
        .map((s) => s.id)
        .join("\n"),
    [sessions, current?.id],
  );
  const env = useMemo<Env>(
    () => ({
      cwd: current?.cwd ?? "",
      openUrl: (url) => {
        const target = url.replace(
          /^(https?:\/\/)(localhost|0\.0\.0\.0|\[::1\])/i,
          "$1127.0.0.1",
        );
        void openPreview({ session: active, kind: "service", target });
      },
      openPath: (path, base) =>
        void openPreview({ session: active, kind: "file", path, base }),
      thumb: (path, base) =>
        `/api/media?session=${encodeURIComponent(active)}&path=${encodeURIComponent(path)}` +
        (base ? `&base=${encodeURIComponent(base)}` : ""),
      reveal: (path, base) =>
        void api(`sessions/${encodeURIComponent(active)}/reveal`, "POST", {
          path,
          base,
        }).catch(catchError),
      view: (items, index) => items.length && setViewer({ items, index }),
      subagentSession: ({ agentId, threadId }) => {
        const id =
          current?.agent === "claude" && agentId
            ? `${current.id}:agent-${agentId}`
            : current?.agent === "codex" && threadId
              ? `codex:${threadId}`
              : "";
        return id && childKey.split("\n").includes(id) ? id : undefined;
      },
      listed: (id) => childKey.split("\n").includes(id),
      openSession: (id) => selectSession(id),
      // Read-only and archived sessions take no messages here.
      answer:
        current && !current.readOnly && !current.archived
          ? (text) => answerRef.current(text)
          : undefined,
      ...(current && !current.readOnly && !current.archived
        ? {
            implementPlan: () =>
              planRef.current.runCommand("default", IMPLEMENT_PLAN),
            implementPlanFresh: (plan: string) =>
              planRef.current.startFromPlan(plan),
            dismissPlan: () =>
              void api(
                `sessions/${encodeURIComponent(active)}/plan-dismiss`,
                "POST",
              ).catch(() => {}),
          }
        : {}),
    }),
    [
      active,
      current?.cwd,
      current?.readOnly,
      current?.archived,
      current?.agent,
      current?.id,
      childKey,
    ],
  );
  // A subagent's session: the session that started it.
  const parentSession = current?.parentId
    ? sessions.find((s) => s.id === current.parentId)
    : undefined;
  const plan = useMemo(() => latestPlan(transcript.state), [transcript.state]);
  // The echo of a sent message gives way to the server's own as soon as it
  // arrives; the engine may need seconds to start (Codex waits for its MCP
  // servers) before the message is delivered.
  const pendingSend = sending?.session === current?.id ? sending : null;
  useEffect(() => {
    if (!sending) return;
    const echoed = transcript.state.keys.slice(sending.from).some((k) => {
      const b = transcript.state.blocks.get(k)!;
      return b.kind === "user" && (b.text ?? "").trim() === sending.text.trim();
    });
    if (echoed || sending.session !== active) setSending(null);
  }, [transcript.state, sending, active]);
  const shownState = useMemo(
    () =>
      pendingSend
        ? withSending(
            transcript.state,
            pendingSend.text,
            "sending:" + pendingSend.at,
          )
        : transcript.state,
    [transcript.state, pendingSend],
  );
  const starting =
    !!pendingSend && !["running", "waiting"].includes(current?.status ?? "");
  const interruptRef = useRef<() => void>(() => {});
  interruptRef.current = () => {
    if (current && running && !current.readOnly)
      void act("interrupt").catch(() => {});
  };
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (
        (e.ctrlKey || e.metaKey) &&
        !e.shiftKey &&
        e.key.toLowerCase() === "o"
      ) {
        e.preventDefault();
        setVerbose((v) => !v);
        return;
      }
      // Esc interrupts like the CLI, unless a dialog, menu or approval owns the key.
      if (e.key !== "Escape" || e.defaultPrevented || e.isComposing) return;
      const target = e.target as HTMLElement | null;
      if (
        document.querySelector("[role=dialog]") ||
        document.querySelector("details.conversation-menu[open]") ||
        target?.closest(".approval")
      )
        return;
      interruptRef.current();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);
  const openPanels = { left, right: panel };
  const shownWidths = clampWidths(widths, viewport, openPanels);
  const resizePanel = (side: Side, width: number, commit: boolean) => {
    const next = clampWidths(
      { ...widths, [side]: width },
      viewport,
      openPanels,
      side,
    );
    setWidths(next);
    if (commit) saveWidths(next);
  };
  const resizable = viewport > 850;
  if (!authReady || connectionError)
    return (
      <main className="pair-screen">
        <div className="pair-card">
          <p role={connectionError ? "alert" : "status"}>
            {connectionError || "Connecting to Workbench…"}
          </p>
          {connectionError && (
            <Button onClick={() => void bootstrap()}>Reconnect</Button>
          )}
        </div>
      </main>
    );
  if (!paired)
    return (
      <main className="pair-screen">
        <div className="pair-card">
          <div className="brand-mark">
            <Layers size={23} />
          </div>
          <p className="eyebrow">LOCAL AGENT CONTROL</p>
          <h1>
            Your agents,
            <br />
            one workbench.
          </h1>
          <p className="subtle">
            Enter the pairing code shown in Settings on the computer that runs
            the workbench (it is also in the pairing-code file in its data
            directory).
          </p>
          <form
            onSubmit={(e) => {
              e.preventDefault();
              setError("");
              api("login", "POST", { code })
                .then(() => {
                  setCode("");
                  setPaired(true);
                  void refresh().catch(catchError);
                })
                .catch(catchError);
            }}
          >
            <label htmlFor="pair-code">Pairing code</label>
            <Input
              id="pair-code"
              autoComplete="off"
              type="password"
              value={code}
              onChange={(e) => setCode(e.target.value)}
              required
            />
            <Button className="w-full mt-4" type="submit">
              Connect <ChevronRight size={16} />
            </Button>
          </form>
          {error && (
            <p role="alert" className="error-text">
              {error}
            </p>
          )}
          <div className="pair-foot">
            <span className="dot" />
            LOCAL FIRST · LINUX
          </div>
        </div>
      </main>
    );
  return (
    <>
      <div
        style={
          {
            height: "100dvh",
            "--sidebar-w": shownWidths.left + "px",
            "--panel-w": shownWidths.right + "px",
          } as React.CSSProperties
        }
        className={
          "workbench " + (!panel ? "no-panel " : "") + (!left ? "no-left" : "")
        }
      >
        {left && resizable && (
          <ResizeHandle
            side="left"
            width={shownWidths.left}
            onResize={(w) => resizePanel("left", w, false)}
            onCommit={(w) => resizePanel("left", w, true)}
          />
        )}
        {panel && resizable && (
          <ResizeHandle
            side="right"
            width={shownWidths.right}
            onResize={(w) => resizePanel("right", w, false)}
            onCommit={(w) => resizePanel("right", w, true)}
          />
        )}
        {left && (
          <Sidebar
            sessions={sessions}
            workspaces={workspaces}
            prefs={sidebarPrefs.prefs}
            activeId={active}
            onSelect={selectSession}
            onNew={openNewSession}
            onPrefs={sidebarPrefs.update}
            onRename={renameSession}
            onArchive={archiveSession}
            onDelete={setDeleting}
          />
        )}
        {/* Replies are styled like the agent's own terminal UI. */}
        <main className="conversation" data-agent={current?.agent}>
          <header className="conversation-heading">
            <Button
              variant="ghost"
              size="icon"
              aria-label="Toggle sessions"
              title={left ? "Hide sessions" : "Show sessions"}
              onClick={() => setLeft(!left)}
            >
              <PanelLeft size={17} />
              {!left && waitingElsewhere && (
                <span className="toggle-dot" aria-label="A session needs you" />
              )}
            </Button>
            <div className="conversation-title" title={current?.cwd}>
              <h2>
                {current && !composing
                  ? displayTitle(current, sidebarPrefs.prefs)
                  : "New session"}
              </h2>
              {current && !composing && (
                <>
                  <AgentIcon agent={current.agent} size={16} />
                  <span className={"status-pill " + current.status}>
                    {labels[current.status] ?? current.status}
                    {(current.background ?? 0) > 0 &&
                      ` · ${current.background} in background`}
                  </span>
                  {(current.related?.length ?? 0) > 0 && (
                    <span className="header-related" title="Also worked in">
                      {current.related!.map((r) => (
                        <span key={r} className="related-chip">
                          {r}
                        </span>
                      ))}
                    </span>
                  )}
                  <span className="conversation-path">
                    {current.cwd.split("/").filter(Boolean).length > 2
                      ? "…/" +
                        current.cwd
                          .split("/")
                          .filter(Boolean)
                          .slice(-2)
                          .join("/")
                      : current.cwd}
                  </span>
                </>
              )}
            </div>
            <details className="conversation-menu">
              <summary aria-label="More actions" title="More actions">
                <MoreHorizontal size={18} />
              </summary>
              <div className="conversation-menu-content">
                <Appearance />
                <NotifyToggle />
                <SuggestToggle />
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={(e) => {
                    e.currentTarget.closest("details")?.removeAttribute("open");
                    openSettings();
                  }}
                >
                  <Settings size={14} />
                  Settings
                </Button>
                {current && (
                  <>
                    <span className="menu-status">
                      {labels[current.status] ?? current.status}
                    </span>
                    {[
                      { label: "Resume engine", action: "resume", icon: Play },
                      {
                        label: "Interrupt turn",
                        action: "interrupt",
                        icon: Square,
                      },
                      { label: "Sleep", action: "sleep", icon: Moon },
                      { label: "End session", action: "stop", icon: X },
                    ].map(({ label, action, icon: Icon }) => (
                      <Button
                        key={action}
                        variant="ghost"
                        size="sm"
                        disabled={busy || current.readOnly || current.archived}
                        onClick={(e) => {
                          e.currentTarget
                            .closest("details")
                            ?.removeAttribute("open");
                          // Sleeping would stop the background work: ask.
                          if (
                            action === "sleep" &&
                            (current.background ?? 0) > 0
                          )
                            return setSleepAsk(true);
                          void act(action).catch(() => {});
                        }}
                      >
                        <Icon size={14} />
                        {label}
                      </Button>
                    ))}
                    <span className="menu-path">{current.cwd}</span>
                  </>
                )}
              </div>
            </details>
            <Button
              variant="ghost"
              size="icon"
              aria-label="Toggle preview"
              title={panel ? "Hide preview" : "Show preview"}
              onClick={() => setPanel(!panel)}
            >
              <PanelRightClose size={17} />
            </Button>
          </header>
          {outdated && (
            <div className="update-banner" role="status">
              Workbench was updated. Reload to use the new version.
              <Button size="sm" onClick={() => location.reload()}>
                Reload
              </Button>
            </div>
          )}
          {error && (
            <div className="error-banner" role="alert">
              {error}
              <button aria-label="Dismiss error" onClick={() => setError("")}>
                <X size={14} />
              </button>
            </div>
          )}
          {current && !composing ? (
            <>
              {transcript.loading || transcript.state.keys.length ? (
                <EnvContext.Provider value={env}>
                  <Timeline
                    key={current.id}
                    state={shownState}
                    verbose={verbose}
                    hasMore={transcript.hasMore}
                    loadingEarlier={transcript.loadingEarlier}
                    onLoadEarlier={transcript.loadEarlier}
                  />
                </EnvContext.Provider>
              ) : (
                <div className="empty-conversation">
                  <AgentIcon agent={current.agent} size={40} />
                  <h1>Start here.</h1>
                  <p>
                    Send a task to start{" "}
                    {current.agent === "claude" ? "Claude Code" : "Codex"}.
                    <br />
                    Context and settings are managed by the local CLI.
                  </p>
                </div>
              )}
              <EnvContext.Provider value={env}>
                <div className="approvals">
                  {[...approvals.values()].map((e) => (
                    <ApprovalCard
                      key={String(e.data.id)}
                      event={e}
                      agent={current.agent}
                      lookup={(id) => transcript.state.blocks.get(id)}
                      onInterrupt={() => void act("interrupt").catch(() => {})}
                      reply={async (
                        id,
                        decision: Decision,
                        message,
                        answers,
                        mode,
                      ) => {
                        await act("reply", {
                          id,
                          decision,
                          message,
                          answers,
                          mode,
                        });
                      }}
                    />
                  ))}
                </div>
              </EnvContext.Provider>
              <div className="composer-stack">
                <GoalBar
                  sessionId={current.id}
                  agent={current.agent}
                  goal={goal}
                  running={running}
                  readOnly={!!current.readOnly || !!current.archived}
                  onCommand={runCommand}
                  onSend={sendMessage}
                  onInterrupt={() => void act("interrupt").catch(() => {})}
                />
                <BackgroundTray
                  sessionId={current.id}
                  tasks={background}
                  readOnly={!!current.readOnly || !!current.archived}
                />
                <TodoPanel steps={plan} busy={running} />
                <QueueList
                  items={transcript.queue.items}
                  paused={transcript.queue.paused}
                  running={running}
                  note={transcript.queue.items.length ? steerNote : ""}
                  onRemove={(qid) =>
                    void api(`sessions/${active}/queue/${qid}`, "DELETE").catch(
                      catchError,
                    )
                  }
                  onResume={() => void act("queue-resume").catch(() => {})}
                  onSteer={(qid) =>
                    void api<SteerResult>(
                      `sessions/${active}/queue/${qid}/steer`,
                      "POST",
                    )
                      .then((res) =>
                        setSteerNote(res.queued ? (res.reason ?? "") : ""),
                      )
                      .catch(catchError)
                  }
                />
                <StatusLine
                  sessionId={current.id}
                  status={starting ? "starting" : current.status}
                  startedAt={
                    starting
                      ? pendingSend!.at
                      : turnClock(
                          transcript.turnStarted,
                          lastSent.current?.session === current.id
                            ? lastSent.current.at
                            : null,
                        )
                  }
                  verb={
                    starting
                      ? current.status === "idle"
                        ? "Sending"
                        : `Starting ${current.agent === "codex" ? "Codex" : "Claude"}`
                      : activity(transcript.state, current.status)
                  }
                  onBackground={moveToBackground}
                />
              </div>
              {parentSession && (
                <div className="archived-banner parent-banner" role="note">
                  <CornerLeftUp size={14} />
                  <span>
                    Subagent of{" "}
                    <strong>
                      {displayTitle(parentSession, sidebarPrefs.prefs)}
                    </strong>
                  </span>
                  <Button
                    size="sm"
                    variant="ghost"
                    onClick={() => selectSession(parentSession.id)}
                  >
                    Open parent
                  </Button>
                </div>
              )}
              {current.archived && (
                <div className="archived-banner" role="status">
                  <Archive size={14} />
                  <span>Archived in Codex. Unarchive it to continue.</span>
                  <Button
                    size="sm"
                    variant="ghost"
                    onClick={() => archiveSession(current, false)}
                  >
                    Unarchive
                  </Button>
                </div>
              )}
              <Composer
                key={current.id}
                session={current}
                running={running}
                disabled={busy}
                goal={goal}
                inputRef={composerRef}
                onSend={sendMessage}
                onCommand={runCommand}
                onAction={(action) => {
                  if (action === "new") openNewSession(current.cwd);
                }}
                onInterrupt={() => void act("interrupt").catch(() => {})}
                onBackground={moveToBackground}
              />
            </>
          ) : (
            <NewSession
              key={composing?.cwd ?? "new"}
              sessions={sessions}
              workspaces={workspaces}
              prefs={sidebarPrefs.prefs}
              initialCwd={composing?.cwd}
              onCreated={(s) => {
                setSessions((old) => [s, ...old.filter((x) => x.id !== s.id)]);
                setComposing(null);
                setActive(s.id);
                void refresh().catch(catchError);
              }}
            />
          )}
        </main>
        {panel && (
          <aside className="artifacts">
            <header className="artifact-header">
              <div>
                <span className="eyebrow">ARTIFACTS</span>
                <h3>Outputs</h3>
              </div>
              <Button
                variant="ghost"
                size="icon"
                aria-label="New web preview"
                disabled={!current}
                onClick={() => setServiceOpen(true)}
              >
                <Globe size={18} />
              </Button>
            </header>
            {preview ? (
              <>
                <div className="preview-toolbar">
                  <span>
                    {preview.owned ? "Managed service" : "Preview"} ·{" "}
                    {preview.entry}
                  </span>
                  {preview.fileUrl &&
                    previewMode(preview, location.host) === "file" && (
                      <a
                        className="preview-newtab"
                        href={preview.fileUrl}
                        target="_blank"
                        rel="noopener noreferrer"
                        aria-label="Open in a new tab"
                        title="Open in a new tab"
                      >
                        <ExternalLink size={14} />
                      </a>
                    )}
                  <Button
                    variant="ghost"
                    size="icon"
                    aria-label="Close preview"
                    onClick={() =>
                      api("previews/" + preview.id, "DELETE")
                        .then(() => {
                          setPreview(null);
                          void refresh();
                        })
                        .catch(catchError)
                    }
                  >
                    <X size={14} />
                  </Button>
                </div>
                {preview.fileUrl &&
                previewMode(preview, location.host) === "file" ? (
                  // Sandboxed on the workbench's address: no same-origin.
                  <iframe
                    key={preview.fileUrl}
                    title="Resource preview"
                    className="preview-frame"
                    src={preview.fileUrl}
                    sandbox={fileFrameSandbox(preview.entry)}
                    referrerPolicy="no-referrer"
                  />
                ) : (
                  <iframe
                    title="Resource preview"
                    name="artifact-preview"
                    className="preview-frame"
                    sandbox="allow-scripts allow-same-origin allow-forms allow-downloads"
                    referrerPolicy="strict-origin"
                  />
                )}
              </>
            ) : (
              <>
                <div className="file-path">
                  <button
                    onClick={() =>
                      setPath(path.split("/").slice(0, -1).join("/"))
                    }
                  >
                    ↑
                  </button>
                  <span>{path || "Working directory"}</span>
                  <button
                    aria-label="Refresh files"
                    onClick={() =>
                      api<Entry[]>(
                        "files?session=" +
                          active +
                          "&path=" +
                          encodeURIComponent(path),
                      )
                        .then(setEntries)
                        .catch(catchError)
                    }
                  >
                    <RefreshCw size={12} />
                  </button>
                </div>
                <div className="files">
                  {entries.map((e) => (
                    <button
                      key={e.path}
                      onClick={() => {
                        if (e.directory) return setPath(e.path);
                        // Images and videos of this folder form one gallery.
                        const media = entries.filter(
                          (x) => !x.directory && isMedia(x.path),
                        );
                        const at = media.findIndex((x) => x.path === e.path);
                        if (at >= 0)
                          return env.view(
                            media.map((x) => pathItem(env, x.path)!),
                            at,
                          );
                        void openPreview({
                          session: active,
                          kind: "file",
                          path: e.path,
                        });
                      }}
                    >
                      {e.directory ? <Folder size={15} /> : <File size={15} />}
                      <span>{e.name}</span>
                      {!e.directory && (
                        <small>
                          {e.size > 1048576
                            ? (e.size / 1048576).toFixed(1) + " MB"
                            : Math.ceil(e.size / 1024) + " KB"}
                        </small>
                      )}
                    </button>
                  ))}
                  {!entries.length && (
                    <div className="artifact-empty">
                      <Folder size={28} />
                      <p>
                        {current
                          ? "No files in this directory"
                          : "Select a session to browse outputs"}
                      </p>
                    </div>
                  )}
                </div>
                <div className="preview-services">
                  {previews
                    .filter((p) => p.session === active)
                    .map((p) => (
                      <div className="flex items-center gap-2" key={p.id}>
                        <button
                          className="truncate flex-1 text-left"
                          onClick={() =>
                            api<{ preview: Preview; grant: string }>(
                              "previews/" + p.id + "/authorize",
                              "POST",
                              {},
                            )
                              .then((x) => authorize(x.preview, x.grant))
                              .catch(catchError)
                          }
                        >
                          {p.owned ? "●" : "○"}{" "}
                          {p.entry === "/" ? "Web service" : p.entry}
                        </button>
                        <Button
                          variant="ghost"
                          size="icon"
                          aria-label="Stop preview service"
                          onClick={() =>
                            api("previews/" + p.id, "DELETE")
                              .then(refresh)
                              .catch(catchError)
                          }
                        >
                          <X size={13} />
                        </Button>
                      </div>
                    ))}
                </div>
                <div className="artifact-foot">
                  Images / Video / HTML / Dev servers
                  <br />
                  <span>Loaded on demand. Original files stay intact.</span>
                </div>
              </>
            )}
          </aside>
        )}
        <SettingsDialog open={settingsOpen} onOpenChange={setSettingsOpen} />
        {viewer && (
          <MediaViewer
            items={viewer.items}
            index={viewer.index}
            onClose={() => setViewer(null)}
            onReveal={(item) => item.path && env.reveal(item.path, item.base)}
            onPanel={(item) => {
              if (!item.path) return;
              setViewer(null);
              setPanel(true);
              env.openPath(item.path, item.base);
            }}
          />
        )}
        {sleepAsk && current && (
          <Dialog open onOpenChange={(open) => !open && setSleepAsk(false)}>
            <DialogContent className="modal modal-content confirm-dialog">
              <DialogTitle asChild>
                <h2>Stop the background work?</h2>
              </DialogTitle>
              <p>
                {(current.background ?? 0) === 1
                  ? "A task runs"
                  : `${current.background} tasks run`}{" "}
                in the background of{" "}
                <strong>{displayTitle(current, sidebarPrefs.prefs)}</strong>.
                Sleeping stops {(current.background ?? 0) === 1 ? "it" : "them"}
                ; the session resumes with your next message.
              </p>
              <div className="confirm-actions">
                <Button variant="ghost" onClick={() => setSleepAsk(false)}>
                  Keep running
                </Button>
                <Button
                  className="danger-button"
                  onClick={() => {
                    setSleepAsk(false);
                    void act("sleep", { force: true }).catch(() => {});
                  }}
                >
                  <Moon size={14} />
                  Stop and sleep
                </Button>
              </div>
            </DialogContent>
          </Dialog>
        )}
        {deleting && (
          <Dialog
            open
            onOpenChange={(open) => {
              if (!open && !deleteBusy) setDeleting(null);
            }}
          >
            <DialogContent className="modal modal-content confirm-dialog">
              <DialogTitle asChild>
                <h2>Delete session?</h2>
              </DialogTitle>
              <p>
                <strong>{displayTitle(deleting, sidebarPrefs.prefs)}</strong>{" "}
                will be deleted from Codex, including the Codex app and CLI.
                This cannot be undone.
              </p>
              {!deleting.archived && (
                <p className="subtle">
                  To keep it out of the way instead, archive it.
                </p>
              )}
              <div className="confirm-actions">
                <Button
                  variant="ghost"
                  disabled={deleteBusy}
                  onClick={() => setDeleting(null)}
                >
                  Cancel
                </Button>
                {!deleting.archived && (
                  <Button
                    variant="ghost"
                    disabled={deleteBusy}
                    onClick={() => {
                      const s = deleting;
                      setDeleting(null);
                      archiveSession(s, true);
                    }}
                  >
                    <Archive size={14} />
                    Archive
                  </Button>
                )}
                <Button
                  className="danger-button"
                  disabled={deleteBusy}
                  onClick={() => void deleteSession(deleting)}
                >
                  <Trash2 size={14} />
                  {deleteBusy ? "Deleting…" : "Delete"}
                </Button>
              </div>
            </DialogContent>
          </Dialog>
        )}
        {serviceOpen && (
          <Dialog
            open
            onOpenChange={(open) => {
              if (!open) setServiceOpen(false);
            }}
          >
            <DialogContent>
              <DialogTitle className="sr-only">New web preview</DialogTitle>
              <button
                className="modal-close"
                aria-label="Close dialog"
                onClick={() => setServiceOpen(false)}
              >
                <X size={18} />
              </button>
              <p className="eyebrow">LIVE PREVIEW</p>
              <h2>Connect a web preview</h2>
              <form
                onSubmit={(e) => {
                  e.preventDefault();
                  void openPreview({
                    session: active,
                    kind: "service",
                    target,
                    command,
                    port: Number(port),
                  });
                }}
              >
                <label htmlFor="target">Existing server URL</label>
                <Input
                  id="target"
                  value={target}
                  onChange={(e) => setTarget(e.target.value)}
                  placeholder="http://127.0.0.1:5173"
                />
                <div className="divider">Or start from Workbench</div>
                <label htmlFor="command">Start command</label>
                <Input
                  id="command"
                  value={command}
                  onChange={(e) => setCommand(e.target.value)}
                  placeholder="npm run dev -- --host 127.0.0.1 --port 5173 --strictPort"
                />
                <label htmlFor="port">Server port</label>
                <Input
                  id="port"
                  type="number"
                  min="1024"
                  max="65535"
                  value={port}
                  onChange={(e) => setPort(e.target.value)}
                />
                <p className="subtle text-xs">
                  Commands run in this working directory. Managed servers stop
                  with the session; existing servers are only disconnected.
                </p>
                <Button className="w-full" type="submit">
                  Open preview <Globe size={15} />
                </Button>
              </form>
              {error && (
                <p role="alert" className="error-text">
                  {error}
                </p>
              )}
            </DialogContent>
          </Dialog>
        )}
      </div>
    </>
  );
}
