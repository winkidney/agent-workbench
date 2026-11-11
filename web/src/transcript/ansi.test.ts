import { describe, expect, it } from "vitest";
import { parseAnsi, stripAnsi } from "./ansi";

const E = "\u001b";

describe("parseAnsi", () => {
  it("keeps plain text as one unstyled run", () => {
    expect(parseAnsi("plain\ntext")).toEqual([
      { text: "plain\ntext", style: {} },
    ]);
    expect(parseAnsi("")).toEqual([]);
  });

  it("applies and resets SGR attributes", () => {
    const segs = parseAnsi(
      `Call log:\n${E}[2m  - waiting${E}[22m done ${E}[1;31mFAIL${E}[0m ok`,
    );
    expect(segs).toEqual([
      { text: "Call log:\n", style: {} },
      { text: "  - waiting", style: { dim: true } },
      { text: " done ", style: {} },
      { text: "FAIL", style: { bold: true, fg: "var(--ansi-1)" } },
      { text: " ok", style: {} },
    ]);
  });

  it("maps bright, background, 256-color and truecolor", () => {
    const [bright, bg, cube, gray, rgb] = parseAnsi(
      `${E}[92ma${E}[0;41mb${E}[0;38;5;196mc${E}[38;5;244md${E}[38;2;10;20;30me`,
    );
    expect(bright.style.fg).toBe("var(--ansi-10)");
    expect(bg.style.bg).toBe("var(--ansi-1)");
    expect(cube.style.fg).toBe("rgb(255,0,0)");
    expect(gray.style.fg).toBe("rgb(128,128,128)");
    expect(rgb.style.fg).toBe("rgb(10,20,30)");
  });

  it("drops non-color escapes and merges equal runs", () => {
    expect(parseAnsi(`${E}]0;title${E}\\a${E}[2K${E}[1Gb${E}[?25lc`)).toEqual([
      { text: "abc", style: {} },
    ]);
    expect(parseAnsi(`${E}[31mx${E}[31my`)).toEqual([
      { text: "xy", style: { fg: "var(--ansi-1)" } },
    ]);
  });
});

describe("stripAnsi", () => {
  it("removes escapes, keeping the text", () => {
    expect(stripAnsi(`${E}[2m  - waiting${E}[22m`)).toBe("  - waiting");
    expect(stripAnsi("no escapes")).toBe("no escapes");
  });
});
