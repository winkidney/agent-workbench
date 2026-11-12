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

// The sidebar's icons share one spec (AgentIcon.tsx too): a 16×16 grid at
// one CSS pixel each, ink within the middle 14×14, 14 wide and centred
// vertically, strokes and blocks 2 pixels thick.

// The box the ink of a grid fills: characters that are not ".".
export function inkBounds(grid: string[]) {
  let top = Infinity;
  let left = Infinity;
  let bottom = -1;
  let right = -1;
  grid.forEach((row, y) =>
    [...row].forEach((ch, x) => {
      if (ch === ".") return;
      top = Math.min(top, y);
      bottom = Math.max(bottom, y);
      left = Math.min(left, x);
      right = Math.max(right, x);
    }),
  );
  return { top, left, w: right - left + 1, h: bottom - top + 1 };
}

// One part of a grid whose parts are lettered: only that letter is drawn.
function Part({
  grid,
  part,
  className,
  style,
}: {
  grid: string[];
  part: string;
  className: string;
  style: CSSProperties;
}) {
  return (
    <Pixels
      grid={grid}
      palette={{ [part]: "currentColor" }}
      className={className}
      style={style}
    />
  );
}

// Working: a bright dot runs clockwise round a ring of eight (a to h), with
// a fading tail. 14×14 dots of 4 with corners cut, so the 1-pixel gaps
// between them still show.
export const CHASE = [
  "................",
  "..aa...bb...cc..",
  ".aaaa.bbbb.cccc.",
  ".aaaa.bbbb.cccc.",
  "..aa...bb...cc..",
  "................",
  "..hh........dd..",
  ".hhhh......dddd.",
  ".hhhh......dddd.",
  "..hh........dd..",
  "................",
  "..gg...ff...ee..",
  ".gggg.ffff.eeee.",
  ".gggg.ffff.eeee.",
  "..gg...ff...ee..",
  "................",
];
// The same ring at 8×8, for a tool's line in the transcript.
export const CHASE_SMALL = [
  "aa.bb.cc",
  "aa.bb.cc",
  "........",
  "hh....dd",
  "hh....dd",
  "........",
  "gg.ff.ee",
  "gg.ff.ee",
];
const RING = "abcdefgh";
// Without motion: the bright dot and its tail, held.
const REST = [1, 0.25, 0.25, 0.25, 0.25, 0.25, 0.4, 0.65];
export function PixelChase({
  size = 16,
  label,
}: {
  size?: number;
  label?: string;
}) {
  // Below 16 the small ring keeps whole pixels.
  const grid = size < 16 ? CHASE_SMALL : CHASE;
  const n = grid.length;
  return (
    <PixelSvg w={n} h={n} scale={size / n} label={label} className="px-anim">
      {[...RING].map((part, i) => (
        <Part
          key={part}
          grid={grid}
          part={part}
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

// Connecting: three dots, the chase's, light in turn.
export const DOTS = [
  "................",
  "................",
  "................",
  "................",
  "................",
  "................",
  "..aa...bb...cc..",
  ".aaaa.bbbb.cccc.",
  ".aaaa.bbbb.cccc.",
  "..aa...bb...cc..",
  "................",
  "................",
  "................",
  "................",
  "................",
  "................",
];
export function PixelDots({
  size = 16,
  label,
}: {
  size?: number;
  label?: string;
}) {
  return (
    <PixelSvg w={16} h={16} scale={size / 16} label={label} className="px-anim">
      {[..."abc"].map((part, i) => (
        <Part
          key={part}
          grid={DOTS}
          part={part}
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

// A small screen on a stand: 14×10, the size of Claude's figure.
export const MONITOR = [
  "................",
  "................",
  "................",
  ".##############.",
  ".##############.",
  ".##..........##.",
  ".##..........##.",
  ".##..........##.",
  ".##..........##.",
  ".##############.",
  ".##############.",
  ".......##.......",
  "....########....",
  "................",
  "................",
  "................",
];
// Where the cursor (2×2) and a line of text (8×2) sit on the screen.
export const CURSOR_AT = [4, 6];

// Another client has the session: its cursor blinks; with a turn running
// there, text types out across the screen.
export function PixelMonitor({
  size = 16,
  label,
  busy,
}: {
  size?: number;
  label?: string;
  busy?: boolean;
}) {
  const [x, y] = CURSOR_AT;
  return (
    <PixelSvg w={16} h={16} scale={size / 16} label={label} className="px-anim">
      <Pixels grid={MONITOR} palette={fill} />
      {busy ? (
        <Pixels
          grid={["########", "########"]}
          palette={fill}
          x={x}
          y={y}
          className="px-type"
        />
      ) : (
        <Pixels
          grid={["##", "##"]}
          palette={fill}
          x={x}
          y={y}
          className="px-blink"
        />
      )}
    </PixelSvg>
  );
}
