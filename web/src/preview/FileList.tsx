import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { Download, File, Folder, FolderOpen, RefreshCw } from "lucide-react";

export type Entry = {
  name: string;
  path: string;
  directory: boolean;
  size: number;
};

// How long a touch is held to open an entry's menu, and how far it may move.
const HOLD_MS = 500;
const HOLD_SLOP = 8;

// The preview panel's file list: one folder of the session's directory.
// An entry shown from a link (located) is marked and scrolled to; each
// entry's menu (right click, long press, the context-menu key) shows it in
// the file manager, or downloads it away from the workbench computer.
export function FileList({
  path,
  entries,
  located,
  here,
  refreshing,
  hasSession,
  onUp,
  onRefresh,
  onOpen,
  onReveal,
}: {
  path: string;
  entries: Entry[];
  // The entry a link pointed at (relative to the session's directory).
  located?: string;
  here?: boolean;
  refreshing: boolean;
  hasSession: boolean;
  onUp: () => void;
  onRefresh: () => void;
  onOpen: (e: Entry) => void;
  onReveal: (e: Entry) => void;
}) {
  const list = useRef<HTMLDivElement>(null);
  const [menu, setMenu] = useState<{ entry: Entry; x: number; y: number }>();
  const hold = useRef<{ timer: number; x: number; y: number } | null>(null);
  // A long press opened the menu: the click that ends it opens nothing.
  const held = useRef(false);

  useEffect(() => {
    if (!located) return;
    const row = list.current?.querySelector<HTMLElement>(
      `[data-path="${CSS.escape(located)}"]`,
    );
    row?.scrollIntoView({ block: "nearest" });
    row?.focus({ preventScroll: true });
  }, [located, entries]);

  const cancelHold = () => {
    if (hold.current) clearTimeout(hold.current.timer);
    hold.current = null;
  };
  useEffect(() => cancelHold, []);

  return (
    <>
      <div className="file-path">
        <button aria-label="Up one folder" onClick={onUp}>
          ↑
        </button>
        <span>{path || "Working directory"}</span>
        <button
          aria-label="Refresh files"
          disabled={refreshing}
          onClick={onRefresh}
        >
          <RefreshCw size={12} className={refreshing ? "spin" : undefined} />
        </button>
      </div>
      <div className="files" ref={list}>
        {entries.map((e) => (
          <button
            key={e.path}
            data-path={e.path}
            className={e.path === located ? "located" : undefined}
            aria-haspopup="menu"
            onClick={() => {
              if (held.current) {
                held.current = false;
                return;
              }
              onOpen(e);
            }}
            onContextMenu={(ev) => {
              ev.preventDefault();
              cancelHold();
              setMenu({ entry: e, x: ev.clientX, y: ev.clientY });
            }}
            onKeyDown={(ev) => {
              if (
                ev.key === "ContextMenu" ||
                (ev.shiftKey && ev.key === "F10")
              ) {
                ev.preventDefault();
                const r = ev.currentTarget.getBoundingClientRect();
                setMenu({ entry: e, x: r.left + 24, y: r.bottom });
              }
            }}
            onPointerDown={(ev) => {
              held.current = false;
              if (ev.pointerType !== "touch") return;
              cancelHold();
              const { clientX: x, clientY: y } = ev;
              hold.current = {
                x,
                y,
                timer: window.setTimeout(() => {
                  hold.current = null;
                  held.current = true;
                  setMenu({ entry: e, x, y });
                }, HOLD_MS),
              };
            }}
            onPointerMove={(ev) => {
              const h = hold.current;
              if (
                h &&
                Math.hypot(ev.clientX - h.x, ev.clientY - h.y) > HOLD_SLOP
              )
                cancelHold();
            }}
            onPointerUp={cancelHold}
            onPointerCancel={cancelHold}
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
              {hasSession
                ? "No files in this directory"
                : "Select a session to browse outputs"}
            </p>
          </div>
        )}
      </div>
      {menu && (
        <FileMenu
          x={menu.x}
          y={menu.y}
          here={here}
          name={menu.entry.name}
          onReveal={() => {
            setMenu(undefined);
            onReveal(menu.entry);
          }}
          onClose={() => setMenu(undefined)}
        />
      )}
    </>
  );
}

const MENU_WIDTH = 240;

// An entry's menu, at the point it was asked for, kept on screen.
export function FileMenu({
  x,
  y,
  here,
  name,
  onReveal,
  onClose,
}: {
  x: number;
  y: number;
  here?: boolean;
  name: string;
  onReveal: () => void;
  onClose: () => void;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const close = useRef(onClose);
  close.current = onClose;
  const [pos, setPos] = useState({ top: y, left: x });
  useLayoutEffect(() => {
    const h = ref.current?.offsetHeight ?? 0;
    setPos({
      top: y + h > innerHeight - 8 ? Math.max(8, y - h) : y,
      left: Math.max(8, Math.min(x, innerWidth - MENU_WIDTH - 8)),
    });
  }, [x, y]);
  useEffect(() => {
    const down = (e: PointerEvent) => {
      if (!ref.current?.contains(e.target as Node)) close.current();
    };
    const key = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.preventDefault();
        close.current();
      }
    };
    const away = () => close.current();
    document.addEventListener("pointerdown", down);
    document.addEventListener("keydown", key);
    window.addEventListener("blur", away);
    window.addEventListener("resize", away);
    ref.current?.querySelector("button")?.focus();
    return () => {
      document.removeEventListener("pointerdown", down);
      document.removeEventListener("keydown", key);
      window.removeEventListener("blur", away);
      window.removeEventListener("resize", away);
    };
  }, []);
  // Away from the workbench computer, paths download (Show in Files cannot
  // open there).
  const away = here === false;
  return createPortal(
    <div
      ref={ref}
      className="row-menu file-menu"
      role="menu"
      aria-label={`${name} actions`}
      style={{ top: pos.top, left: pos.left, width: MENU_WIDTH }}
    >
      <button type="button" role="menuitem" onClick={onReveal}>
        {away ? <Download size={14} /> : <FolderOpen size={14} />}
        <span>{away ? "Download" : "Open in system file manager"}</span>
      </button>
    </div>,
    document.body,
  );
}
