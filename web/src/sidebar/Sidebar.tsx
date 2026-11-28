import {
  memo,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { createPortal } from "react-dom";
import {
  AlarmClock,
  Archive,
  ArrowDown,
  ArrowUp,
  MessageSquarePlus,
  Power,
  PowerOff,
  Check,
  RefreshCw,
  Settings,
  Wrench,
  TriangleAlert,
  ArchiveRestore,
  ChevronRight,
  Copy,
  EyeOff,
  Eye,
  Folder,
  FolderOpen,
  MoreHorizontal,
  Pencil,
  Pin,
  PinOff,
  Plus,
  Search,
  Sparkles,
  Terminal,
  Trash2,
  X,
} from "lucide-react";
import { Button } from "../components/ui/button";
import { ManualCopyDialog } from "../components/ManualCopyDialog";
import { copyText } from "../lib/copy";
import { AgentIcon, StatusIcon, statusLabel } from "../components/AgentIcon";
import { MossIcon } from "../components/MossIcon";
import { LogoLockup } from "../components/Logo";
import { ATTENTION_LABEL } from "../attention";
import type { FooterConnection } from "../lib/connection";
import {
  assistantsChanged,
  MAIN_NAME,
  moved,
  newRoutine,
  openAssistantSettings,
  reloadAssistants,
  saveOrder,
  setUpAssistant,
  setUpAssistants,
  type Assistant,
} from "../lib/assistants";
import type { WaitingRuns } from "../routines/waiting";
import { api } from "../lib/api";
import { LIST_SHOWN, ShowMoreLess, limited } from "./limit";
import { assistantRows, waitingFor } from "./waitingFor";
import {
  buildSidebar,
  relativeTime,
  resumeCommand,
  shortPath,
  type Node,
  type Prefs,
  type PrefsOp,
  type Project,
  type Session,
  needsYou,
} from "./tree";

export const statusLabels: Record<string, string> = {
  external: "Active elsewhere · Read only",
  external_running: "Running elsewhere · Read only",
  sleeping: "Sleeping",
  starting: "Connecting",
  idle: "Idle",
  running: "Running",
  waiting: "Awaiting approval",
  stopping: "Stopping",
  stopped: "Stopped",
  error: "Error",
};

const readStored = (key: string): Record<string, boolean> => {
  try {
    return JSON.parse(localStorage.getItem(key) || "{}");
  } catch {
    return {};
  }
};
const writeStored = (key: string, k: string, v: boolean) => {
  try {
    const stored = readStored(key);
    stored[k] = v;
    localStorage.setItem(key, JSON.stringify(stored));
  } catch {}
};

export type FolderOp = "archive" | "delete";

type Actions = {
  activeId: string;
  prefs: Prefs;
  now: number;
  menu: string | null;
  editing: string | null;
  onSelect: (id: string) => void;
  onPrefs: (op: PrefsOp, id: string) => void;
  onRename: (id: string, title: string) => void;
  // Native thread operations (Codex only).
  onArchive: (session: Session, archived: boolean) => void;
  onDelete: (session: Session) => void;
  // Archive or delete every session in a project folder and its folders.
  onFolder: (path: string, name: string, op: FolderOp) => void;
  // Schedule a message to the session (routines).
  onSchedule: (session: Session) => void;
  setMenu: (key: string | null) => void;
  setEditing: (key: string | null) => void;
  onNew: (cwd?: string) => void;
  query: string;
};

function Highlight({ text, query }: { text: string; query: string }) {
  const tokens = query.toLowerCase().split(/\s+/).filter(Boolean);
  if (!tokens.length) return <>{text}</>;
  const lower = text.toLowerCase();
  const marks = new Array(text.length).fill(false);
  for (const t of tokens)
    for (let i = lower.indexOf(t); i >= 0; i = lower.indexOf(t, i + 1))
      for (let k = i; k < i + t.length; k++) marks[k] = true;
  const parts: React.ReactNode[] = [];
  let start = 0;
  for (let i = 1; i <= text.length; i++)
    if (i === text.length || marks[i] !== marks[start]) {
      const chunk = text.slice(start, i);
      parts.push(marks[start] ? <mark key={start}>{chunk}</mark> : chunk);
      start = i;
    }
  return <>{parts}</>;
}

function RowMenu({
  session,
  anchor,
  editKey,
  actions,
  onClose,
}: {
  session: Session;
  anchor: DOMRect;
  editKey: string;
  actions: Actions;
  onClose: () => void;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const close = useRef(onClose);
  close.current = onClose;
  const [copied, setCopied] = useState("");
  const [manual, setManual] = useState<string | null>(null);
  const hidden = actions.prefs.hidden.includes(session.id);
  // Codex archives and deletes threads natively; Claude Code cannot, so its
  // sessions keep the workbench-only hide.
  const native = session.agent === "codex";
  const [pos, setPos] = useState({
    top: anchor.bottom + 4,
    left: anchor.right - 220,
  });
  useLayoutEffect(() => {
    const h = ref.current?.offsetHeight ?? 0;
    const top =
      anchor.bottom + 4 + h > innerHeight - 8
        ? Math.max(8, anchor.top - h - 4)
        : anchor.bottom + 4;
    setPos({
      top,
      left: Math.max(8, Math.min(anchor.right - 220, innerWidth - 228)),
    });
  }, [anchor]);
  useEffect(() => {
    if (manual !== null) return;
    const down = (e: MouseEvent) => {
      if (!ref.current?.contains(e.target as globalThis.Node)) close.current();
    };
    const key = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.preventDefault();
        close.current();
      }
    };
    document.addEventListener("mousedown", down);
    document.addEventListener("keydown", key);
    ref.current?.querySelector("button")?.focus();
    return () => {
      document.removeEventListener("mousedown", down);
      document.removeEventListener("keydown", key);
    };
  }, [manual]);
  const copy = (label: string, value: string) =>
    void copyText(value).then((ok) => {
      if (!ok) return setManual(value);
      setCopied(label);
      setTimeout(onClose, 700);
    });
  const item = (icon: React.ReactNode, label: string, run: () => void) => (
    <button type="button" role="menuitem" onClick={run}>
      {icon}
      <span>{copied === label ? "Copied" : label}</span>
    </button>
  );
  return createPortal(
    <>
      <div
        ref={ref}
        className="row-menu"
        role="menu"
        style={{ top: pos.top, left: pos.left }}
      >
        {item(<Pencil size={14} />, "Rename", () => {
          actions.setEditing(editKey);
          onClose();
        })}
        {item(<Terminal size={14} />, "Copy resume command", () =>
          copy("Copy resume command", resumeCommand(session)),
        )}
        {item(<Copy size={14} />, "Copy session ID", () =>
          copy("Copy session ID", session.nativeId),
        )}
        {!session.readOnly &&
          item(<AlarmClock size={14} />, "Schedule message…", () => {
            actions.onSchedule(session);
            onClose();
          })}
        <div className="row-menu-sep" />
        {native &&
          item(
            session.archived ? (
              <ArchiveRestore size={14} />
            ) : (
              <Archive size={14} />
            ),
            session.archived ? "Unarchive" : "Archive",
            () => {
              actions.onArchive(session, !session.archived);
              onClose();
            },
          )}
        {/* Codex sessions hidden before Archive existed can still come back. */}
        {(!native || hidden) &&
          item(
            hidden ? <Eye size={14} /> : <EyeOff size={14} />,
            hidden ? "Show in sidebar" : "Hide from sidebar",
            () => {
              actions.onPrefs(hidden ? "unhide" : "hide", session.id);
              onClose();
            },
          )}
        {native && (
          <button
            type="button"
            role="menuitem"
            className="danger"
            onClick={() => {
              actions.onDelete(session);
              onClose();
            }}
          >
            <Trash2 size={14} />
            <span>Delete…</span>
          </button>
        )}
      </div>
      <ManualCopyDialog text={manual} onClose={onClose} />
    </>,
    document.body,
  );
}

const SessionRow = memo(function SessionRow({
  node,
  depth,
  scope,
  context,
  actions,
}: {
  node: Node;
  depth: number;
  scope: string;
  context?: string;
  actions: Actions;
}) {
  const s = node.session;
  const selected = s.id === actions.activeId;
  const childSelected = node.children.some(
    (c) => c.session.id === actions.activeId,
  );
  const [open, setOpen] = useState(childSelected);
  const [limit, setLimit] = useState(LIST_SHOWN);
  const kids = limited(
    node.children,
    limit,
    (c) => keepsNode(c, actions.activeId),
    !!actions.query,
  );
  const ref = useRef<HTMLDivElement>(null);
  const menuKey = scope + ":" + s.id;
  const [anchor, setAnchor] = useState<DOMRect | null>(null);
  const editing = actions.editing === menuKey;
  const pinned = actions.prefs.pinnedSessions.includes(s.id);
  useEffect(() => {
    if (childSelected || (actions.query && node.children.length)) setOpen(true);
  }, [childSelected, actions.query, node.children.length]);
  useEffect(() => {
    if (selected && scope === "tree")
      ref.current?.scrollIntoView({ block: "nearest" });
  }, [selected, scope]);
  // Cross-project sessions get a second line naming the subprojects they touched.
  const related = s.related?.length ? "↗ " + s.related.join(", ") : undefined;
  const line2 = context ?? related;
  return (
    <>
      <div
        ref={ref}
        className={
          "session-row" +
          (line2 ? " stacked" : "") +
          (selected ? " selected" : "") +
          (node.hidden ? " is-hidden" : "") +
          (node.archived ? " is-archived" : "") +
          (actions.menu === menuKey ? " menu-open" : "")
        }
        style={{ paddingLeft: 6 + depth * 12 }}
      >
        {node.children.length > 0 ? (
          <button
            type="button"
            className="tree-toggle"
            aria-label={
              open
                ? "Collapse subsessions"
                : `Show ${node.children.length} subsessions`
            }
            aria-expanded={open}
            onClick={() => setOpen(!open)}
          >
            <ChevronRight size={12} className={open ? "chev open" : "chev"} />
          </button>
        ) : (
          <span className="tree-spacer" />
        )}
        {editing ? (
          <input
            className="rename-input"
            aria-label="Session name"
            defaultValue={node.title}
            autoFocus
            onFocus={(e) => e.currentTarget.select()}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault();
                const v = e.currentTarget.value.trim();
                if (v !== node.title) actions.onRename(s.id, v);
                actions.setEditing(null);
              } else if (e.key === "Escape") {
                e.preventDefault();
                actions.setEditing(null);
              }
            }}
            onBlur={(e) => {
              const v = e.currentTarget.value.trim();
              if (v && v !== node.title) actions.onRename(s.id, v);
              actions.setEditing(null);
            }}
          />
        ) : (
          <button
            type="button"
            className="session-main"
            aria-current={selected ? "page" : undefined}
            title={`${node.title} · ${s.agent === "codex" ? "Codex" : "Claude"} · ${(s.attention && ATTENTION_LABEL[s.attention]) || statusLabels[s.status] || s.status}`}
            onClick={() => actions.onSelect(s.id)}
          >
            <AgentIcon agent={s.agent} size={16} />
            {line2 ? (
              <span className="session-stack">
                <span className="session-line">
                  <span className="session-name">
                    <Highlight text={node.title} query={actions.query} />
                  </span>
                  <StatusIcon
                    status={s.status}
                    attention={s.attention}
                    background={s.background}
                  />
                </span>
                <span className="session-sub">
                  {[
                    line2,
                    node.children.length && !open
                      ? `${node.children.length} sub`
                      : "",
                    relativeTime(s.updated, actions.now),
                  ]
                    .filter(Boolean)
                    .join(" · ")}
                </span>
              </span>
            ) : (
              <>
                <span className="session-name">
                  <Highlight text={node.title} query={actions.query} />
                </span>
                {node.children.length > 0 && !open && (
                  <span className="session-kids">
                    {node.children.length} sub
                  </span>
                )}
                {statusLabel[s.status] || s.attention || s.background ? (
                  <StatusIcon
                    status={s.status}
                    attention={s.attention}
                    background={s.background}
                  />
                ) : (
                  <span className="session-time">
                    {relativeTime(s.updated, actions.now)}
                  </span>
                )}
              </>
            )}
          </button>
        )}
        {!editing && (
          <div className="side-actions">
            <button
              type="button"
              className="row-pin"
              aria-label={pinned ? "Unpin session" : "Pin session"}
              title={pinned ? "Unpin" : "Pin"}
              aria-pressed={pinned}
              onClick={() =>
                actions.onPrefs(pinned ? "unpinSession" : "pinSession", s.id)
              }
            >
              {pinned ? <PinOff size={13} /> : <Pin size={13} />}
            </button>
            <button
              type="button"
              className="row-more"
              aria-label="Session actions"
              aria-haspopup="menu"
              aria-expanded={actions.menu === menuKey}
              onClick={(e) => {
                setAnchor(e.currentTarget.getBoundingClientRect());
                actions.setMenu(actions.menu === menuKey ? null : menuKey);
              }}
            >
              <MoreHorizontal size={14} />
            </button>
          </div>
        )}
      </div>
      {actions.menu === menuKey && anchor && (
        <RowMenu
          session={s}
          anchor={anchor}
          editKey={menuKey}
          actions={actions}
          onClose={() => actions.setMenu(null)}
        />
      )}
      {open && (
        <>
          {kids.map((c) => (
            <SessionRow
              key={c.session.id}
              node={c}
              depth={depth + 1}
              scope={scope}
              actions={actions}
            />
          ))}
          <ShowMoreLess
            total={node.children.length}
            shown={kids.length}
            limit={limit}
            setLimit={setLimit}
            depth={depth + 1}
            all={!!actions.query}
          />
        </>
      )}
    </>
  );
});

const holds = (p: Project, id: string): boolean =>
  p.nodes.some(
    (n) => n.session.id === id || n.children.some((c) => c.session.id === id),
  ) || p.children.some((c) => holds(c, id));

// Whether a folder holds a session that needs the user, at any depth.
const waitsIn = (p: Project): boolean =>
  p.nodes.some(needsYou) || p.children.some(waitsIn);

function ProjectNode({
  project,
  actions,
  forceOpen,
  depth = 0,
}: {
  project: Project;
  actions: Actions;
  forceOpen: boolean;
  depth?: number;
}) {
  const holdsActive = holds(project, actions.activeId);
  const [open, setOpen] = useState(
    () =>
      readStored("workbench-workspaces")[project.path] ??
      (project.pinned || holdsActive),
  );
  const [limit, setLimit] = useState(LIST_SHOWN);
  const [relatedLimit, setRelatedLimit] = useState(LIST_SHOWN);
  const related = limited(
    project.related,
    relatedLimit,
    (n) => keepsNode(n, actions.activeId),
    forceOpen,
  );
  const [relatedOpen, setRelatedOpen] = useState(false);
  const [anchor, setAnchor] = useState<DOMRect | null>(null);
  const menuKey = "folder:" + project.path;
  const folder = (op: FolderOp) => {
    actions.setMenu(null);
    actions.onFolder(project.path, project.name, op);
  };
  useEffect(() => {
    if (holdsActive) setOpen(true);
  }, [holdsActive]);
  const expanded = forceOpen || open;
  // Folders and sessions share one list ordered by activity; the entries
  // holding the selected session or one that needs the user stay visible
  // beyond the limit.
  const shown = limited(
    project.entries,
    limit,
    (e) =>
      e.kind === "project"
        ? holds(e.project, actions.activeId) || waitsIn(e.project)
        : keepsNode(e.node, actions.activeId),
    forceOpen,
  );
  const pinOp: PrefsOp = project.pinned ? "unpinWorkspace" : "pinWorkspace";
  const showRelated = forceOpen || relatedOpen;
  return (
    <section
      className="project"
      data-namespace={project.path}
      style={{ "--depth": depth } as React.CSSProperties}
    >
      <div
        className={
          "project-row" + (actions.menu === menuKey ? " menu-open" : "")
        }
        style={{ paddingLeft: depth * 12 }}
      >
        <button
          type="button"
          className="project-toggle"
          title={project.path}
          aria-expanded={expanded}
          onClick={() => {
            setOpen(!expanded);
            writeStored("workbench-workspaces", project.path, !expanded);
          }}
        >
          <ChevronRight size={12} className={expanded ? "chev open" : "chev"} />
          {expanded ? <FolderOpen size={14} /> : <Folder size={14} />}
          <span className="project-name">
            <Highlight text={project.name} query={actions.query} />
          </span>
          {project.pinned && (
            <Pin
              size={11}
              className="project-pin"
              aria-label="Pinned project"
            />
          )}
          <span className="project-count">
            {project.total || ""}
            {project.related.length > 0 && (
              <span className="project-related-count">
                ↗{project.related.length}
              </span>
            )}
          </span>
        </button>
        <div className="side-actions">
          <button
            type="button"
            className="row-more"
            aria-label={project.pinned ? "Unpin project" : "Pin project"}
            title={project.pinned ? "Unpin project" : "Pin project"}
            onClick={() => actions.onPrefs(pinOp, project.path)}
          >
            {project.pinned ? <PinOff size={13} /> : <Pin size={13} />}
          </button>
          <button
            type="button"
            className="row-more"
            aria-label="New session in this project"
            title="New session here"
            onClick={() => actions.onNew(project.path)}
          >
            <Plus size={14} />
          </button>
          <button
            type="button"
            className="row-more"
            aria-label="Project actions"
            aria-haspopup="menu"
            aria-expanded={actions.menu === menuKey}
            onClick={(e) => {
              setAnchor(e.currentTarget.getBoundingClientRect());
              actions.setMenu(actions.menu === menuKey ? null : menuKey);
            }}
          >
            <MoreHorizontal size={14} />
          </button>
        </div>
      </div>
      {actions.menu === menuKey && anchor && (
        <PopMenu
          anchor={anchor}
          onClose={() => actions.setMenu(null)}
          label="Project actions"
        >
          <button
            type="button"
            role="menuitem"
            onClick={() => folder("archive")}
          >
            <Archive size={14} />
            <span>Archive all sessions…</span>
          </button>
          <button
            type="button"
            role="menuitem"
            className="danger"
            onClick={() => folder("delete")}
          >
            <Trash2 size={14} />
            <span>Delete all sessions…</span>
          </button>
        </PopMenu>
      )}
      {expanded && (
        <div className="project-sessions">
          {shown.map((e) =>
            e.kind === "project" ? (
              <ProjectNode
                key={e.project.path}
                project={e.project}
                actions={actions}
                forceOpen={forceOpen}
                depth={depth + 1}
              />
            ) : (
              <SessionRow
                key={e.node.session.id}
                node={e.node}
                depth={depth + 1}
                scope="tree"
                actions={actions}
              />
            ),
          )}
          <ShowMoreLess
            total={project.entries.length}
            shown={shown.length}
            limit={limit}
            setLimit={setLimit}
            depth={depth + 1}
            all={forceOpen}
          />
          {project.related.length > 0 && (
            <>
              <button
                type="button"
                className="related-toggle"
                style={{ paddingLeft: 22 + depth * 12 }}
                aria-expanded={showRelated}
                title="Sessions launched elsewhere that worked in this project"
                onClick={() => setRelatedOpen(!relatedOpen)}
              >
                <ChevronRight
                  size={11}
                  className={showRelated ? "chev open" : "chev"}
                />
                Related · {project.related.length}
              </button>
              {showRelated &&
                related.map((n) => (
                  <SessionRow
                    key={"related:" + n.session.id}
                    node={n}
                    depth={depth + 1}
                    scope={"related:" + project.path}
                    context={
                      "↗ " +
                      (n.session.workspace || n.session.cwd)
                        .split("/")
                        .filter(Boolean)
                        .pop()
                    }
                    actions={actions}
                  />
                ))}
              {showRelated && (
                <ShowMoreLess
                  total={project.related.length}
                  shown={related.length}
                  limit={relatedLimit}
                  setLimit={setRelatedLimit}
                  depth={depth + 1}
                  all={forceOpen}
                />
              )}
            </>
          )}
          {!project.nodes.length &&
            !project.children.length &&
            !project.related.length && (
              <span
                className="project-empty"
                style={{ paddingLeft: 36 + depth * 12 }}
              >
                No sessions yet
              </span>
            )}
        </div>
      )}
    </section>
  );
}

function OtherGroup({
  id,
  label,
  nodes,
  actions,
  forceOpen,
}: {
  id: string;
  label: string;
  nodes: Node[];
  actions: Actions;
  forceOpen: boolean;
}) {
  const holdsActive = nodes.some((n) => holdsNode(n, actions.activeId));
  const [open, setOpen] = useState(
    () => readStored("workbench-workspaces")[id] ?? holdsActive,
  );
  const [limit, setLimit] = useState(LIST_SHOWN);
  useEffect(() => {
    if (holdsActive) setOpen(true);
  }, [holdsActive]);
  const expanded = forceOpen || open;
  const shown = limited(
    nodes,
    limit,
    (n) => keepsNode(n, actions.activeId),
    forceOpen,
  );
  return (
    <section
      className="project other-group"
      data-group={id}
      style={{ "--depth": 0 } as React.CSSProperties}
    >
      <div className="project-row">
        <button
          type="button"
          className="project-toggle"
          aria-expanded={expanded}
          onClick={() => {
            setOpen(!expanded);
            writeStored("workbench-workspaces", id, !expanded);
          }}
        >
          <ChevronRight size={12} className={expanded ? "chev open" : "chev"} />
          <span className="project-name">{label}</span>
          <span className="project-count">{nodes.length}</span>
        </button>
      </div>
      {expanded && (
        <div className="project-sessions">
          {shown.map((n) => (
            <SessionRow
              key={n.session.id}
              node={n}
              depth={1}
              scope={id}
              context={shortPath(n.session.workspace || n.session.cwd)}
              actions={actions}
            />
          ))}
          <ShowMoreLess
            total={nodes.length}
            shown={shown.length}
            limit={limit}
            setLimit={setLimit}
            all={forceOpen}
          />
        </div>
      )}
    </section>
  );
}

// How many of a section's sessions wait for the user, as its header says.
// One assistant (lib/assistants.ts): opening it shows its ongoing topic or
// a new message for it; it unfolds to its sessions, newest first.
// A small menu by a row's ⋯ button, kept inside the window.
function PopMenu({
  anchor,
  onClose,
  label,
  children,
}: {
  anchor: DOMRect;
  onClose: () => void;
  label: string;
  children: React.ReactNode;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const close = useRef(onClose);
  close.current = onClose;
  const [pos, setPos] = useState({
    top: anchor.bottom + 4,
    left: anchor.right - 220,
  });
  useLayoutEffect(() => {
    const h = ref.current?.offsetHeight ?? 0;
    setPos({
      top:
        anchor.bottom + 4 + h > innerHeight - 8
          ? Math.max(8, anchor.top - h - 4)
          : anchor.bottom + 4,
      left: Math.max(8, Math.min(anchor.right - 220, innerWidth - 228)),
    });
  }, [anchor]);
  useEffect(() => {
    const down = (e: MouseEvent) => {
      if (!ref.current?.contains(e.target as globalThis.Node)) close.current();
    };
    const key = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.preventDefault();
        close.current();
      }
    };
    document.addEventListener("mousedown", down);
    document.addEventListener("keydown", key);
    ref.current?.querySelector("button")?.focus();
    return () => {
      document.removeEventListener("mousedown", down);
      document.removeEventListener("keydown", key);
    };
  }, []);
  return createPortal(
    <div
      ref={ref}
      className="row-menu"
      role="menu"
      aria-label={label}
      style={{ top: pos.top, left: pos.left }}
    >
      {children}
    </div>,
    document.body,
  );
}

// Dragging an assistant onto another moves it there (lib/assistants.ts),
// with pointer events: native drag and drop does not start from the row's
// buttons in every browser, and touch needs a long press so scrolling still
// works.
type AssistantDrag = {
  dragging: string;
  over: string;
  handlers: (dir: string) => React.HTMLAttributes<HTMLDivElement>;
};

function useAssistantDrag(onDrop: (dir: string, to: string) => void) {
  const [state, setState] = useState({ dragging: "", over: "" });
  const ref = useRef<{
    dir: string;
    id: number;
    x: number;
    y: number;
    active: boolean;
    timer?: number;
    moved: boolean;
  } | null>(null);
  const end = () => {
    const r = ref.current;
    if (r?.timer) clearTimeout(r.timer);
    ref.current = null;
    setState({ dragging: "", over: "" });
  };
  const under = (x: number, y: number) =>
    (
      document
        .elementFromPoint(x, y)
        ?.closest("[data-assistant]") as HTMLElement | null
    )?.dataset.assistant ?? "";
  const handlers = (dir: string): React.HTMLAttributes<HTMLDivElement> => ({
    onPointerDown: (e) => {
      if (e.button !== 0 || (e.target as HTMLElement).closest(".side-actions"))
        return;
      const r = {
        dir,
        id: e.pointerId,
        x: e.clientX,
        y: e.clientY,
        active: false,
        moved: false,
        timer: undefined as number | undefined,
      };
      ref.current = r;
      if (e.pointerType === "touch") {
        // A long press starts the drag; moving first scrolls.
        r.timer = window.setTimeout(() => {
          if (ref.current !== r || r.moved) return;
          r.active = true;
          setState({ dragging: dir, over: dir });
        }, 350);
      }
    },
    onPointerMove: (e) => {
      const r = ref.current;
      if (!r || r.id !== e.pointerId) return;
      const far = Math.abs(e.clientY - r.y) + Math.abs(e.clientX - r.x) > 6;
      if (!r.active) {
        if (!far) return;
        if (e.pointerType === "touch") {
          r.moved = true;
          return end();
        }
        r.active = true;
        (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
      }
      e.preventDefault();
      const over = under(e.clientX, e.clientY);
      setState({ dragging: r.dir, over: over || r.dir });
    },
    onPointerUp: (e) => {
      const r = ref.current;
      if (!r || r.id !== e.pointerId) return;
      if (r.active) {
        const to = under(e.clientX, e.clientY);
        if (to && to !== r.dir) onDrop(r.dir, to);
        // The click that ends a drag opens nothing.
        const stop = (c: MouseEvent) => {
          c.stopPropagation();
          c.preventDefault();
        };
        window.addEventListener("click", stop, { capture: true, once: true });
        setTimeout(() => window.removeEventListener("click", stop, true), 0);
      }
      end();
    },
    onPointerCancel: end,
  });
  return { ...state, handlers };
}

function AssistantRow({
  assistant: a,
  nodes,
  actions,
  onOpen,
  order,
  drag,
  open: isOpen,
  installed,
  runs = 0,
}: {
  assistant: Assistant;
  nodes: Node[];
  actions: Actions;
  onOpen: (dir: string) => void;
  // Every assistant's folder, in order: for moving.
  order: string[];
  drag: AssistantDrag;
  // It is the assistant open now: highlighted.
  open: boolean;
  installed?: { claude?: boolean; codex?: boolean };
  // Its routine runs waiting for approval: shown first, like a session
  // that needs the user.
  runs?: number;
}) {
  const [open, setOpen] = useState(false);
  const [limit, setLimit] = useState(LIST_SHOWN);
  // Searching: its matching sessions are listed, all of them, as a
  // project's are.
  const searching = !!actions.query && nodes.length > 0;
  const listed = open || searching;
  const items = limited(
    nodes,
    limit,
    (n) => keepsNode(n, actions.activeId),
    searching,
  );
  const [anchor, setAnchor] = useState<DOMRect | null>(null);
  const menuKey = "assistant:" + a.dir;
  const latest = nodes[0]?.session;
  // Its sessions and routine runs waiting for the user: one bell; else
  // its latest session's state.
  const wait = waitingFor(nodes, runs);
  const shown = latest;
  const selected = isOpen;
  const movable = a.dir !== "main";
  const at = order.indexOf(a.dir);
  const close = () => actions.setMenu(null);
  const item = (icon: React.ReactNode, label: string, run: () => void) => (
    <button
      type="button"
      role="menuitem"
      onClick={() => {
        close();
        run();
      }}
    >
      {icon}
      <span>{label}</span>
    </button>
  );
  const move = (to: string) => void saveOrder(moved(order, a.dir, to));
  const toggle = (enabled: boolean) =>
    void api(`assistants/${a.dir}`, "PATCH", { enabled }).then(
      assistantsChanged,
    );
  return (
    <>
      <div
        className={
          "session-row assistant-row" +
          (selected ? " selected" : "") +
          (a.enabled === false ? " assistant-off" : "") +
          (drag.over === a.dir && drag.dragging && drag.dragging !== a.dir
            ? " drop-target"
            : "")
        }
        style={{ paddingLeft: 6 }}
        data-assistant={a.dir}
        data-movable={movable || undefined}
        {...(movable ? drag.handlers(a.dir) : {})}
      >
        {nodes.length > 0 ? (
          <button
            type="button"
            className="tree-toggle"
            aria-label={
              listed ? "Hide its sessions" : `Show its ${nodes.length} sessions`
            }
            aria-expanded={listed}
            onClick={() => setOpen(!listed)}
          >
            <ChevronRight size={12} className={listed ? "chev open" : "chev"} />
          </button>
        ) : (
          <span className="tree-spacer" />
        )}
        <button
          type="button"
          className="session-main"
          title={
            a.error
              ? `${a.dir}/assistant.toml: ${a.error}`
              : a.enabled === false
                ? `${a.name} · turned off`
                : `${a.name} · ${a.sessions === "continuous" ? "one ongoing conversation" : "a new session for each request"}`
          }
          onClick={() => onOpen(a.dir)}
        >
          {a.agent ? (
            <MossIcon agent={a.agent} size={16} title={a.name} />
          ) : (
            <TriangleAlert size={14} aria-label="Has an error" />
          )}
          <span className="session-name">
            <Highlight text={a.name} query={actions.query} />
          </span>
          {wait.count > 0 ? (
            <span className="assistant-waiting" title={wait.title}>
              <StatusIcon status="waiting" />
              {wait.count}
            </span>
          ) : a.enabled === false ? (
            <span className="session-time">off</span>
          ) : !a.ready && !a.error ? (
            <span className="session-time assistant-setup-hint">set up</span>
          ) : shown &&
            (statusLabel[shown.status] ||
              shown.attention ||
              shown.background) ? (
            <StatusIcon
              status={shown.status}
              attention={shown.attention}
              background={shown.background}
            />
          ) : (
            <span className="session-time">
              {a.error
                ? "error"
                : latest
                  ? relativeTime(latest.updated, actions.now)
                  : ""}
            </span>
          )}
        </button>
        <div className="side-actions">
          <button
            type="button"
            className="row-more"
            aria-label={`${a.name} actions`}
            aria-haspopup="menu"
            aria-expanded={actions.menu === menuKey}
            onClick={(e) => {
              setAnchor(e.currentTarget.getBoundingClientRect());
              actions.setMenu(actions.menu === menuKey ? null : menuKey);
            }}
          >
            <MoreHorizontal size={14} />
          </button>
        </div>
      </div>
      {actions.menu === menuKey && anchor && (
        <PopMenu anchor={anchor} onClose={close} label={`${a.name} actions`}>
          {a.enabled !== false &&
            !a.error &&
            item(<FolderOpen size={14} />, "Open", () => onOpen(a.dir))}
          {a.enabled !== false &&
            a.sessions === "continuous" &&
            item(
              <MessageSquarePlus size={14} />,
              "Start fresh",
              () =>
                void api(`assistants/${a.dir}/topic`, "POST").then(() =>
                  onOpen(a.dir),
                ),
            )}
          {a.enabled !== false &&
            !a.error &&
            item(<AlarmClock size={14} />, "New routine for it…", () =>
              newRoutine(a.dir),
            )}
          {a.enabled !== false &&
            !a.error &&
            !a.ready &&
            item(
              <Wrench size={14} />,
              "Set up…",
              () => void setUpAssistant(a.dir),
            )}
          {!a.error &&
            item(<Settings size={14} />, "Settings…", () =>
              openAssistantSettings(a.dir),
            )}
          {a.dir === "main" &&
            item(
              <RefreshCw size={14} />,
              "Reload assistants",
              () => void reloadAssistants(),
            )}
          {!a.error && (
            <>
              <div className="row-menu-sep" />
              <div className="row-menu-label">Agent</div>
              {(["claude", "codex"] as const).map((v) => (
                <button
                  key={v}
                  type="button"
                  role="menuitemradio"
                  aria-checked={a.agent === v}
                  disabled={installed?.[v] === false && a.agent !== v}
                  onClick={() => {
                    close();
                    if (a.agent !== v)
                      void api(`assistants/${a.dir}`, "PATCH", {
                        agent: v,
                      }).then(assistantsChanged);
                  }}
                >
                  {a.agent === v ? (
                    <Check size={14} />
                  ) : (
                    <AgentIcon agent={v} size={14} />
                  )}
                  <span>
                    {v === "claude" ? "Claude" : "Codex"}
                    {installed?.[v] === false && " (not installed)"}
                  </span>
                </button>
              ))}
            </>
          )}
          {movable && (
            <>
              <div className="row-menu-sep" />
              {at > 1 &&
                item(<ArrowUp size={14} />, "Move up", () =>
                  move(order[at - 1]),
                )}
              {at >= 0 &&
                at < order.length - 1 &&
                item(<ArrowDown size={14} />, "Move down", () =>
                  move(order[at + 1]),
                )}
              <div className="row-menu-sep" />
              {a.enabled === false
                ? item(<Power size={14} />, "Turn on", () => toggle(true))
                : item(<PowerOff size={14} />, "Turn off", () => toggle(false))}
            </>
          )}
        </PopMenu>
      )}
      {listed && (
        <>
          {items.map((n) => (
            <SessionRow
              key={n.session.id}
              node={n}
              depth={1}
              scope="assistants"
              actions={actions}
            />
          ))}
          <ShowMoreLess
            total={nodes.length}
            shown={items.length}
            limit={limit}
            setLimit={setLimit}
            all={searching}
          />
        </>
      )}
    </>
  );
}

// Whether the session is n's, or one of its subsessions.
export function holdsNode(n: Node, id?: string | null): boolean {
  return n.session.id === id || n.children.some((c) => c.session.id === id);
}

// What a folded list keeps past its limit: the open session, and every one
// that needs the user (sidebar/limit.tsx), so a badge always has a row.
export const keepsNode = (n: Node, id?: string | null): boolean =>
  holdsNode(n, id) || needsYou(n);

function needsYouBadge(nodes: Node[]) {
  return countBadge(nodes.filter(needsYou).length);
}
const countBadge = (n: number) =>
  n ? `${n} need${n === 1 ? "s" : ""} you` : undefined;

function Section({
  id,
  title,
  count,
  forceOpen,
  defaultOpen = true,
  badge,
  children,
}: {
  id: string;
  title: string;
  // Left out where a number would say nothing.
  count?: number;
  forceOpen: boolean;
  defaultOpen?: boolean;
  // Shown even while the section is closed (sessions waiting for the user).
  badge?: string;
  children: React.ReactNode;
}) {
  const [open, setOpen] = useState(
    () => readStored("workbench-sections")[id] ?? defaultOpen,
  );
  const expanded = forceOpen || open;
  return (
    <section className="sidebar-section" aria-label={title}>
      <button
        type="button"
        className="section-head"
        aria-expanded={expanded}
        onClick={() => {
          setOpen(!expanded);
          writeStored("workbench-sections", id, !expanded);
        }}
      >
        <ChevronRight size={11} className={expanded ? "chev open" : "chev"} />
        <span>{title}</span>
        {badge && <span className="section-badge">{badge}</span>}
        {count !== undefined && <span className="section-count">{count}</span>}
      </button>
      {expanded && <div className="section-body">{children}</div>}
    </section>
  );
}

export function Sidebar({
  sessions,
  workspaces,
  prefs,
  activeId,
  onSelect,
  onNew,
  onPrefs,
  onRename,
  onArchive,
  onDelete,
  onFolder,
  onSchedule,
  assistants,
  waitingRuns = {},
  assistantRoot,
  assistantsReady,
  installedAgents,
  openAssistantDir,
  onOpenAssistant,
  connection,
  onReconnect,
}: {
  sessions: Session[];
  workspaces: string[];
  prefs: Prefs;
  activeId: string;
  onSelect: (id: string) => void;
  onNew: (cwd?: string) => void;
  onPrefs: (op: PrefsOp, id: string) => void;
  onRename: (id: string, title: string) => void;
  onArchive: (session: Session, archived: boolean) => void;
  onDelete: (session: Session) => void;
  onFolder: (path: string, name: string, op: FolderOp) => void;
  onSchedule: (session: Session) => void;
  // The assistants and their folder (lib/assistants.ts).
  assistants: Assistant[];
  // Routine runs waiting for approval, by assistant (routines/waiting.ts).
  waitingRuns?: WaitingRuns;
  assistantRoot: string;
  // Known whether they are set up; until then nothing is offered.
  assistantsReady: boolean;
  // Which agents can run, for the assistant menu's agent switch.
  installedAgents?: { claude?: boolean; codex?: boolean };
  // The assistant open now (its new request, or a session in its folder).
  openAssistantDir: string;
  onOpenAssistant: (dir: string) => void;
  // Where this page is and whether live updates arrive.
  connection: FooterConnection;
  onReconnect: () => void;
}) {
  const [query, setQuery] = useState("");
  const [showHidden, setShowHidden] = useState(false);
  const [showArchived, setShowArchived] = useState(false);
  const [menu, setMenu] = useState<string | null>(null);
  // Assistants: those turned off are under Show more; dragging reorders.
  const [showOff, setShowOff] = useState(false);
  const assistantOrder = assistants.map((a) => a.dir);
  const assistantDrag: AssistantDrag = useAssistantDrag(
    (dir, to) => void saveOrder(moved(assistantOrder, dir, to)),
  );
  const offCount = assistants.filter((a) => a.enabled === false).length;
  const [editing, setEditing] = useState<string | null>(null);
  const [now, setNow] = useState(() => Date.now());
  const search = useRef<HTMLInputElement>(null);
  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), 60000);
    const key = (e: KeyboardEvent) => {
      const el = document.activeElement as HTMLElement | null;
      const typing =
        el &&
        (el.tagName === "INPUT" ||
          el.tagName === "TEXTAREA" ||
          el.isContentEditable);
      if (e.key === "/" && !typing && !e.ctrlKey && !e.metaKey) {
        e.preventDefault();
        search.current?.focus();
      }
    };
    window.addEventListener("keydown", key);
    return () => {
      clearInterval(t);
      window.removeEventListener("keydown", key);
    };
  }, []);
  const model = useMemo(
    () =>
      buildSidebar(sessions, prefs, {
        query,
        showHidden,
        showArchived,
        workspaces,
        assistantRoot,
      }),
    [
      sessions,
      prefs,
      query,
      showHidden,
      showArchived,
      workspaces,
      assistantRoot,
    ],
  );
  // The assistants' rows and what their heading counts
  // (sidebar/waitingFor.ts).
  const rows = assistantRows(
    assistants,
    model.assistants,
    waitingRuns,
    showOff,
  );
  const names = useMemo(
    () => new Map(model.projects.map((p) => [p.path, p.name])),
    [model.projects],
  );
  const actions = useMemo<Actions>(
    () => ({
      activeId,
      prefs,
      now,
      menu,
      editing,
      onSelect,
      onPrefs,
      onRename,
      onArchive,
      onDelete,
      onFolder,
      onSchedule,
      setMenu,
      setEditing,
      onNew,
      query,
    }),
    [
      activeId,
      prefs,
      now,
      menu,
      editing,
      onSelect,
      onPrefs,
      onRename,
      onArchive,
      onDelete,
      onFolder,
      onSchedule,
      onNew,
      query,
    ],
  );
  const projectOf = (n: Node) =>
    names.get(n.session.workspace || n.session.cwd);
  const otherCount =
    model.other.chats.length +
    model.other.automated.length +
    model.other.temporary.length;
  const nothing =
    model.searching &&
    !model.pinned.length &&
    !model.active.length &&
    !model.projects.length &&
    !otherCount;
  return (
    <aside className="sidebar">
      <div className="brand">
        <LogoLockup />
        {/* A routine in one click (lib/assistants.ts newRoutine). */}
        <Button
          variant="ghost"
          size="icon"
          aria-label="New routine"
          title="New routine"
          className="brand-routine"
          onClick={() => newRoutine()}
        >
          <AlarmClock size={16} />
        </Button>
        <Button
          variant="ghost"
          size="icon"
          aria-label="New session"
          title="New session"
          onClick={() => onNew()}
        >
          <Plus size={16} />
        </Button>
      </div>
      <div className="sidebar-search">
        <Search size={14} />
        <input
          ref={search}
          aria-label="Search sessions"
          placeholder="Search sessions"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Escape") {
              setQuery("");
              e.currentTarget.blur();
            }
          }}
        />
        {query ? (
          <button
            type="button"
            aria-label="Clear search"
            onClick={() => setQuery("")}
          >
            <X size={13} />
          </button>
        ) : (
          <kbd>/</kbd>
        )}
      </div>
      <nav className="sidebar-scroll" aria-label="Sessions">
        {/* Not set up: the personal assistant is offered, and set up on
            first use (assistants/SetUpDialog.tsx). */}
        {assistantsReady && !assistantRoot && !model.searching && (
          <Section id="assistants" title="Assistants" forceOpen={false}>
            <div
              className="session-row assistant-row"
              style={{ paddingLeft: 6 }}
            >
              <span className="tree-spacer" />
              <button
                type="button"
                className="session-main"
                title="Set up your personal assistant"
                onClick={setUpAssistants}
              >
                <Sparkles size={15} aria-hidden />
                <span className="session-name">{MAIN_NAME}</span>
                <span className="session-time">Set up</span>
              </button>
            </div>
          </Section>
        )}
        {assistants.length > 0 && (
          <Section
            id="assistants"
            title="Assistants"
            count={assistants.length - offCount}
            badge={countBadge(rows.waiting)}
            forceOpen={false}
          >
            {rows.listed.map((r) => (
              <AssistantRow
                key={r.assistant.dir}
                assistant={r.assistant}
                nodes={r.nodes}
                actions={actions}
                onOpen={onOpenAssistant}
                order={assistantOrder}
                drag={assistantDrag}
                open={r.assistant.dir === openAssistantDir}
                installed={installedAgents}
                runs={r.runs}
              />
            ))}
            {(showOff ? offCount > 0 : rows.folded > 0) && (
              <button
                type="button"
                className="show-more"
                onClick={() => setShowOff(!showOff)}
              >
                {showOff ? "Show less" : `Show ${rows.folded} more (off)`}
              </button>
            )}
          </Section>
        )}
        {model.pinned.length > 0 && (
          <Section
            id="pinned"
            title="Pinned"
            count={model.pinned.length}
            badge={needsYouBadge(model.pinned)}
            forceOpen={model.searching}
          >
            {model.pinned.map((n) => (
              <SessionRow
                key={n.session.id}
                node={n}
                depth={0}
                scope="pinned"
                context={projectOf(n)}
                actions={actions}
              />
            ))}
          </Section>
        )}
        {model.active.length > 0 && (
          <Section
            id="active"
            title="Active"
            count={model.active.length}
            badge={needsYouBadge(model.active)}
            forceOpen={model.searching}
          >
            {model.active.map((n) => (
              <SessionRow
                key={n.session.id}
                node={n}
                depth={0}
                scope="active"
                context={projectOf(n)}
                actions={actions}
              />
            ))}
          </Section>
        )}
        <Section
          id="projects"
          title="Projects"
          count={model.projects.length}
          forceOpen={model.searching}
        >
          {model.projects.map((p) => (
            <ProjectNode
              key={p.path}
              project={p}
              actions={actions}
              forceOpen={model.searching}
            />
          ))}
        </Section>
        {otherCount > 0 && (
          <Section
            id="other"
            title="Other"
            count={otherCount}
            forceOpen={model.searching}
            defaultOpen={false}
          >
            {(
              [
                ["other:chats", "Chats", model.other.chats],
                ["other:automated", "Automated runs", model.other.automated],
                ["other:temporary", "Temporary", model.other.temporary],
              ] as const
            ).map(
              ([id, label, nodes]) =>
                nodes.length > 0 && (
                  <OtherGroup
                    key={id}
                    id={id}
                    label={label}
                    nodes={[...nodes]}
                    actions={actions}
                    forceOpen={model.searching}
                  />
                ),
            )}
          </Section>
        )}
        {nothing && (
          <p className="sidebar-empty">No sessions match “{query}”.</p>
        )}
        {!sessions.length && !model.searching && (
          <p className="sidebar-empty">
            Create a session with your agent and working directory.
          </p>
        )}
      </nav>
      <div className="sidebar-footer">
        <span
          className="sidebar-connection"
          title={connection.title}
          role="status"
        >
          <span className={`status-dot ${connection.tone}`} />
          {connection.label}
          {connection.tone !== "ok" && (
            <button
              type="button"
              className="hidden-toggle"
              onClick={onReconnect}
            >
              Retry
            </button>
          )}
        </span>
        {model.hiddenCount > 0 && (
          <button
            type="button"
            className="hidden-toggle"
            aria-pressed={showHidden}
            onClick={() => setShowHidden(!showHidden)}
          >
            {showHidden ? <EyeOff size={12} /> : <Eye size={12} />}
            {showHidden ? "Hide hidden" : `Hidden ${model.hiddenCount}`}
          </button>
        )}
        {model.archivedCount > 0 && (
          <button
            type="button"
            className="hidden-toggle"
            aria-pressed={showArchived}
            onClick={() => setShowArchived(!showArchived)}
          >
            <Archive size={12} />
            {showArchived ? "Hide archived" : `Archived ${model.archivedCount}`}
          </button>
        )}
      </div>
    </aside>
  );
}
