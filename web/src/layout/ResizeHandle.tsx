import { useRef } from "react";
import { DEFAULT_WIDTHS, LIMITS, type Side } from "./widths";

// Drag handle on a side panel's inner edge. Dragging toward the conversation
// widens the panel; the parent clamps and persists the result.
export function ResizeHandle({
  side,
  width,
  onResize,
  onCommit,
}: {
  side: Side;
  width: number;
  onResize: (width: number) => void;
  onCommit: (width: number) => void;
}) {
  const drag = useRef<{
    x: number;
    width: number;
    frame: number;
    last: number;
  } | null>(null);
  const [min, max] = LIMITS[side];
  // The left panel grows to the right, the right panel grows to the left.
  const sign = side === "left" ? 1 : -1;
  const finish = () => {
    const d = drag.current;
    if (!d) return;
    cancelAnimationFrame(d.frame);
    drag.current = null;
    document.body.classList.remove("resizing");
    onCommit(d.last);
  };
  return (
    <div
      role="separator"
      aria-orientation="vertical"
      aria-label={side === "left" ? "Resize sessions" : "Resize preview"}
      aria-valuenow={width}
      aria-valuemin={min}
      aria-valuemax={max}
      tabIndex={0}
      title="Drag to resize · double-click to reset"
      className={"resize-handle " + side}
      onPointerDown={(e) => {
        if (e.button !== 0) return;
        e.preventDefault();
        e.currentTarget.setPointerCapture(e.pointerId);
        drag.current = { x: e.clientX, width, frame: 0, last: width };
        // Iframes would swallow pointer events while dragging over them.
        document.body.classList.add("resizing");
      }}
      onPointerMove={(e) => {
        const d = drag.current;
        if (!d) return;
        const next = d.width + sign * (e.clientX - d.x);
        cancelAnimationFrame(d.frame);
        d.frame = requestAnimationFrame(() => {
          d.last = next;
          onResize(next);
        });
      }}
      onPointerUp={finish}
      onPointerCancel={finish}
      onLostPointerCapture={finish}
      onDoubleClick={() => onCommit(DEFAULT_WIDTHS[side])}
      onKeyDown={(e) => {
        const step = e.shiftKey ? 64 : 16;
        const keys: Record<string, number> = {
          ArrowLeft: width - sign * step,
          ArrowRight: width + sign * step,
          Home: min,
          End: max,
        };
        if (e.key in keys) {
          e.preventDefault();
          onCommit(keys[e.key]);
        }
      }}
    />
  );
}
