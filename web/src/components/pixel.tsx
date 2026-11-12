import type { CSSProperties, ReactNode } from "react";

// Pixel art as SVG: grids of characters, one per pixel, drawn as rectangles
// (a row's same-colored neighbours merged into one) on an integer grid with
// crisp edges. Sizes are whole multiples of the grid, so pixels stay sharp.
// Animations are CSS keyframes with steps() (style.css), nothing in JS.

export type Palette = Record<string, string>;

export type Rect = { x: number; y: number; w: number; h: number; fill: string };

// The rectangles of a grid: each row's runs of one palette character, and
// runs that repeat in the rows below merged into taller rectangles.
// Characters missing from the palette ("." and the like) stay transparent.
export function pixelRects(grid: string[], palette: Palette): Rect[] {
  const out: Rect[] = [];
  // The run each column span ended with in the row above.
  let above = new Map<string, Rect>();
  grid.forEach((row, y) => {
    const here = new Map<string, Rect>();
    let x = 0;
    while (x < row.length) {
      const ch = row[x];
      let end = x + 1;
      while (end < row.length && row[end] === ch) end++;
      const fill = palette[ch];
      if (fill) {
        const key = `${x}:${end}:${fill}`;
        const prev = above.get(key);
        if (prev) {
          prev.h++;
          here.set(key, prev);
        } else {
          const r = { x, y, w: end - x, h: 1, fill };
          out.push(r);
          here.set(key, r);
        }
      }
      x = end;
    }
    above = here;
  });
  return out;
}

export function Pixels({
  grid,
  palette,
  x = 0,
  y = 0,
  className,
  style,
}: {
  grid: string[];
  palette: Palette;
  x?: number;
  y?: number;
  className?: string;
  style?: CSSProperties;
}) {
  const rects = pixelRects(grid, palette).map((r, i) => (
    <rect key={i} x={r.x} y={r.y} width={r.w} height={r.h} fill={r.fill} />
  ));
  // Placed by the outer group, animated by the inner one: a CSS transform
  // would replace a transform attribute on the same element.
  return (
    <g transform={x || y ? `translate(${x} ${y})` : undefined}>
      {className || style ? (
        <g className={className} style={style}>
          {rects}
        </g>
      ) : (
        rects
      )}
    </g>
  );
}

// An SVG of w×h grid pixels, each `scale` CSS pixels.
export function PixelSvg({
  w,
  h,
  scale,
  label,
  className,
  children,
}: {
  w: number;
  h: number;
  scale: number;
  // Without a label the picture is decorative.
  label?: string;
  className?: string;
  children: ReactNode;
}) {
  return (
    <svg
      className={"pixel " + (className ?? "")}
      width={w * scale}
      height={h * scale}
      viewBox={`0 0 ${w} ${h}`}
      shapeRendering="crispEdges"
      role={label ? "img" : undefined}
      aria-label={label}
      aria-hidden={label ? undefined : true}
    >
      {label && <title>{label}</title>}
      {children}
    </svg>
  );
}

const fill = { "#": "currentColor" };

// Working: a bright block runs clockwise round a ring of eight, with a
// fading tail. 8×8; size 8 or 16.
const RING = [
  [0, 0],
  [3, 0],
  [6, 0],
  [6, 3],
  [6, 6],
  [3, 6],
  [0, 6],
  [0, 3],
];
// Without motion: the bright block and its tail, held.
const REST = [1, 0.25, 0.25, 0.25, 0.25, 0.25, 0.4, 0.65];
export function PixelChase({
  size = 16,
  label,
}: {
  size?: number;
  label?: string;
}) {
  return (
    <PixelSvg w={8} h={8} scale={size / 8} label={label} className="px-anim">
      {RING.map(([x, y], i) => (
        <Pixels
          key={i}
          grid={["##", "##"]}
          palette={fill}
          x={x}
          y={y}
          className="px-chase"
          style={
            {
              animationDelay: `${(i * 0.1 - 0.8).toFixed(1)}s`,
              "--px-rest": REST[i],
            } as CSSProperties
          }
        />
      ))}
    </PixelSvg>
  );
}

// Connecting: three blocks light in turn.
export function PixelDots({
  size = 16,
  label,
}: {
  size?: number;
  label?: string;
}) {
  return (
    <PixelSvg w={8} h={8} scale={size / 8} label={label} className="px-anim">
      {[0, 3, 6].map((x, i) => (
        <Pixels
          key={x}
          grid={["##", "##"]}
          palette={fill}
          x={x}
          y={3}
          className="px-step"
          style={
            {
              animationDelay: `${(i * 0.3 - 0.9).toFixed(1)}s`,
              "--px-rest": i === 0 ? 1 : 0.3,
            } as CSSProperties
          }
        />
      ))}
    </PixelSvg>
  );
}

export const MONITOR = [
  "########",
  "#......#",
  "#......#",
  "#......#",
  "########",
  "...##...",
  ".######.",
  "........",
];

// Another client has the session: a small screen, its cursor blinking; with
// a turn running there, text types out across it.
export function PixelMonitor({
  size = 16,
  label,
  busy,
}: {
  size?: number;
  label?: string;
  busy?: boolean;
}) {
  return (
    <PixelSvg w={8} h={8} scale={size / 8} label={label} className="px-anim">
      <Pixels grid={MONITOR} palette={fill} />
      {busy ? (
        <Pixels
          grid={["####"]}
          palette={fill}
          x={2}
          y={2}
          className="px-type"
        />
      ) : (
        <Pixels grid={["#"]} palette={fill} x={2} y={2} className="px-blink" />
      )}
    </PixelSvg>
  );
}
