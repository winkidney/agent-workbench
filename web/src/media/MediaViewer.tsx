import { useEffect, useRef, useState } from "react";
import {
  ChevronLeft,
  ChevronRight,
  FolderOpen,
  ImageOff,
  Maximize,
  PanelRight,
  X,
  ZoomIn,
  ZoomOut,
} from "lucide-react";
import type { ViewItem } from "../transcript/media";
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

// A popup for images (zoom with the wheel or a pinch, drag to pan) and
// videos (native player), with ←/→ between the items of one message.
export function MediaViewer({
  items,
  index,
  onClose,
  onReveal,
  onPanel,
}: {
  items: ViewItem[];
  index: number;
  onClose: () => void;
  onReveal: (item: ViewItem) => void;
  onPanel: (item: ViewItem) => void;
}) {
  const [at, setAt] = useState(index);
  const item = items[at] ?? items[0];
  const [natural, setNatural] = useState<Size | null>(null);
  const [view, setView] = useState<View | null>(null);
  const [failed, setFailed] = useState(false);
  const dialog = useRef<HTMLDivElement>(null);
  const stage = useRef<HTMLDivElement>(null);
  const pointers = useRef(new Map<number, { x: number; y: number }>());
  const gesture = useRef({ moved: 0, pinch: 0 });

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

  // Focus moves into the dialog and back to the opener on close.
  useEffect(() => {
    const opener = document.activeElement as HTMLElement | null;
    dialog.current?.focus();
    return () => opener?.focus?.();
  }, []);
  useEffect(() => {
    setNatural(null);
    setView(null);
    setFailed(false);
  }, [at]);
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

  const name = item.title.split("/").pop() || item.title;
  const dir = item.title.includes("/")
    ? item.title.slice(0, item.title.lastIndexOf("/") + 1)
    : "";
  return (
    <div
      ref={dialog}
      className="viewer"
      role="dialog"
      aria-modal="true"
      aria-label={`Viewing ${name}`}
      tabIndex={-1}
      onKeyDown={(e) => {
        const inVideo = (e.target as HTMLElement).tagName === "VIDEO";
        if (e.key === "Escape") {
          e.preventDefault();
          onClose();
        } else if (e.key === "ArrowLeft" && !inVideo && items.length > 1)
          go(-1);
        else if (e.key === "ArrowRight" && !inVideo && items.length > 1) go(1);
        else if (item.kind === "image") {
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
          {item.path && (
            <>
              <button
                type="button"
                title="Show in Files (opens on this computer)"
                onClick={() => onReveal(item)}
              >
                <FolderOpen size={15} />
                <span>Show in Files</span>
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
            onClick={onClose}
          >
            <X size={17} />
          </button>
        </div>
      </div>
      <div
        ref={stage}
        className={"viewer-stage " + item.kind}
        onPointerDown={(e) => {
          if (item.kind !== "image" || e.button !== 0) return;
          // Buttons on the stage keep their own clicks.
          if ((e.target as HTMLElement).closest("button")) return;
          e.currentTarget.setPointerCapture(e.pointerId);
          pointers.current.set(e.pointerId, { x: e.clientX, y: e.clientY });
          gesture.current.moved = 0;
          if (pointers.current.size === 2) {
            const [a, b] = [...pointers.current.values()];
            gesture.current.pinch = Math.hypot(a.x - b.x, a.y - b.y);
          }
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
          } else {
            const dx = now.x - last.x;
            const dy = now.y - last.y;
            gesture.current.moved += Math.abs(dx) + Math.abs(dy);
            setView((v) => (v ? pan(v, dx, dy) : v));
          }
        }}
        onPointerUp={(e) => {
          pointers.current.delete(e.pointerId);
          if (pointers.current.size < 2) gesture.current.pinch = 0;
        }}
        onPointerCancel={(e) => pointers.current.delete(e.pointerId)}
        onClick={(e) => {
          // A click on the empty backdrop (not after a drag) closes.
          if (e.target === e.currentTarget && gesture.current.moved < 4)
            onClose();
        }}
        onDoubleClick={() => {
          if (!natural || !view) return;
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
                <FolderOpen size={14} /> Show in Files
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
          <img
            key={item.src}
            className="viewer-image"
            src={item.src}
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
        )}
        {items.length > 1 && (
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
