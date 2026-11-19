import { SidePanel, type SideChat } from "./side/SidePanel";
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
import { Composer, type Attached } from "./composer/Composer";

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
import { api, download } from "./lib/api";
import { randomUUID } from "./lib/uuid";
import {
  afterError,
  footerConnection,
  reconnectDelay,
  shouldRenew,
  type Live,
} from "./lib/connection";
import { Appearance } from "./components/Appearance";
import { NotifyToggle } from "./components/NotifyToggle";
import { SuggestToggle } from "./components/SuggestToggle";
import { notifyChange, notifyRun, useAttention } from "./attention";
import { newDraft, type Draft, type RoutineRun } from "./routines/when";
import { recentProjects } from "./newsession/recent";
import {
  assistantOf,
  assistantsChanged,
  changesChanged,
  ASSISTANT_SETTINGS_EVENT,
  latestFor,
  MAIN_STARTERS,
  newRoutine,
  NEW_ROUTINE_EVENT,
  OPEN_SESSION_EVENT,
  SET_UP_EVENT,
  useAssistants,
  useChanges,
  type Assistant,
  type AssistantChange,
  type Opened,
} from "./lib/assistants";
import {
  AssistantHome,
  AssistantSettings,
  ChangeBanner,
  RoutineDialog,
  SetUpDialog,
} from "./assistants/lazy";
import {
  Dialog as AboutDialog,
  DialogContent as AboutContent,
  DialogTitle as AboutTitle,
} from "./components/ui/dialog";
import { SettingsDialog } from "./components/settings/SettingsDialog";
import { LogoMark } from "./components/Logo";
import { PreviewPanel } from "./preview/PreviewPanel";
import { FileList, type Entry } from "./preview/FileList";
import { mayLeave } from "./preview/unsaved";
import { inTerminal } from "./preview/terminal";
import {
  drifted,
  mergeSessions,
  type SessionsDelta,
} from "./lib/sessionsDelta";
import { tabFromPreview, type Preview } from "./preview/tabs";
import { usePreviewTabs } from "./preview/usePreviewTabs";
import { useEffect, useMemo, useRef, useState } from "react";
import { Timeline } from "./transcript/Timeline";
import { IMPLEMENT_PLAN, freshPlanMessage } from "./transcript/plans";
import type { Options } from "./agentOptions";
import { useTranscript, type ExternalUpdate } from "./transcript/useTranscript";
import { RecordMissing } from "./transcript/RecordMissing";
import {
  EnvContext,
  isMedia,
  pathItem,
  type Env,
  type LineAt,
  type ViewItem,
} from "./transcript/media";
import { MediaViewer } from "./media/MediaViewer";
import { withSending } from "./transcript/reducer";
import { splitFiles } from "./transcript/files";
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
  X,
  Globe,
  PanelRightClose,
  PanelLeft,
  MoreHorizontal,
  ChevronRight,
  RefreshCw,
  Settings,
  Archive,
  Trash2,
  ExternalLink,
  CornerLeftUp,
  MessagesSquare,
  AlarmClock,
  MessageSquarePlus,
  BookOpen,
  Info,
  ListChecks,
} from "lucide-react";
import { Button } from "./components/ui/button";
import { Input } from "./components/ui/input";

import { Dialog, DialogContent, DialogTitle } from "./components/ui/dialog";

type Event = WorkbenchEvent;
export default function App() {
  const [workspaces, setWorkspaces] = useState<string[]>([]);
  const [left, setLeft] = useState(
    () =>
      localStorage.getItem("workbench-left") !== "false" && innerWidth > 850,
  );
  const [connectionError, setConnectionError] = useState("");
  const [authReady, setAuthReady] = useState(false);
  const [settingsOpen, setSettingsOpen] = useState(false);
  // The routine being made or changed (routines/RoutineDialog.tsx).
  const [routineDraft, setRoutineDraft] = useState<Draft | null>(null);
  // When this page last sent a message: the turn clock starts there, not
  // when the engine has finished starting.
  const lastSent = useRef<{ session: string; at: number } | null>(null);
  // A message sent from this page, shown until the server echoes it.
  const [sending, setSending] = useState<{
    session: string;
    text: string;
    images: string[];
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
  // Whether this page runs on the workbench computer: there "Show in Files"
  // opens its file manager; anywhere else paths download instead.
  const [here, setHere] = useState(true);
  // How this page signs in (for the sidebar's connection line), and whether
  // the live update stream is open; retry makes it again once closed.
  const [signin, setSignin] = useState<{ open: boolean; method?: string }>({
    open: true,
  });
  const [live, setLive] = useState<Live>("connecting");
  const [retry, setRetry] = useState(0);
  // When the stream last said anything (ADR 0001: a stalled one is made
  // again), and how many times in a row the browser gave up on it.
  const lastHeard = useRef(Date.now());
  const gaveUp = useRef(0);
  const liveRef = useRef(live);
  liveRef.current = live;
  // A new stream now, from where this page is.
  const renewStream = () => {
    setLive((was) => (was === "open" ? "reconnecting" : was));
    setRetry((n) => n + 1);
  };
  const [paired, setPaired] = useState(false),
    [code, setCode] = useState(""),
    [error, setError] = useState(""),
    [sessions, setSessions] = useState<Session[]>([]),
    [active, setActive] = useState(""),
    [busy, setBusy] = useState(false),
    // A new session, or a new message for an assistant (lib/assistants.ts).
    [composing, setComposing] = useState<{
      cwd?: string;
      assistant?: Assistant;
      continues?: string;
    } | null>(null),
    [entries, setEntries] = useState<Entry[]>([]),
    [path, setPath] = useState(""),
    // The file list entry a link pointed at, relative to the session's
    // directory; cleared when the list moves elsewhere.
    [located, setLocated] = useState<string>(),
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
        void refresh(false).catch(catchError);
    }, 10000);
    return () => clearInterval(timer);
  }, [paired]);
  const selected = useRef(active);
  selected.current = active;
  const streamCursor = useRef(0);
  // The session list this page has, as the workbench names it: polls get
  // only what changed since (sessionlist.go).
  const listToken = useRef("");
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
  // Per session: whether its side chat panel is shown (hidden keeps it
  // going), the side chat a command asked to show, and the live one's status.
  const [sideOpen, setSideOpen] = useState<Record<string, boolean>>({});
  const [sideFocus, setSideFocus] = useState<Record<string, string>>({});
  const [sideLive, setSideLive] = useState<Record<string, string | null>>({});
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
  // The session's preview tabs, and the preview over the conversation.
  const previewTabs = usePreviewTabs(
    paired && active ? active : undefined,
    catchError,
  );
  const [expanded, setExpanded] = useState(false);
  // Actions under way, so each shows it was taken: "files", "open",
  // "service", "stop:<id>".
  const [working, setWorking] = useState<ReadonlySet<string>>(new Set());
  const busyWhile = async <T,>(key: string, run: () => Promise<T>) => {
    setWorking((w) => new Set(w).add(key));
    try {
      return await run();
    } finally {
      setWorking((w) => {
        const next = new Set(w);
        next.delete(key);
        return next;
      });
    }
  };
  const sidebarPrefs = usePrefs(paired, catchError);
  const receivePrefs = useRef(sidebarPrefs.receive);
  // Opens a session from a routine's notification (set below).
  const openSession = useRef<(id: string) => void>(() => {});
  receivePrefs.current = sidebarPrefs.receive;
  // What changed in the session list since this page's copy; all of it the
  // first time, or when the copy drifted.
  const loadSessions = async () => {
    const d = await api<SessionsDelta>(
      "sessions?since=" + encodeURIComponent(listToken.current),
    );
    listToken.current = d.token;
    setSessions((old) => {
      const merged = mergeSessions(old, d);
      if (drifted(merged, d)) listToken.current = "";
      return merged;
    });
    return d;
  };
  // Everything the page lists. The 10-second poll (all false) reads the
  // folders and previews only when the session list changed; after an
  // action, all of it.
  const refresh = async (all = true) => {
    const d = await loadSessions();
    if (!all && !d.full && !d.changed?.length && !d.removed?.length) return;
    setWorkspaces(await api<string[]>("workspaces"));
    setPreviews(await api<Preview[]>("previews"));
  };
  useEffect(() => {
    const requirePairing = () => {
      setAuthReady(true);
      setSettingsOpen(false);
      setPaired(false);
      setSessions([]);
      listToken.current = "";
      setActive("");
      setEntries([]);
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
      const status = await api<{
        authenticated: boolean;
        thisComputer?: boolean;
        local?: boolean;
        method?: string;
      }>("auth/status");
      setHere(status.thisComputer !== false);
      setSignin({ open: !!status.local, method: status.method });
      if (status.authenticated) {
        listToken.current = "";
        const list = (await loadSessions()).changed ?? [];
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
  // Settings, on a given tab when one is asked for.
  const [settingsTab, setSettingsTab] = useState<"routines" | undefined>();
  const openSettings = (tab?: "routines") => {
    setSettingsTab(tab);
    setSettingsOpen(true);
  };
  useEffect(() => {
    if (!paired) return;
    const source = new EventSource(
      "/api/events?after=" +
        streamCursor.current +
        "&session=" +
        encodeURIComponent(active),
    );
    // Anything from the workbench shows the stream is alive.
    const heard = () => {
      lastHeard.current = Date.now();
    };
    heard();
    for (const name of ["message", "telemetry", "cursor", "hello", "external"])
      source.addEventListener(name, heard);
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
    // The stream passed over other sessions' events: where to start again.
    source.addEventListener("cursor", (e) => {
      const seq = Number((e as MessageEvent).data);
      if (seq > streamCursor.current) streamCursor.current = seq;
    });
    // Each connection announces the server's web build; a restart onto a new
    // build shows up as a different one.
    source.addEventListener("hello", (e) => {
      const { build, after } = JSON.parse((e as MessageEvent).data) as {
        build?: string;
        after?: number;
      };
      // Where the stream starts: a renewed one resumes there, even if no
      // event came before it (ADR 0001).
      if (after && after > streamCursor.current) streamCursor.current = after;
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
      if (v.kind === "assistants") {
        assistantsChanged();
        return;
      }
      if (v.kind === "assistant") {
        // A background run started or ended (assistants/Changes.tsx).
        changesChanged();
        notifyChange(v.data as AssistantChange, (id) =>
          openSession.current(id),
        );
        return;
      }
      if (v.kind === "routine") {
        // Settings → Routines reloads; a run's end may notify.
        window.dispatchEvent(new CustomEvent("workbench:routines"));
        const run = (v.data as { run?: RoutineRun }).run;
        if (run) notifyRun(run, (id) => openSession.current(id));
        return;
      }
      if (v.kind === "prefs") receivePrefs.current(v.data);
      else if (v.session === selected.current) pushEvent.current(v);
    };
    source.addEventListener("ping", heard);
    source.onopen = () => {
      heard();
      gaveUp.current = 0;
      setLive("open");
    };
    let again = 0;
    source.onerror = () => {
      setLive(afterError);
      // EventSource hides HTTP status; probe authentication on connection
      // failure (a small poll of the list).
      void loadSessions().catch(() => {});
      // The browser gave up (an HTTP error, say while the workbench
      // restarts behind a proxy): never leave it so. Again, later each time.
      if (source.readyState === EventSource.CLOSED && !again)
        again = window.setTimeout(
          () => setRetry((n) => n + 1),
          reconnectDelay(gaveUp.current++),
        );
    };
    return () => {
      source.close();
      clearTimeout(again);
    };
  }, [paired, active, retry]);
  // A stream that went quiet (a dead connection the browser does not
  // notice) is made again; so is one found quiet or down when the page
  // wakes: shown again, back online, or restored from the back-forward cache.
  useEffect(() => {
    if (!paired) return;
    const check = (waking: boolean) => {
      if (
        shouldRenew({
          now: Date.now(),
          last: lastHeard.current,
          live: liveRef.current,
          waking,
        })
      )
        renewStream();
    };
    const timer = window.setInterval(() => check(false), 5000);
    const wake = () => {
      if (document.visibilityState === "visible") check(true);
    };
    document.addEventListener("visibilitychange", wake);
    window.addEventListener("online", wake);
    window.addEventListener("pageshow", wake);
    return () => {
      clearInterval(timer);
      document.removeEventListener("visibilitychange", wake);
      window.removeEventListener("online", wake);
      window.removeEventListener("pageshow", wake);
    };
  }, [paired]);
  // Live updates down for a while: said at the top of the conversation too
  // (a phone does not show the sidebar's footer).
  const [liveDown, setLiveDown] = useState(false);
  useEffect(() => {
    if (live === "open") {
      setLiveDown(false);
      return;
    }
    const t = window.setTimeout(() => setLiveDown(true), 5000);
    return () => clearTimeout(t);
  }, [live]);
  useEffect(() => {
    if (!paired || !active) return;
    setPath("");
    setLocated(undefined);
    setExpanded(false);
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
    ) {
      setFilesVersion((v) => v + 1);
      // A file a tab shows may have changed.
      previewTabs.refresh();
    }
    lastStatus.current = status;
  }, [status]);
  useEffect(() => {
    if (!paired || !active) return;
    let cancelled = false;
    listFiles(path)
      .then((v) => {
        if (!cancelled) setEntries(v);
      })
      .catch(catchError);
    return () => {
      cancelled = true;
    };
  }, [active, path, paired, filesVersion, located]);
  const act = async (action: string, body?: unknown) => {
    setBusy(true);
    setError("");
    try {
      if (["stop", "sleep", "archive", "delete"].includes(action)) {
        const sides = await api<SideChat[]>(
          `sessions/${encodeURIComponent(active)}/side-chats`,
        );
        const live = sides.filter(
          (x) => !["closed", "interrupted"].includes(x.status),
        );
        if (live.length) {
          if (
            !window.confirm(
              "This will close the active side chat and release its resources. Its transcript will remain read-only. Continue?",
            )
          )
            return;
          for (const x of live) {
            await api(`side-chats/${encodeURIComponent(x.id)}/close`, "POST", {
              confirmed: true,
            });
            const deadline = Date.now() + 35000;
            for (;;) {
              const sc = await api<SideChat>(
                `side-chats/${encodeURIComponent(x.id)}`,
              );
              if (["closed", "interrupted"].includes(sc.status)) break;
              if (sc.status === "cleanup_failed" || Date.now() > deadline)
                throw Error(sc.error || "Side chat cleanup did not finish");
              await new Promise((resolve) => setTimeout(resolve, 200));
            }
          }
        }
      }
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
    // A file being edited in the preview panel goes with the session.
    if (id !== selected.current && !mayLeave()) return;
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
  openSession.current = selectSession;
  const recent = useMemo(
    () => recentProjects(sessions, sidebarPrefs.prefs, workspaces),
    [sessions, sidebarPrefs.prefs, workspaces],
  );
  const sessionTitle = (id: string) => {
    const s = sessions.find((x) => x.id === id);
    return s ? displayTitle(s, sidebarPrefs.prefs) : "a session that is gone";
  };
  const scheduleFor = (s: Session) => setRoutineDraft(newDraft(s.id, s.cwd));
  const assistantList = useAssistants(paired);
  // Assistants used before they are set up (lib/assistants.ts).
  const [settingUp, setSettingUp] = useState(false);
  // One assistant's settings, and the open conversation's assistant card.
  const [settingsFor, setSettingsFor] = useState<string | null>(null);
  const [aboutOpen, setAboutOpen] = useState(false);
  useEffect(() => {
    const show = ((e: CustomEvent<string>) =>
      setSettingsFor(e.detail)) as EventListener;
    window.addEventListener(ASSISTANT_SETTINGS_EVENT, show);
    return () => window.removeEventListener(ASSISTANT_SETTINGS_EVENT, show);
  }, []);
  useEffect(() => {
    const ask = () => setSettingUp(true);
    window.addEventListener(SET_UP_EVENT, ask);
    return () => window.removeEventListener(SET_UP_EVENT, ask);
  }, []);
  // Settings and quick starts open a session or the routine dialog.
  useEffect(() => {
    // (Event here is the workbench's own type; this one is the DOM's.)
    const show = ((e: CustomEvent<string>) => {
      setSettingsOpen(false);
      setComposing(null);
      selectSession(e.detail);
    }) as EventListener;
    // Aimed at the assistant named (Moss by default), when it is there.
    const routine = ((e: CustomEvent<string>) => {
      const dir = e.detail || "main";
      const d = newDraft(undefined, recent[0]?.path ?? "");
      if (
        assistantList.assistants.some(
          (a) => a.dir === dir && !a.error && a.enabled !== false,
        )
      ) {
        d.target = "assistant";
        d.assistant = dir;
      }
      setRoutineDraft(d);
    }) as EventListener;
    window.addEventListener(OPEN_SESSION_EVENT, show);
    window.addEventListener(NEW_ROUTINE_EVENT, routine);
    return () => {
      window.removeEventListener(OPEN_SESSION_EVENT, show);
      window.removeEventListener(NEW_ROUTINE_EVENT, routine);
    };
  });
  // An assistant opens on its ongoing topic, or on a new message for it.
  const openAssistant = (dir: string) =>
    void api<Opened>(`assistants/${dir}/open`, "POST")
      .then((o) => {
        if (o.session) {
          setComposing(null);
          selectSession(o.session);
        } else {
          const prev = o.previous && sessions.find((x) => x.id === o.previous);
          setComposing({
            assistant: o.assistant,
            continues: prev
              ? displayTitle(prev, sidebarPrefs.prefs)
              : undefined,
          });
        }
        if (innerWidth <= 850) setLeft(false);
      })
      .catch(catchError);
  // The continuous assistant whose ongoing topic is the open session.
  const topicOf = current
    ? assistantOf(assistantList.assistants, current.cwd)
    : undefined;
  const ongoing =
    topicOf?.sessions === "continuous" && topicOf.current === current?.id
      ? topicOf
      : undefined;
  // A continuous assistant's conversation that is getting long: offer a
  // fresh one (a new topic; ADR 0010), which also happens on its own each
  // morning and at 60% of the context.
  const startFresh = (dir: string) =>
    void api(`assistants/${dir}/topic`, "POST")
      .then(() => openAssistant(dir))
      .catch(catchError);
  const [topicFull, setTopicFull] = useState(0);
  useEffect(() => {
    setTopicFull(0);
    if (!ongoing || !current || current.status === "running") return;
    let live = true;
    api<{ context?: { used?: number; window?: number } }>(
      `sessions/${encodeURIComponent(current.id)}/telemetry`,
    )
      .then((t) => {
        if (live && t.context?.window)
          setTopicFull((t.context.used ?? 0) / t.context.window);
      })
      .catch(() => {});
    return () => {
      live = false;
    };
  }, [ongoing?.dir, current?.id, current?.status]);
  // What the open conversation last added to the wiki.
  const changes = useChanges(topicOf?.memory === "wiki");
  const memory = latestFor(changes, current?.id);
  // The answer to "Update memory now", for a few seconds.
  const [memoryNote, setMemoryNote] = useState("");
  useEffect(() => {
    if (!memoryNote) return;
    const t = setTimeout(() => setMemoryNote(""), 6000);
    return () => clearTimeout(t);
  }, [memoryNote]);
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
      .then(() => refresh())
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
  // A folder of the session's directory; the entry a link pointed at is
  // listed even when the list would hide it.
  const listFiles = (dir: string) => {
    const name =
      located && located.split("/").slice(0, -1).join("/") === dir
        ? located.split("/").pop()
        : undefined;
    return api<Entry[]>(
      "files?session=" +
        active +
        "&path=" +
        encodeURIComponent(dir) +
        (name ? "&show=" + encodeURIComponent(name) : ""),
    );
  };
  const goTo = (dir: string) => {
    setLocated(undefined);
    setPath(dir);
  };
  // Shows a path a session wrote in the preview panel's file list.
  const locate = (p: string, base?: string) =>
    void api<{ path: string; directory: boolean }>(
      `resolve?session=${encodeURIComponent(active)}&path=${encodeURIComponent(p)}` +
        (base ? `&base=${encodeURIComponent(base)}` : ""),
    )
      .then((r) => {
        if (previewTabs.state.active !== -1 && !mayLeave()) return;
        setPanel(true);
        previewTabs.show(-1);
        setPath(r.path.split("/").slice(0, -1).join("/"));
        setLocated(r.path);
      })
      .catch(catchError);
  const locateRef = useRef(locate);
  locateRef.current = locate;
  // Opens a file or service as the session's newest preview tab; the
  // panel shows at once that it is opening.
  // at: the lines a file link pointed to.
  const openPreview = (body: unknown, key = "open", at?: LineAt) =>
    busyWhile(key, async () => {
      if (key === "open") setPanel(true);
      try {
        const result = await api<{ preview: Preview; grant: string }>(
          "previews",
          "POST",
          body,
        );
        const tab = tabFromPreview(result.preview);
        previewTabs.open(
          at && tab.kind === "file"
            ? { ...tab, line: at.line, lineEnd: at.end }
            : tab,
        );
        setPanel(true);
        setServiceOpen(false);
        await refresh();
      } catch (e) {
        catchError(e);
      }
    });
  const approvals = transcript.approvals;
  // Sends from the composer, or answers an agent's questions: into the
  // running turn (steer), as the next message, or queued for after the turn.
  const sendMessage = async (
    text: string,
    how: "send" | "steer" | "queue",
    attached: Attached = { images: [], files: [] },
  ) => {
    if (!current) return;
    const images = attached.images.map((i) => i.id);
    const files = attached.files;
    if (how === "steer") {
      // Into the running turn; the server shows it as steered,
      // or queues it (and says why) when the turn cannot take it.
      setError("");
      try {
        const res = await api<SteerResult>(
          "sessions/" + current.id + "/send",
          "POST",
          { text, steer: true, images, files },
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
        images: attached.images.map((i) => i.url),
        at: Date.now(),
        from: transcript.state.keys.length,
      });
    try {
      await act("send", { text, queue, images, files });
    } catch (e) {
      // Not sent: its text goes back to the composer.
      setSending(null);
      throw e;
    }
    // Sent: it stays on screen until its echo arrives (ADR 0001).
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
        sideChat?: string;
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
      if (res.sideChat) {
        setSideFocus((xs) => ({ ...xs, [current.id]: res.sideChat! }));
        setSideOpen((xs) => ({ ...xs, [current.id]: true }));
      }
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
        requestID: randomUUID(),
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
  // Opens images and videos in the viewer.
  const view = (items: ViewItem[], index: number) =>
    void (items.length && setViewer({ items, index }));
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
      openPath: (path, base, at) =>
        void openPreview(
          { session: active, kind: "file", path, base },
          "open",
          at,
        ),
      thumb: (path, base, stamp) =>
        `/api/media?session=${encodeURIComponent(active)}&path=${encodeURIComponent(path)}` +
        (base ? `&base=${encodeURIComponent(base)}` : "") +
        (stamp ? `&v=${encodeURIComponent(stamp)}` : ""),
      here,
      reveal: (path, base) =>
        void (
          here
            ? api(`sessions/${encodeURIComponent(active)}/reveal`, "POST", {
                path,
                base,
              })
            : download(active, path, base)
        ).catch(catchError),
      locate: (path, base) => locateRef.current(path, base),
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
      here,
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
      return (
        b.kind === "user" &&
        splitFiles(b.text ?? "").text.trim() === sending.text.trim() &&
        (b.media?.length ?? 0) === sending.images.length
      );
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
            pendingSend.images,
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
      // A terminal's keys are the shell's (Esc, Ctrl+O and the rest).
      if (inTerminal(e.target)) return;
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
  // A preview gets the right panel's other width: half the window until
  // dragged.
  const previewing = panel && previewTabs.state.active >= 0;
  const sized = previewing
    ? { ...widths, right: widths.preview || Math.round(viewport / 2) }
    : widths;
  const shownWidths = clampWidths(sized, viewport, openPanels);
  const resizePanel = (side: Side, width: number, commit: boolean) => {
    const next = clampWidths(
      { ...sized, [side]: width },
      viewport,
      openPanels,
      side,
    );
    const kept = previewing
      ? { ...widths, left: next.left, preview: next.right }
      : { ...widths, ...next };
    setWidths(kept);
    if (commit) saveWidths(kept);
  };
  const expandedView = expanded && previewing;
  useEffect(() => {
    if (!previewing) setExpanded(false);
  }, [previewing]);
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
            <LogoMark scale={3} />
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
          "workbench " +
          (!panel ? "no-panel " : "") +
          (!left ? "no-left " : "") +
          (expandedView ? "preview-expanded" : "")
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
        {panel && resizable && !expandedView && (
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
            onSchedule={scheduleFor}
            assistants={assistantList.assistants}
            installedAgents={assistantList.installed}
            openAssistantDir={composing?.assistant?.dir ?? topicOf?.dir ?? ""}
            assistantRoot={assistantList.root}
            assistantsReady={!!assistantList.loaded}
            onOpenAssistant={openAssistant}
            connection={footerConnection({
              here,
              open: signin.open,
              method: signin.method,
              protocol: location.protocol,
              live,
            })}
            onReconnect={renewStream}
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
                  : composing?.assistant
                    ? composing.assistant.name
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
            {topicOf && current && !composing && (
              <Button
                variant="ghost"
                size="icon"
                aria-label={`About ${topicOf.name}`}
                title={`About ${topicOf.name}`}
                onClick={() => setAboutOpen(true)}
              >
                <Info size={17} />
              </Button>
            )}
            <details className="conversation-menu">
              <summary aria-label="More actions" title="More actions">
                <MoreHorizontal size={18} />
              </summary>
              <div className="conversation-menu-content">
                {/* What applies to the open conversation, then the
                    workbench's own settings (grouped). */}
                {current && !composing && (
                  <div
                    className="menu-group"
                    role="group"
                    aria-label="This conversation"
                  >
                    <div className="menu-group-title">
                      <span>This conversation</span>
                      <span className="menu-status">
                        {labels[current.status] ?? current.status}
                      </span>
                    </div>
                    {current && !current.readOnly && !current.archived && (
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={(e) => {
                          e.currentTarget
                            .closest("details")
                            ?.removeAttribute("open");
                          scheduleFor(current);
                        }}
                      >
                        <AlarmClock size={14} />
                        Schedule message…
                      </Button>
                    )}
                    {ongoing && !composing && (
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={(e) => {
                          e.currentTarget
                            .closest("details")
                            ?.removeAttribute("open");
                          startFresh(ongoing.dir);
                        }}
                        title="Starts a new conversation. What matters is kept in memory and open items carry over; do this when you change to an unrelated subject."
                      >
                        <MessageSquarePlus size={14} />
                        Start fresh
                      </Button>
                    )}
                    {topicOf?.memory === "wiki" && current && !composing && (
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={(e) => {
                          e.currentTarget
                            .closest("details")
                            ?.removeAttribute("open");
                          void api<{ started: boolean }>(
                            `assistants/${topicOf.dir}/memory`,
                            "POST",
                          )
                            .then((o) =>
                              setMemoryNote(
                                o.started
                                  ? "Updating memory from this conversation…"
                                  : "Nothing new to remember since the last update.",
                              ),
                            )
                            .catch(catchError);
                        }}
                      >
                        <BookOpen size={14} />
                        Update memory now
                      </Button>
                    )}
                    {current && (
                      <>
                        {[
                          {
                            label: "Resume engine",
                            action: "resume",
                            icon: Play,
                          },
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
                            disabled={
                              busy || current.readOnly || current.archived
                            }
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
                      </>
                    )}
                    <span className="menu-path">{current.cwd}</span>
                  </div>
                )}
                <div className="menu-group" role="group" aria-label="Workbench">
                  <div className="menu-group-title">
                    <span>Workbench</span>
                  </div>
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={(e) => {
                      e.currentTarget
                        .closest("details")
                        ?.removeAttribute("open");
                      newRoutine();
                    }}
                  >
                    <AlarmClock size={14} />
                    New routine…
                  </Button>
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={(e) => {
                      e.currentTarget
                        .closest("details")
                        ?.removeAttribute("open");
                      openSettings("routines");
                    }}
                  >
                    <ListChecks size={14} />
                    Routines
                  </Button>
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={(e) => {
                      e.currentTarget
                        .closest("details")
                        ?.removeAttribute("open");
                      openSettings();
                    }}
                  >
                    <Settings size={14} />
                    Settings
                  </Button>
                  <Appearance />
                  <NotifyToggle />
                  <SuggestToggle />
                </div>
              </div>
            </details>
            {current &&
              (() => {
                const shown = !!sideOpen[current.id];
                const live = sideLive[current.id];
                const label = live
                  ? `Side chat (${live === "running" ? "working" : "open"})`
                  : "Side chat";
                return (
                  <Button
                    variant="ghost"
                    size="icon"
                    aria-label={(shown ? "Hide " : "Show ") + label}
                    aria-pressed={shown}
                    title={(shown ? "Hide " : "Show ") + label}
                    onClick={() =>
                      setSideOpen((xs) => ({ ...xs, [current.id]: !shown }))
                    }
                  >
                    <MessagesSquare size={17} />
                    {!shown && live && (
                      <span
                        className={
                          "toggle-dot side" +
                          (live === "running" ? " running" : "")
                        }
                        aria-hidden
                      />
                    )}
                  </Button>
                );
              })()}
            <Button
              variant="ghost"
              size="icon"
              aria-label="Toggle preview"
              title={panel ? "Hide preview" : "Show preview"}
              onClick={() => (!panel || mayLeave()) && setPanel(!panel)}
            >
              <PanelRightClose size={17} />
            </Button>
          </header>
          {paired && liveDown && (
            <div className="update-banner" role="status">
              Live updates interrupted — reconnecting…
              <Button size="sm" onClick={renewStream}>
                Retry
              </Button>
            </div>
          )}
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
              ) : transcript.recordMissing ? (
                <RecordMissing remains={transcript.recordMissing} />
              ) : (
                <div className="empty-conversation">
                  <AgentIcon agent={current.agent} size={48} />
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
                        annotations,
                      ) => {
                        await act("reply", {
                          id,
                          decision,
                          message,
                          answers,
                          mode,
                          annotations,
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
                  onView={view}
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
                  onRetry={
                    current.agent === "codex" && !current.readOnly
                      ? () => void sendMessage("Continue.", "steer")
                      : undefined
                  }
                />
              </div>
              {ongoing && topicFull >= 0.4 && !composing && (
                <div className="archived-banner" role="note">
                  <MessageSquarePlus size={14} />
                  <span>
                    This conversation is getting long (
                    {Math.round(topicFull * 100)}% of its context). A fresh one
                    keeps what matters.
                  </span>
                  <Button
                    size="sm"
                    variant="ghost"
                    onClick={() => startFresh(ongoing.dir)}
                  >
                    Start fresh
                  </Button>
                </div>
              )}
              {memoryNote && (
                <div className="archived-banner" role="status">
                  <BookOpen size={14} />
                  <span>{memoryNote}</span>
                </div>
              )}
              {memory && <ChangeBanner key={memory.id} change={memory} />}
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
                // The same requests to try as its card (assistant.toml);
                // the built-in starts only when Moss has none.
                starters={
                  topicOf?.examples?.length
                    ? topicOf.examples.map((e) => ({ label: e, text: e }))
                    : topicOf?.dir === "main"
                      ? MAIN_STARTERS
                      : undefined
                }
                session={
                  transcript.recordMissing
                    ? { ...current, readOnly: true, recordMissing: true }
                    : current
                }
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
                onView={view}
              />
            </>
          ) : (
            <NewSession
              key={composing?.assistant?.dir ?? composing?.cwd ?? "new"}
              sessions={sessions}
              workspaces={workspaces}
              prefs={sidebarPrefs.prefs}
              initialCwd={composing?.cwd}
              // The live entry, so its card follows changes to its file.
              assistant={
                composing?.assistant &&
                (assistantList.assistants.find(
                  (a) => a.dir === composing.assistant?.dir,
                ) ??
                  composing.assistant)
              }
              continues={composing?.continues}
              onView={view}
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
            {/* Markdown and text files shown here open links as the
                conversation does. */}
            <EnvContext.Provider value={env}>
              <PreviewPanel
                session={active}
                tabs={previewTabs}
                opening={working.has("open")}
                expanded={expanded}
                onExpand={setExpanded}
                onError={catchError}
                files={
                  <>
                    <FileList
                      path={path}
                      entries={entries}
                      located={located}
                      here={here}
                      hasSession={!!current}
                      refreshing={working.has("files")}
                      onUp={() => goTo(path.split("/").slice(0, -1).join("/"))}
                      onRefresh={() =>
                        void busyWhile("files", () =>
                          listFiles(path).then(setEntries).catch(catchError),
                        )
                      }
                      onReveal={(e) => env.reveal(e.path)}
                      onOpen={(e) => {
                        if (e.directory) return goTo(e.path);
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
                        // Its tab opens at once; it loads its page.
                        previewTabs.open({
                          kind: "file",
                          path: e.path.replace(/^\//, ""),
                        });
                      }}
                    />
                    <div className="preview-services">
                      {previews
                        .filter((p) => p.session === active)
                        .map((p) => (
                          <div className="flex items-center gap-2" key={p.id}>
                            <button
                              className="truncate flex-1 text-left"
                              onClick={() =>
                                previewTabs.open(tabFromPreview(p))
                              }
                            >
                              {p.owned ? "●" : "○"}{" "}
                              {p.entry === "/" ? "Web service" : p.entry}
                            </button>
                            <Button
                              variant="ghost"
                              size="icon"
                              aria-label="Stop preview service"
                              disabled={working.has("stop:" + p.id)}
                              onClick={() =>
                                void busyWhile("stop:" + p.id, () =>
                                  api("previews/" + p.id, "DELETE")
                                    .then(() => refresh())
                                    .catch(catchError),
                                )
                              }
                            >
                              {working.has("stop:" + p.id) ? (
                                <RefreshCw size={13} className="spin" />
                              ) : (
                                <X size={13} />
                              )}
                            </Button>
                          </div>
                        ))}
                    </div>
                    <div className="artifact-foot">
                      Images / Video / HTML / Markdown / Text / Dev servers
                      <br />
                      <span>Loaded on demand. Original files stay intact.</span>
                    </div>
                  </>
                }
              />
            </EnvContext.Provider>
          </aside>
        )}
        <SettingsDialog
          open={settingsOpen}
          onOpenChange={setSettingsOpen}
          sessionTitle={sessionTitle}
          onEditRoutine={setRoutineDraft}
          assistants={assistantList}
          onOpenAssistant={openAssistant}
          openTab={settingsTab}
        />
        {settingsFor &&
          (() => {
            const a = assistantList.assistants.find(
              (x) => x.dir === settingsFor,
            );
            return a ? (
              <AssistantSettings
                key={a.dir}
                assistant={a}
                installed={assistantList.installed}
                onClose={() => setSettingsFor(null)}
                onOpenFile={
                  current
                    ? (p) => {
                        setPanel(true);
                        env.openPath(p);
                      }
                    : undefined
                }
              />
            ) : null;
          })()}
        {aboutOpen && topicOf && current && (
          <AboutDialog open onOpenChange={(o) => !o && setAboutOpen(false)}>
            <AboutContent className="modal modal-content setup-dialog">
              <AboutTitle className="sr-only">About {topicOf.name}</AboutTitle>
              <button
                className="modal-close"
                aria-label="Close"
                onClick={() => setAboutOpen(false)}
              >
                <X size={18} />
              </button>
              <AssistantHome
                assistant={topicOf}
                onTry={(text) => {
                  setAboutOpen(false);
                  window.dispatchEvent(
                    new CustomEvent("workbench:compose", {
                      detail: { session: current.id, text },
                    }),
                  );
                }}
                onOpenFile={(p) => {
                  setAboutOpen(false);
                  setPanel(true);
                  env.openPath(p);
                }}
              />
            </AboutContent>
          </AboutDialog>
        )}
        {settingUp && (
          <SetUpDialog
            defaultAgent={assistantList.defaultAgent ?? ""}
            onClose={() => setSettingUp(false)}
            onDone={(session) => {
              setSettingUp(false);
              setSettingsOpen(false);
              // Moss greets the user first; without a welcome, a new topic.
              if (session) {
                setComposing(null);
                selectSession(session);
              } else openAssistant("main");
            }}
          />
        )}
        {routineDraft && (
          <RoutineDialog
            draft={routineDraft}
            assistants={assistantList.assistants.filter((a) => !a.error)}
            recent={recent}
            sessionTitle={sessionTitle}
            onClose={() => setRoutineDraft(null)}
            onSaved={() =>
              window.dispatchEvent(new CustomEvent("workbench:routines"))
            }
          />
        )}
        {viewer && (
          <MediaViewer
            items={viewer.items}
            index={viewer.index}
            onClose={() => setViewer(null)}
            here={here}
            onReveal={(item) => item.path && env.reveal(item.path, item.base)}
            onPanel={(item) => {
              if (!item.path) return;
              setViewer(null);
              setPanel(true);
              env.openPath(item.path, item.base);
            }}
          />
        )}
        {current && (
          <EnvContext.Provider
            value={{
              ...env,
              answer: undefined,
              implementPlan: undefined,
              implementPlanFresh: undefined,
              dismissPlan: undefined,
            }}
          >
            <SidePanel
              key={current.id}
              parent={current.id}
              open={!!sideOpen[current.id]}
              focus={sideFocus[current.id]}
              onHide={() =>
                setSideOpen((xs) => ({ ...xs, [current.id]: false }))
              }
              onLive={(status) => {
                const id = current.id;
                setSideLive((xs) =>
                  xs[id] === status ? xs : { ...xs, [id]: status },
                );
              }}
            />
          </EnvContext.Provider>
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
                  void openPreview(
                    {
                      session: active,
                      kind: "service",
                      target,
                      command,
                      port: Number(port),
                    },
                    "service",
                  );
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
                <Button
                  className="w-full"
                  type="submit"
                  disabled={working.has("service")}
                >
                  {working.has("service") ? (
                    <>
                      {command ? "Starting…" : "Connecting…"}{" "}
                      <RefreshCw size={15} className="spin" />
                    </>
                  ) : (
                    <>
                      Open preview <Globe size={15} />
                    </>
                  )}
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
