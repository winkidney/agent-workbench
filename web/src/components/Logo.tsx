import { Pixels, PixelSvg } from "./pixel";

// The workbench's mark: two agents on a bench. Claude's coral spark (C) and
// Codex's slate tile (X, with a prompt and cursor cut out) sit on the bench
// (#). 13×9 pixels.
export const SPARK = [".C.C.C", "..CCC.", ".CCCCC", "..CCC.", ".C.C.C"];
export const TILE = ["XXXXX", "XoXXX", "XXoXX", "XoXoX", "XXXXX"];
export const BENCH = ["#############", ".#.........#.", ".#.........#."];
export const MARK_W = 13;
export const MARK_H = 9;

const COLORS = {
  C: "var(--agent-claude)",
  X: "var(--agent-codex)",
  "#": "currentColor",
};

// The wordmark's lowercase pixel font: 7 rows, ascenders in the first two.
export const GLYPHS: Record<string, string[]> = {
  w: [".....", ".....", "#...#", "#...#", "#.#.#", "#.#.#", ".#.#."],
  o: ["....", "....", ".##.", "#..#", "#..#", "#..#", ".##."],
  r: ["....", "....", "#.##", "##..", "#...", "#...", "#..."],
  k: ["#...", "#...", "#..#", "#.#.", "##..", "#.#.", "#..#"],
  b: ["#...", "#...", "###.", "#..#", "#..#", "#..#", "###."],
  e: ["....", "....", ".##.", "#..#", "####", "#...", ".###"],
  n: ["....", "....", "###.", "#..#", "#..#", "#..#", "#..#"],
  c: ["....", "....", ".###", "#...", "#...", "#...", ".###"],
  h: ["#...", "#...", "###.", "#..#", "#..#", "#..#", "#..#"],
};

// The letters of a word laid out with one pixel between them, and where
// each begins.
export function wordmark(word: string): { x: number; glyph: string[] }[] {
  let x = 0;
  return [...word].map((ch) => {
    const glyph = GLYPHS[ch];
    const at = { x, glyph };
    x += glyph[0].length + 1;
    return at;
  });
}

// The mark's parts; the spark and the tile hop (and the cursor blinks)
// while the pointer is on the logo.
function MarkParts({ x = 0, y = 0 }: { x?: number; y?: number }) {
  return (
    <>
      <Pixels grid={SPARK} palette={COLORS} x={x} y={y} className="px-hop" />
      <g transform={`translate(${x + 7} ${y})`}>
        <g className="px-hop px-hop-late">
          <Pixels grid={TILE} palette={COLORS} />
          <Pixels
            grid={["X"]}
            palette={COLORS}
            x={3}
            y={3}
            className="px-cursor"
          />
        </g>
      </g>
      <Pixels grid={BENCH} palette={COLORS} x={x} y={y + 6} />
    </>
  );
}

// Mark and wordmark, "workbench." with a coral full stop: the sidebar's
// header. 2 CSS pixels per pixel.
export function LogoLockup({ scale = 2 }: { scale?: number }) {
  const letters = wordmark("workbench");
  const last = letters[letters.length - 1];
  const textX = MARK_W + 3;
  const width = textX + last.x + last.glyph[0].length + 1 + 2;
  return (
    <PixelSvg
      w={width}
      h={MARK_H}
      scale={scale}
      label="Agent Workbench"
      className="logo"
    >
      <MarkParts />
      {letters.map((l, i) => (
        <Pixels
          key={i}
          grid={l.glyph}
          palette={{ "#": "currentColor" }}
          x={textX + l.x}
          y={2}
        />
      ))}
      <Pixels
        grid={["CC", "CC"]}
        palette={COLORS}
        x={width - 2}
        y={MARK_H - 2}
      />
    </PixelSvg>
  );
}

// The mark alone, in a square (16×16 at scale 1): the sign-in page.
export function LogoMark({
  scale = 2,
  label = "Agent Workbench",
}: {
  scale?: number;
  label?: string;
}) {
  return (
    <PixelSvg w={16} h={16} scale={scale} label={label} className="logo">
      <MarkParts x={1} y={3} />
    </PixelSvg>
  );
}
