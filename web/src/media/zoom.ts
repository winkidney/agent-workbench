// Pan and zoom for the media viewer. The image is drawn at (x, y) in stage
// pixels with its top-left corner as the transform origin, scaled by s.
export type View = { s: number; x: number; y: number };
export type Size = { w: number; h: number };

export const MIN_SCALE = 0.1;
export const MAX_SCALE = 16;
const clamp = (v: number) => Math.min(MAX_SCALE, Math.max(MIN_SCALE, v));

// Centered at the given scale.
export function centered(stage: Size, image: Size, s: number): View {
  return { s, x: (stage.w - image.w * s) / 2, y: (stage.h - image.h * s) / 2 };
}

// Whole image visible; small images stay at their natural size.
export function fitScale(stage: Size, image: Size): number {
  if (!image.w || !image.h) return 1;
  return clamp(Math.min(stage.w / image.w, stage.h / image.h, 1));
}

export const fit = (stage: Size, image: Size) =>
  centered(stage, image, fitScale(stage, image));

// Zoom by factor keeping the stage point (px, py) over the same image pixel.
export function zoomAt(v: View, factor: number, px: number, py: number): View {
  const s = clamp(v.s * factor);
  const k = s / v.s;
  return { s, x: px - (px - v.x) * k, y: py - (py - v.y) * k };
}

export const pan = (v: View, dx: number, dy: number): View => ({
  ...v,
  x: v.x + dx,
  y: v.y + dy,
});

// Gallery position after moving by d, wrapping around.
export const step = (i: number, d: number, n: number) =>
  n ? (((i + d) % n) + n) % n : 0;
