// Side panel widths: user-adjustable, saved per browser, and clamped so the
// conversation column never gets narrower than MIN_CONVERSATION.

export type Side = "left" | "right";
// The right panel has its own width while it shows a preview (0 is half
// the window), and another while it shows the side chat (0 is DEFAULT_SIDE).
export type Widths = Record<Side, number> & { preview?: number; side?: number };

export const DEFAULT_WIDTHS: Widths = { left: 288, right: 360 };
export const DEFAULT_SIDE = 480;
export const LIMITS: Record<Side, [number, number]> = {
  left: [200, 520],
  right: [280, 2400],
};
export const MIN_CONVERSATION = 420;
const KEY = "workbench-widths";

const within = (v: number, [lo, hi]: [number, number]) =>
  Math.min(hi, Math.max(lo, Math.round(v)));

export const limitWidth = (side: Side, v: number) => within(v, LIMITS[side]);

// Fit the open panels into the viewport. The panel being dragged gives way
// first, then the other one, but never below its minimum.
export function clampWidths(
  w: Widths,
  viewport: number,
  open: Record<Side, boolean>,
  dragging: Side = "right",
): Widths {
  const out: Widths = {
    left: limitWidth("left", w.left),
    right: limitWidth("right", w.right),
  };
  let overflow =
    (open.left ? out.left : 0) +
    (open.right ? out.right : 0) +
    MIN_CONVERSATION -
    viewport;
  for (const side of [
    dragging,
    dragging === "left" ? "right" : "left",
  ] as Side[]) {
    if (overflow <= 0 || !open[side]) continue;
    const give = Math.min(overflow, out[side] - LIMITS[side][0]);
    out[side] -= give;
    overflow -= give;
  }
  return out;
}

export function loadWidths(): Widths {
  try {
    const v = JSON.parse(localStorage.getItem(KEY) || "{}");
    return {
      left: limitWidth("left", Number(v.left) || DEFAULT_WIDTHS.left),
      right: limitWidth("right", Number(v.right) || DEFAULT_WIDTHS.right),
      preview: Number(v.preview) ? limitWidth("right", Number(v.preview)) : 0,
      side: Number(v.side) ? limitWidth("right", Number(v.side)) : 0,
    };
  } catch {
    return { ...DEFAULT_WIDTHS };
  }
}

export function saveWidths(w: Widths) {
  try {
    localStorage.setItem(KEY, JSON.stringify(w));
  } catch {}
}

// The sessions whose side chat panel is shown, so a reload shows it again.
// Only the latest few are kept.
const SIDE_KEY = "workbench-side-open";
const SIDE_KEPT = 20;

export function loadSideOpen(): Record<string, boolean> {
  try {
    const v: unknown = JSON.parse(localStorage.getItem(SIDE_KEY) || "[]");
    return Array.isArray(v)
      ? Object.fromEntries(
          v.filter((x) => typeof x === "string").map((x) => [x, true]),
        )
      : {};
  } catch {
    return {};
  }
}

export function saveSideOpen(open: Record<string, boolean>) {
  try {
    const ids = Object.keys(open).filter((id) => open[id]);
    localStorage.setItem(SIDE_KEY, JSON.stringify(ids.slice(-SIDE_KEPT)));
  } catch {}
}
