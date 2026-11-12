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
  Archive,
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
  Terminal,
  Trash2,
  X,
} from "lucide-react";
import { Button } from "../components/ui/button";
import { AgentIcon, StatusIcon, statusLabel } from "../components/AgentIcon";
import { ATTENTION_LABEL } from "../attention";
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
  const copy = (label: string, value: string) =>
    void navigator.clipboard?.writeText(value).then(() => {
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
    </div>,
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
          (s.attention ? " needs-you" : "") +
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
            <AgentIcon agent={s.agent} size={15} />
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
                  <span className="session-kids">{node.children.length}</span>
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
      {open &&
        node.children.map((c) => (
          <SessionRow
            key={c.session.id}
            node={c}
            depth={depth + 1}
            scope={scope}
            actions={actions}
          />
        ))}
    </>
  );
});

const holds = (p: Project, id: string): boolean =>
  p.nodes.some(
    (n) => n.session.id === id || n.children.some((c) => c.session.id === id),
  ) || p.children.some((c) => holds(c, id));

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
  const [limit, setLimit] = useState(6);
  const [relatedOpen, setRelatedOpen] = useState(false);
  useEffect(() => {
    if (holdsActive) setOpen(true);
  }, [holdsActive]);
  const expanded = forceOpen || open;
  // Folders and sessions share one list ordered by activity; the entry holding
  // the selected session stays visible beyond the limit.
  let shown = forceOpen ? project.entries : project.entries.slice(0, limit);
  const selected = project.entries.find((e) =>
    e.kind === "project"
      ? holds(e.project, actions.activeId)
      : e.node.session.id === actions.activeId ||
        e.node.children.some((c) => c.session.id === actions.activeId),
  );
  if (selected && !shown.includes(selected)) shown = [...shown, selected];
  const pinOp: PrefsOp = project.pinned ? "unpinWorkspace" : "pinWorkspace";
  const showRelated = forceOpen || relatedOpen;
  return (
    <section
      className="project"
      data-namespace={project.path}
      style={{ "--depth": depth } as React.CSSProperties}
    >
      <div className="project-row" style={{ paddingLeft: depth * 12 }}>
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
        </div>
      </div>
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
          {!forceOpen && project.entries.length > limit && (
            <button
              type="button"
              className="show-more"
              style={{ paddingLeft: 36 + depth * 12 }}
              onClick={() => setLimit(limit + 15)}
            >
              Show {Math.min(15, project.entries.length - limit)} more
            </button>
          )}
          {!forceOpen && limit > 6 && (
            <button
              type="button"
              className="show-more"
              style={{ paddingLeft: 36 + depth * 12 }}
              onClick={() => setLimit(6)}
            >
              Show less
            </button>
          )}
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
                project.related.map((n) => (
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
  const holdsActive = nodes.some(
    (n) =>
      n.session.id === actions.activeId ||
      n.children.some((c) => c.session.id === actions.activeId),
  );
  const [open, setOpen] = useState(
    () => readStored("workbench-workspaces")[id] ?? holdsActive,
  );
  const [limit, setLimit] = useState(8);
  useEffect(() => {
    if (holdsActive) setOpen(true);
  }, [holdsActive]);
  const expanded = forceOpen || open;
  const shown = forceOpen ? nodes : nodes.slice(0, limit);
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
          {!forceOpen && nodes.length > limit && (
            <button
              type="button"
              className="show-more"
              onClick={() => setLimit(limit + 20)}
            >
              Show {Math.min(20, nodes.length - limit)} more
            </button>
          )}
        </div>
      )}
    </section>
  );
}

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
  count: number;
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
        <span className="section-count">{count}</span>
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
}) {
  const [query, setQuery] = useState("");
  const [showHidden, setShowHidden] = useState(false);
  const [showArchived, setShowArchived] = useState(false);
  const [menu, setMenu] = useState<string | null>(null);
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
      }),
    [sessions, prefs, query, showHidden, showArchived, workspaces],
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
        <span>workbench.</span>
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
        {model.pinned.length > 0 && (
          <Section
            id="pinned"
            title="Pinned"
            count={model.pinned.length}
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
            badge={(() => {
              const n = model.active.filter((x) => x.session.attention).length;
              return n ? `${n} need${n === 1 ? "s" : ""} you` : undefined;
            })()}
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
        <span className="status-dot idle" /> Connected locally
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
