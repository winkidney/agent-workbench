import type { Size, View } from "./zoom";

// Marks drawn over an image in the viewer: boxes, arrows, freehand strokes
// and text,
// in the image's own pixels so they follow zoom and pan, and export at the
// image's full size. They live only while the viewer shows that image.

export type Tool = "move" | "rect" | "arrow" | "pen" | "text";

export type Mark =
  | {
      kind: "rect";
      x: number;
      y: number;
      w: number;
      h: number;
      color: string;
      width: number;
    }
  // From its tail (x1, y1) to the point it shows (x2, y2).
  | {
      kind: "arrow";
      x1: number;
      y1: number;
      x2: number;
      y2: number;
      color: string;
      width: number;
    }
  // points: x0, y0, x1, y1, …
  | { kind: "pen"; points: number[]; color: string; width: number }
  | {
      kind: "text";
      x: number;
      y: number;
      text: string;
      color: string;
      size: number;
    };

// Plain colors that read on screenshots, light or dark.
export const COLORS = [
  { name: "Red", value: "#ef4444" },
  { name: "Yellow", value: "#facc15" },
  { name: "Green", value: "#22c55e" },
  { name: "Blue", value: "#3b82f6" },
  { name: "White", value: "#ffffff" },
  { name: "Black", value: "#111111" },
];

// Undo and redo: what is drawn, and what was undone (newest last). Drawing
// anything new drops what was undone.
export type History = { done: Mark[]; undone: Mark[] };
export const EMPTY: History = { done: [], undone: [] };

export const addMark = (h: History, m: Mark): History => ({
  done: [...h.done, m],
  undone: [],
});

export function undo(h: History): History {
  const last = h.done.at(-1);
  return last ? { done: h.done.slice(0, -1), undone: [...h.undone, last] } : h;
}

export function redo(h: History): History {
  const next = h.undone.at(-1);
  return next ? { done: [...h.done, next], undone: h.undone.slice(0, -1) } : h;
}

// The image pixel under a stage point.
export const toImage = (v: View, px: number, py: number) => ({
  x: (px - v.x) / v.s,
  y: (py - v.y) / v.s,
});

// Line and text sizes that read at the image's own scale.
export const strokeWidth = (n: Size) =>
  Math.max(2, Math.round(Math.max(n.w, n.h) / 320));
export const textSize = (n: Size) =>
  Math.max(14, Math.round(Math.max(n.w, n.h) / 36));

// A box from two corners, whichever way it was dragged.
export function box(ax: number, ay: number, bx: number, by: number) {
  return {
    x: Math.min(ax, bx),
    y: Math.min(ay, by),
    w: Math.abs(bx - ax),
    h: Math.abs(by - ay),
  };
}

export function penPath(points: number[]): string {
  // A dot: a stroke of one point still shows.
  if (points.length === 2)
    return `M${points[0]} ${points[1]} L${points[0]} ${points[1] + 0.01}`;
  let d = "";
  for (let i = 0; i + 1 < points.length; i += 2)
    d += (i ? " L" : "M") + points[i] + " " + points[i + 1];
  return d;
}

// The outline that keeps text readable on any background.
export const halo = (color: string) =>
  ["#ffffff", "#facc15"].includes(color) ? "#000000" : "#ffffff";
export const FONT = "600 {size}px system-ui, -apple-system, sans-serif";

// Whether the page may read the image's pixels: images from other sites
// taint the canvas, so they cannot be marked up or copied.
export function foreign(src: string, origin = location.origin): boolean {
  if (/^(data|blob):/i.test(src)) return false;
  try {
    return new URL(src, origin).origin !== origin;
  } catch {
    return true;
  }
}

// An arrow's head: its tip at (x2, y2) and two back corners, sized by the
// line's width; and where the line stops, so it does not show through the
// tip.
export function arrowHead(
  x1: number,
  y1: number,
  x2: number,
  y2: number,
  width: number,
) {
  const length = Math.hypot(x2 - x1, y2 - y1) || 1;
  const ux = (x2 - x1) / length;
  const uy = (y2 - y1) / length;
  // Long enough to see, never longer than the arrow itself.
  const head = Math.min(Math.max(width * 4.5, 10), length);
  const half = head * 0.55;
  const bx = x2 - ux * head;
  const by = y2 - uy * head;
  return {
    points: [
      x2,
      y2,
      bx - uy * half,
      by + ux * half,
      bx + uy * half,
      by - ux * half,
    ],
    lineEnd: { x: x2 - ux * head * 0.6, y: y2 - uy * head * 0.6 },
  };
}

export function drawMarks(ctx: CanvasRenderingContext2D, marks: Mark[]) {
  ctx.lineCap = "round";
  ctx.lineJoin = "round";
  for (const m of marks) {
    if (m.kind === "text") {
      ctx.font = FONT.replace("{size}", String(m.size));
      ctx.textBaseline = "top";
      ctx.lineWidth = Math.max(2, m.size / 6);
      ctx.strokeStyle = halo(m.color);
      ctx.strokeText(m.text, m.x, m.y);
      ctx.fillStyle = m.color;
      ctx.fillText(m.text, m.x, m.y);
      continue;
    }
    ctx.strokeStyle = m.color;
    ctx.lineWidth = m.width;
    if (m.kind === "arrow") {
      const { points: p, lineEnd } = arrowHead(m.x1, m.y1, m.x2, m.y2, m.width);
      ctx.beginPath();
      ctx.moveTo(m.x1, m.y1);
      ctx.lineTo(lineEnd.x, lineEnd.y);
      ctx.stroke();
      ctx.fillStyle = m.color;
      ctx.beginPath();
      ctx.moveTo(p[0], p[1]);
      ctx.lineTo(p[2], p[3]);
      ctx.lineTo(p[4], p[5]);
      ctx.closePath();
      ctx.fill();
      continue;
    }
    ctx.beginPath();
    if (m.kind === "rect") ctx.rect(m.x, m.y, m.w, m.h);
    else {
      ctx.moveTo(m.points[0], m.points[1]);
      for (let i = 2; i + 1 < m.points.length; i += 2)
        ctx.lineTo(m.points[i], m.points[i + 1]);
      // A dot: a stroke of one point still shows.
      if (m.points.length === 2) ctx.lineTo(m.points[0], m.points[1] + 0.01);
    }
    ctx.stroke();
  }
}

// The image with its marks as a PNG at the image's own size. The canvas is
// let go at once.
export async function exportPng(
  image: CanvasImageSource,
  size: Size,
  marks: Mark[],
): Promise<Blob> {
  const canvas = document.createElement("canvas");
  try {
    canvas.width = size.w;
    canvas.height = size.h;
    const ctx = canvas.getContext("2d");
    if (!ctx) throw new Error("This browser cannot draw the image");
    ctx.drawImage(image, 0, 0, size.w, size.h);
    drawMarks(ctx, marks);
    return await new Promise<Blob>((resolve, reject) =>
      canvas.toBlob(
        (b) =>
          b ? resolve(b) : reject(new Error("The image could not be saved")),
        "image/png",
      ),
    );
  } finally {
    canvas.width = canvas.height = 0;
  }
}

// The file name of the exported image.
export function exportName(title: string, marked: boolean): string {
  const name = (title.split("/").pop() || "image").replace(/\.[^.]*$/, "");
  const base = name && name !== "Image" ? name : "image";
  return `${base}${marked ? "-marked" : ""}.png`;
}
