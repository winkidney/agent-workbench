import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import {
  ALERT,
  BELL,
  BELL_BADGE,
  CHASE,
  CHASE_SMALL,
  DOT,
  DOTS,
  inkBounds,
  MONITOR,
  PixelChase,
  PixelPulse,
  pixelRects,
  PULSE,
} from "./pixel";
import { AgentIcon, CLAUDE_ICON, CODEX_ICON, StatusIcon } from "./AgentIcon";
import {
  BENCH,
  GLYPHS,
  LogoLockup,
  LogoMark,
  CLAWD,
  TILE,
  wordmark,
} from "./Logo";

const square = (grid: string[], n: number) =>
  grid.length === n && grid.every((row) => row.length === n);
const leftRight = (grid: string[]) =>
  grid.every((row) => row === [...row].reverse().join(""));
const mirrored = (grid: string[]) =>
  leftRight(grid) && grid.join() === [...grid].reverse().join();

describe("pixel art", () => {
  it("draws runs as rectangles, merging rows that repeat", () => {
    expect(
      pixelRects(["AAB.", "AAB.", ".A.."], { A: "red", B: "blue" }),
    ).toEqual([
      { x: 0, y: 0, w: 2, h: 2, fill: "red" },
      { x: 2, y: 0, w: 1, h: 2, fill: "blue" },
      { x: 1, y: 2, w: 1, h: 1, fill: "red" },
    ]);
    // Unknown characters stay transparent.
    expect(pixelRects(["?x?"], { x: "red" })).toEqual([
      { x: 1, y: 0, w: 1, h: 1, fill: "red" },
    ]);
  });

  it("keeps every grid whole and the agent marks symmetric", () => {
    expect(square(CLAUDE_ICON, 16) && square(CODEX_ICON, 16)).toBe(true);
    // Claude's figure faces you: left and right match.
    expect(leftRight(CLAUDE_ICON)).toBe(true);
    expect(leftRight(CLAWD)).toBe(true);
    // The tile's outline is symmetric; its prompt is cut out of it.
    expect(mirrored(CODEX_ICON.map((r) => r.replace(/o/g, "X")))).toBe(true);
    expect(CLAWD.every((r) => r.length === 9)).toBe(true);
    expect(TILE.every((r) => r.length === 5)).toBe(true);
    expect(BENCH.every((r) => r.length === 15)).toBe(true);
    for (const [ch, g] of Object.entries(GLYPHS)) {
      expect(g.length, ch).toBe(7);
      expect(new Set(g.map((r) => r.length)).size, ch).toBe(1);
    }
    expect(wordmark("work").map((l) => l.x)).toEqual([0, 6, 11, 16]);
  });

  it("draws the sidebar's icons to one spec", () => {
    const icons = {
      CLAUDE_ICON,
      CODEX_ICON,
      MONITOR,
      CHASE,
      DOTS,
      BELL_BADGE,
      ALERT,
      PULSE,
    };
    for (const [name, grid] of Object.entries(icons)) {
      // 16×16 at one CSS pixel each.
      expect(square(grid, 16), name).toBe(true);
      // Ink 14 wide within the middle 14×14, centred.
      const b = inkBounds(grid);
      expect(b.left, name).toBe(1);
      expect(b.w, name).toBe(14);
      expect(b.top, name).toBeGreaterThanOrEqual(1);
      expect(b.h, name).toBeLessThanOrEqual(14);
      expect(16 - b.top - b.h, name).toBe(b.top);
    }
    // Claude's figure and the monitor fill the same box.
    expect(inkBounds(MONITOR)).toEqual(inkBounds(CLAUDE_ICON));
    // Eight dots of 12 pixels round the ring; the small ring stays 8×8.
    for (const part of "abcdefgh")
      expect(CHASE.join("").split(part).length - 1, part).toBe(12);
    expect(square(CHASE_SMALL, 8)).toBe(true);
    expect(renderToStaticMarkup(<PixelChase size={8} />)).toContain(
      'width="8"',
    );
    // The badge and the warning sign face you; the bell stays on its badge
    // while it swings a pixel either way.
    for (const g of [BELL_BADGE, BELL, ALERT]) expect(leftRight(g)).toBe(true);
    expect(square(BELL, 16)).toBe(true);
    BELL.forEach((row, y) =>
      [...row].forEach((ch, x) => {
        if (ch !== "o") return;
        for (const dx of [-1, 1])
          expect(BELL_BADGE[y][x + dx], `${x + dx},${y}`).toBe("#");
      }),
    );
    // The loaded engine is one of the chase's dots, centred.
    expect(square(DOT, 16)).toBe(true);
    expect(inkBounds(DOT)).toEqual({ top: 6, left: 6, w: 4, h: 4 });
  });

  it("names the agents unless decorative, at sizes of whole pixels", () => {
    const claude = renderToStaticMarkup(<AgentIcon agent="claude" />);
    expect(claude).toContain('aria-label="Claude"');
    expect(claude).toContain('shape-rendering="crispEdges"');
    expect(claude).toContain('width="16"');
    const big = renderToStaticMarkup(
      <AgentIcon agent="codex" size={48} decorative />,
    );
    expect(big).toContain('width="48"');
    expect(big).toContain('aria-hidden="true"');
    expect(big).not.toContain("aria-label");
  });

  it("animates work in progress and keeps its words", () => {
    const html = (status: string) =>
      renderToStaticMarkup(<StatusIcon status={status} />);
    expect(html("running")).toContain("px-chase");
    expect(html("running")).toContain('aria-label="Running"');
    expect(html("starting")).toContain("px-step");
    expect(html("starting")).toContain('aria-label="Connecting"');
    // Idle in another client: the cursor rests.
    expect(html("external")).not.toMatch(/px-(blink|type|chase|step|scan)/);
    expect(html("external")).toContain('aria-label="Active elsewhere"');
    expect(html("external_running")).toContain("px-type");
    expect(html("external_running")).toContain(
      'aria-label="Running in another client"',
    );
    // The rest hold still, in pixels too.
    for (const status of ["waiting", "error", "idle"]) {
      expect(html(status), status).toContain('shape-rendering="crispEdges"');
      expect(html(status), status).not.toContain("lucide");
    }
    expect(html("error")).toContain('aria-label="Error"');
    expect(html("idle")).toContain('aria-label="Engine loaded"');
    // Awaiting approval is a request for you: the bell, until its kind is
    // known.
    expect(html("waiting")).toContain('class="status-icon needs-you"');
    expect(html("waiting")).toContain('aria-label="Awaiting approval"');
    // The bell swings when it arrives (three times, in style.css).
    expect(
      renderToStaticMarkup(<StatusIcon status="idle" attention="plan" />),
    ).toContain("px-swing");
  });

  it("puts the count of background work before its pulse", () => {
    const html = renderToStaticMarkup(
      <StatusIcon status="idle" background={3} />,
    );
    expect(html.indexOf("bg-count")).toBeGreaterThan(-1);
    expect(html.indexOf("bg-count")).toBeLessThan(html.indexOf("<svg"));
    // Still in the sidebar; lit in turn in the composer's tray while live.
    expect(html).not.toContain("px-scan");
    expect(renderToStaticMarkup(<PixelPulse live />)).toContain("px-scan");
  });

  it("names the logo", () => {
    const lockup = renderToStaticMarkup(<LogoLockup />);
    expect(lockup).toContain('aria-label="Agent Workbench"');
    // Mark, gap and "workbench." at two CSS pixels each.
    expect(lockup).toContain('width="132"');
    expect(lockup).toContain('height="18"');
    expect(renderToStaticMarkup(<LogoMark scale={3} />)).toContain(
      'width="48"',
    );
  });
});
