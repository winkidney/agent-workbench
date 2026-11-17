import { useEffect, useRef, useState } from "react";
import {
  ChevronLeft,
  ChevronRight,
  ClipboardPaste,
  Copy,
  Download,
  FolderOpen,
  Hand,
  ImageOff,
  Maximize,
  PanelRight,
  PenLine,
  Pencil,
  Redo2,
  Square,
  Type,
  Undo2,
  X,
  ZoomIn,
  ZoomOut,
} from "lucide-react";
import type { ViewItem } from "../transcript/media";
import { composerInbox } from "../composer/inbox";
import {
  centered,
  fit,
  fitScale,
  pan,
  step,
  zoomAt,
  type Size,
  type View,
} from "./zoom";
import {
  COLORS,
  EMPTY,
  addMark,
  box,
  exportName,
  exportPng,
  foreign,
  redo,
  strokeWidth,
  textSize,
  toImage,
  undo,
  type History,
  type Mark,
  type Tool,
} from "./markup";
import { MarkLayer } from "./Markup";

const TOOLS: { tool: Tool; label: string; Icon: typeof Hand }[] = [
  { tool: "move", label: "Move", Icon: Hand },
  { tool: "rect", label: "Box", Icon: Square },
  { tool: "pen", label: "Pen", Icon: PenLine },
  { tool: "text", label: "Text", Icon: Type },
];

// Workspace images are fetched afresh each time the viewer opens one:
// agents rewrite files under the same name.
const fresh = (item: ViewItem) =>
  item.kind === "image" && item.src.startsWith("/api/media?");

const dataUrl = (b: Blob) =>
  new Promise<string>((resolve, reject) => {
    const r = new FileReader();
    r.onload = () => resolve(r.result as string);
    r.onerror = () => reject(r.error);
    r.readAsDataURL(b);
  });

type Typing = {
  id: number;
  x: number;
  y: number;
  text: string;
  color: string;
  size: number;
};

// A popup for images (zoom with the wheel or a pinch, drag to pan) and
// videos (native player), with ←/→ between the items of one message. An
// image can be marked up (boxes, strokes, text) and copied, or handed to
// the message being written. Marks live only while it is shown here.
export function MediaViewer({
  items,
  index,
  onClose,
  onReveal,
  onPanel,
  here = true,
}: {
  items: ViewItem[];
  index: number;
  onClose: () => void;
  onReveal: (item: ViewItem) => void;
  onPanel: (item: ViewItem) => void;
  // On the workbench computer: "Show in Files"; elsewhere, onReveal
  // downloads.
  here?: boolean;
}) {
  const [at, setAt] = useState(index);
  const item = items[at] ?? items[0];
  const [natural, setNatural] = useState<Size | null>(null);
  const [view, setView] = useState<View | null>(null);
  const [failed, setFailed] = useState(false);
  // What the <img> shows: the item's address, or a fresh copy of it.
  const [shown, setShown] = useState<string | null>(() =>
    fresh(item) ? null : item.src,
  );
  const [editing, setEditing] = useState(false);
  const [tool, setTool] = useState<Tool>("rect");
  const [color, setColor] = useState(COLORS[0].value);
  const [history, setHistory] = useState<History>(EMPTY);
  const [draft, setDraftState] = useState<Mark | null>(null);
  const draftRef = useRef<Mark | null>(null);
  const setDraft = (m: Mark | null) => {
    draftRef.current = m;
    setDraftState(m);
  };
  const [typing, setTypingState] = useState<Typing | null>(null);
  const typingRef = useRef<Typing | null>(null);
  const setTyping = (t: Typing | null) => {
    typingRef.current = t;
    setTypingState(t);
  };
  const typingSeq = useRef(0);
  const [notice, setNotice] = useState("");
  const noticeTimer = useRef<ReturnType<typeof setTimeout>>(undefined);
  const dialog = useRef<HTMLDivElement>(null);
  const stage = useRef<HTMLDivElement>(null);
  const image = useRef<HTMLImageElement>(null);
  const pointers = useRef(new Map<number, { x: number; y: number }>());
  const gesture = useRef({ moved: 0, pinch: 0 });
  const drawing = useRef<{ pointer: number; x: number; y: number } | null>(
    null,
  );

  const marked = history.done.length > 0;
  const drawTool = editing && tool !== "move";
  const local = item.kind === "image" && !foreign(item.src);
  const ready = local && !!natural && !!shown && !failed;
  const inbox = composerInbox();

  const stageSize = (): Size => {
    const r = stage.current?.getBoundingClientRect();
    return { w: r?.width ?? innerWidth, h: r?.height ?? innerHeight };
  };
  const zoomBy = (factor: number, px?: number, py?: number) =>
    setView((v) => {
      if (!v) return v;
      const s = stageSize();
      return zoomAt(v, factor, px ?? s.w / 2, py ?? s.h / 2);
    });
  const toFit = () => natural && setView(fit(stageSize(), natural));
  const toActual = () => natural && setView(centered(stageSize(), natural, 1));
  const go = (d: number) => setAt((i) => step(i, d, items.length));
  const say = (text: string) => {
    setNotice(text);
    clearTimeout(noticeTimer.current);
    noticeTimer.current = setTimeout(() => setNotice(""), 2500);
  };
  // Leaving with marks drawn asks first.
  const close = () => {
    if (marked && !confirm("Discard your markup?")) return;
    onClose();
  };
  const commitTyping = () => {
    const t = typingRef.current;
    if (!t) return;
    setTyping(null);
    if (t.text.trim())
      setHistory((h) =>
        addMark(h, {
          kind: "text",
          x: t.x,
          y: t.y,
          text: t.text,
          color: t.color,
          size: t.size,
        }),
      );
  };
  const stagePoint = (e: { clientX: number; clientY: number }) => {
    const r = stage.current!.getBoundingClientRect();
    return toImage(view!, e.clientX - r.left, e.clientY - r.top);
  };

  // Focus moves into the dialog and back to the opener on close.
  useEffect(() => {
    const opener = document.activeElement as HTMLElement | null;
    dialog.current?.focus();
    return () => {
      clearTimeout(noticeTimer.current);
      opener?.focus?.();
    };
  }, []);
  useEffect(() => {
    setNatural(null);
    setView(null);
    setFailed(false);
    setHistory(EMPTY);
    setDraft(null);
    setTyping(null);
  }, [at]);
  // A workspace image is fetched again, revalidated (a 304 when unchanged),
  // and shown from a data: address (the page's policy allows no blob:
  // images), let go with the viewer. The browser would otherwise reuse an
  // image it holds under the same address.
  useEffect(() => {
    if (!fresh(item)) {
      setShown(item.src);
      return;
    }
    setShown(null);
    const control = new AbortController();
    fetch(item.src, { cache: "no-cache", signal: control.signal })
      .then((r) => {
        if (!r.ok) throw new Error(r.statusText);
        return r.blob();
      })
      .then(dataUrl)
      .then((url) => {
        if (!control.signal.aborted) setShown(url);
      })
      .catch(() => {
        if (!control.signal.aborted) setFailed(true);
      });
    return () => control.abort();
  }, [item.src, item.kind]);
  // The wheel must not scroll the page, so it needs a non-passive listener.
  useEffect(() => {
    const el = stage.current;
    if (!el) return;
    const onWheel = (e: WheelEvent) => {
      if (item.kind !== "image") return;
      e.preventDefault();
      const r = el.getBoundingClientRect();
      zoomBy(
        Math.exp(-e.deltaY * 0.0015),
        e.clientX - r.left,
        e.clientY - r.top,
      );
    };
    el.addEventListener("wheel", onWheel, { passive: false });
    return () => el.removeEventListener("wheel", onWheel);
  }, [item.kind]);
  useEffect(() => {
    const onResize = () => toFit();
    addEventListener("resize", onResize);
    return () => removeEventListener("resize", onResize);
  });

  // The image as shown, marks and all, at its own size.
  const render = () => exportPng(image.current!, natural!, history.done);
  const copy = async () => {
    if (!ready) return;
    try {
      if (!navigator.clipboard?.write || typeof ClipboardItem === "undefined") {
        // Without a secure page there is no clipboard for images.
        const blob = await render();
        const a = document.createElement("a");
        a.href = URL.createObjectURL(blob);
        a.download = exportName(item.title, marked);
        a.click();
        setTimeout(() => URL.revokeObjectURL(a.href), 1000);
        say("Downloaded: copying images needs HTTPS or this computer");
        return;
      }
      // The promise goes in as is: Safari wants the clipboard write to
      // start within the click.
      await navigator.clipboard.write([
        new ClipboardItem({ "image/png": render() }),
      ]);
      say("Copied");
    } catch (e) {
      say("Couldn't copy: " + (e instanceof Error ? e.message : String(e)));
    }
  };
  const paste = async () => {
    if (!ready || !inbox) return;
    try {
      const blob = await render();
      const file = new File([blob], exportName(item.title, marked), {
        type: "image/png",
      });
      if (item.attachment) inbox.replace(item.attachment, file);
      else inbox.add([file]);
      onClose();
    } catch (e) {
      say("Couldn't add it: " + (e instanceof Error ? e.message : String(e)));
    }
  };

  const name = item.title.split("/").pop() || item.title;
  const dir = item.title.includes("/")
    ? item.title.slice(0, item.title.lastIndexOf("/") + 1)
    : "";
  const notLocal = "This image comes from another site";
  return (
    <div
      ref={dialog}
      className="viewer"
      role="dialog"
      aria-modal="true"
      aria-label={`Viewing ${name}`}
      tabIndex={-1}
      onKeyDown={(e) => {
        const target = e.target as HTMLElement;
        // The text being typed onto the image keeps its keys.
        if (target.tagName === "INPUT") return;
        const inVideo = target.tagName === "VIDEO";
        const mod = e.ctrlKey || e.metaKey;
        if (mod && (e.key === "z" || e.key === "Z") && (editing || marked)) {
          e.preventDefault();
          setHistory(e.shiftKey ? redo : undo);
        } else if (mod && e.key === "y" && (editing || marked)) {
          e.preventDefault();
          setHistory(redo);
        } else if (e.key === "Escape") {
          e.preventDefault();
          close();
        } else if (
          e.key === "ArrowLeft" &&
          !inVideo &&
          items.length > 1 &&
          !marked
        )
          go(-1);
        else if (
          e.key === "ArrowRight" &&
          !inVideo &&
          items.length > 1 &&
          !marked
        )
          go(1);
        else if (item.kind === "image" && !mod) {
          if (e.key === "+" || e.key === "=") zoomBy(1.25);
          else if (e.key === "-") zoomBy(0.8);
          else if (e.key === "0") toFit();
          else if (e.key === "1") toActual();
        }
      }}
    >
      <div className="viewer-bar">
        <div className="viewer-title" title={item.title}>
          {dir && <span className="viewer-dir">{dir}</span>}
          <span>{name}</span>
          {items.length > 1 && (
            <span className="viewer-count">
              {at + 1} / {items.length}
            </span>
          )}
        </div>
        {notice && (
          <span className="viewer-notice" role="status">
            {notice}
          </span>
        )}
        {item.kind === "image" && view && (
          <div className="viewer-zoom" role="group" aria-label="Zoom">
            <button
              type="button"
              title="Zoom out (−)"
              onClick={() => zoomBy(0.8)}
            >
              <ZoomOut size={15} />
            </button>
            <span className="viewer-scale">{Math.round(view.s * 100)}%</span>
            <button
              type="button"
              title="Zoom in (+)"
              onClick={() => zoomBy(1.25)}
            >
              <ZoomIn size={15} />
            </button>
            <button type="button" title="Fit (0)" onClick={toFit}>
              <Maximize size={14} />
            </button>
            <button type="button" title="Actual size (1)" onClick={toActual}>
              1:1
            </button>
          </div>
        )}
        <div className="viewer-actions">
          {item.kind === "image" && (
            <>
              <button
                type="button"
                title={local ? "Mark up this image" : notLocal}
                aria-label="Mark up"
                aria-pressed={editing}
                disabled={!ready}
                className={editing ? "on" : undefined}
                onClick={() => {
                  commitTyping();
                  setEditing((v) => !v);
                }}
              >
                <Pencil size={15} />
                <span>Mark up</span>
              </button>
              <button
                type="button"
                title={local ? "Copy the image" : notLocal}
                aria-label="Copy"
                disabled={!ready}
                onClick={() => void copy()}
              >
                <Copy size={15} />
                <span>Copy</span>
              </button>
              {inbox && (
                <button
                  type="button"
                  title={
                    !local
                      ? notLocal
                      : item.attachment
                        ? "Put this copy in place of the image in your message"
                        : "Add the image to your message"
                  }
                  aria-label={
                    item.attachment ? "Replace in message" : "Add to message"
                  }
                  disabled={!ready}
                  onClick={() => void paste()}
                >
                  <ClipboardPaste size={15} />
                  <span>
                    {item.attachment ? "Replace in message" : "Add to message"}
                  </span>
                </button>
              )}
            </>
          )}
          {item.path && (
            <>
              <button
                type="button"
                title={
                  here ? "Show in Files (opens on this computer)" : "Download"
                }
                onClick={() => onReveal(item)}
              >
                {here ? <FolderOpen size={15} /> : <Download size={15} />}
                <span>{here ? "Show in Files" : "Download"}</span>
              </button>
              <button
                type="button"
                title="Open in side panel"
                onClick={() => onPanel(item)}
              >
                <PanelRight size={15} />
              </button>
            </>
          )}
          <button
            type="button"
            title="Close (Esc)"
            aria-label="Close"
            onClick={close}
          >
            <X size={17} />
          </button>
        </div>
      </div>
      {editing && item.kind === "image" && (
        <div className="viewer-tools" role="toolbar" aria-label="Mark up">
          {TOOLS.map(({ tool: t, label, Icon }) => (
            <button
              key={t}
              type="button"
              title={label}
              aria-label={label}
              aria-pressed={tool === t}
              className={tool === t ? "on" : undefined}
              onClick={() => {
                commitTyping();
                setTool(t);
              }}
            >
              <Icon size={15} />
            </button>
          ))}
          <span className="viewer-sep" />
          {COLORS.map((c) => (
            <button
              key={c.value}
              type="button"
              className={"viewer-swatch" + (color === c.value ? " on" : "")}
              title={c.name}
              aria-label={c.name}
              aria-pressed={color === c.value}
              onClick={() => setColor(c.value)}
            >
              <span style={{ background: c.value }} />
            </button>
          ))}
          <span className="viewer-sep" />
          <button
            type="button"
            title="Undo (Ctrl+Z)"
            aria-label="Undo"
            disabled={!marked}
            onClick={() => setHistory(undo)}
          >
            <Undo2 size={15} />
          </button>
          <button
            type="button"
            title="Redo (Ctrl+Shift+Z)"
            aria-label="Redo"
            disabled={!history.undone.length}
            onClick={() => setHistory(redo)}
          >
            <Redo2 size={15} />
          </button>
        </div>
      )}
      <div
        ref={stage}
        className={
          "viewer-stage " + item.kind + (drawTool ? " drawing " + tool : "")
        }
        onPointerDown={(e) => {
          if (item.kind !== "image" || e.button !== 0) return;
          // Buttons and the text being typed keep their own clicks.
          if ((e.target as HTMLElement).closest("button, input")) return;
          e.currentTarget.setPointerCapture(e.pointerId);
          pointers.current.set(e.pointerId, { x: e.clientX, y: e.clientY });
          gesture.current.moved = 0;
          if (pointers.current.size === 2) {
            // A second finger zooms rather than draws.
            drawing.current = null;
            setDraft(null);
            const [a, b] = [...pointers.current.values()];
            gesture.current.pinch = Math.hypot(a.x - b.x, a.y - b.y);
            return;
          }
          if (!drawTool || tool === "text" || !view || !natural) return;
          const p = stagePoint(e);
          const width = strokeWidth(natural);
          drawing.current = { pointer: e.pointerId, ...p };
          setDraft(
            tool === "rect"
              ? { kind: "rect", ...p, w: 0, h: 0, color, width }
              : { kind: "pen", points: [p.x, p.y], color, width },
          );
        }}
        onPointerMove={(e) => {
          const last = pointers.current.get(e.pointerId);
          if (!last) return;
          const now = { x: e.clientX, y: e.clientY };
          pointers.current.set(e.pointerId, now);
          if (pointers.current.size === 2) {
            const [a, b] = [...pointers.current.values()];
            const d = Math.hypot(a.x - b.x, a.y - b.y);
            const r = e.currentTarget.getBoundingClientRect();
            if (gesture.current.pinch)
              zoomBy(
                d / gesture.current.pinch,
                (a.x + b.x) / 2 - r.left,
                (a.y + b.y) / 2 - r.top,
              );
            gesture.current.pinch = d;
            gesture.current.moved += 10;
            return;
          }
          const dx = now.x - last.x;
          const dy = now.y - last.y;
          gesture.current.moved += Math.abs(dx) + Math.abs(dy);
          const start = drawing.current;
          const d = draftRef.current;
          if (start && d && view) {
            const p = stagePoint(e);
            if (d.kind === "rect")
              setDraft({ ...d, ...box(start.x, start.y, p.x, p.y) });
            else if (d.kind === "pen") {
              const n = d.points.length;
              // Points closer than 2 screen pixels add nothing.
              const far =
                Math.hypot(p.x - d.points[n - 2], p.y - d.points[n - 1]) *
                  view.s >=
                2;
              if (far) setDraft({ ...d, points: [...d.points, p.x, p.y] });
            }
          } else if (!drawTool) setView((v) => (v ? pan(v, dx, dy) : v));
        }}
        onPointerUp={(e) => {
          pointers.current.delete(e.pointerId);
          if (pointers.current.size < 2) gesture.current.pinch = 0;
          if (drawing.current?.pointer !== e.pointerId) return;
          drawing.current = null;
          const d = draftRef.current;
          setDraft(null);
          // A box too small to see was a slip.
          if (!d || !view) return;
          if (d.kind === "rect" && Math.max(d.w, d.h) * view.s < 4) return;
          setHistory((h) => addMark(h, d));
        }}
        onPointerCancel={(e) => {
          pointers.current.delete(e.pointerId);
          if (drawing.current?.pointer === e.pointerId) {
            drawing.current = null;
            setDraft(null);
          }
        }}
        onClick={(e) => {
          const target = e.target as HTMLElement;
          if (gesture.current.moved >= 4 || target.closest("button, input"))
            return;
          // Text goes where the image is clicked (after the press, so the
          // input keeps the focus).
          if (drawTool && tool === "text" && view && natural) {
            commitTyping();
            const p = stagePoint(e);
            setTyping({
              id: ++typingSeq.current,
              ...p,
              text: "",
              color,
              size: textSize(natural),
            });
            return;
          }
          // A click on the empty backdrop closes, not while marking up.
          if (!editing && target === e.currentTarget) close();
        }}
        onDoubleClick={() => {
          if (drawTool || !natural || !view) return;
          const fitted =
            Math.abs(view.s - fitScale(stageSize(), natural)) < 1e-3;
          if (fitted) toActual();
          else toFit();
        }}
      >
        {failed ? (
          <div className="viewer-error">
            <ImageOff size={28} />
            <p>Couldn't load {name}</p>
            {item.path && (
              <button type="button" onClick={() => onReveal(item)}>
                {here ? (
                  <>
                    <FolderOpen size={14} /> Show in Files
                  </>
                ) : (
                  <>
                    <Download size={14} /> Download
                  </>
                )}
              </button>
            )}
          </div>
        ) : item.kind === "video" ? (
          <video
            key={item.src}
            className="viewer-video"
            src={item.src}
            controls
            autoPlay
            playsInline
            onError={() => setFailed(true)}
          />
        ) : (
          shown && (
            <img
              ref={image}
              key={shown}
              className="viewer-image"
              src={shown}
              alt={item.title}
              draggable={false}
              style={
                view && natural
                  ? {
                      width: natural.w,
                      height: natural.h,
                      transform: `translate(${view.x}px, ${view.y}px) scale(${view.s})`,
                    }
                  : { visibility: "hidden" }
              }
              onLoad={(e) => {
                const img = e.currentTarget;
                const n = { w: img.naturalWidth, h: img.naturalHeight };
                setNatural(n);
                setView(fit(stageSize(), n));
              }}
              onError={() => setFailed(true)}
            />
          )
        )}
        {item.kind === "image" &&
          natural &&
          view &&
          (marked || draft) &&
          !failed && (
            <MarkLayer
              marks={draft ? [...history.done, draft] : history.done}
              natural={natural}
              view={view}
            />
          )}
        {typing && view && (
          <input
            key={typing.id}
            className="viewer-text-input"
            aria-label="Text to add to the image"
            placeholder="Type, then Enter"
            autoFocus
            value={typing.text}
            style={{
              left: view.x + typing.x * view.s,
              top: view.y + typing.y * view.s,
              fontSize: Math.max(12, typing.size * view.s),
              color: typing.color,
            }}
            onChange={(e) => setTyping({ ...typing, text: e.target.value })}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault();
                commitTyping();
                dialog.current?.focus();
              } else if (e.key === "Escape") {
                e.preventDefault();
                e.stopPropagation();
                setTyping(null);
                dialog.current?.focus();
              }
            }}
            onBlur={() => {
              if (typingRef.current?.id === typing.id) commitTyping();
            }}
          />
        )}
        {items.length > 1 && !marked && (
          <>
            <button
              type="button"
              className="viewer-nav prev"
              aria-label="Previous"
              onClick={() => go(-1)}
            >
              <ChevronLeft size={22} />
            </button>
            <button
              type="button"
              className="viewer-nav next"
              aria-label="Next"
              onClick={() => go(1)}
            >
              <ChevronRight size={22} />
            </button>
          </>
        )}
      </div>
    </div>
  );
}
