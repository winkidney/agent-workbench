import { afterEach, describe, expect, it, vi } from "vitest";
import {
  DEFAULT_WIDTHS,
  MIN_CONVERSATION,
  clampWidths,
  limitWidth,
  loadSideOpen,
  loadWidths,
  saveSideOpen,
  saveWidths,
} from "./widths";

const both = { left: true, right: true };

describe("panel widths", () => {
  afterEach(() => vi.unstubAllGlobals());

  it("keeps each panel inside its range", () => {
    expect(limitWidth("left", 50)).toBe(200);
    expect(limitWidth("left", 9999)).toBe(520);
    expect(limitWidth("right", 100)).toBe(280);
    // A preview may take most of a wide screen.
    expect(limitWidth("right", 1200)).toBe(1200);
    expect(limitWidth("right", 9999)).toBe(2400);
  });

  it("leaves room for the conversation column", () => {
    for (const viewport of [1280, 1024]) {
      const w = clampWidths({ left: 520, right: 900 }, viewport, both, "right");
      expect(viewport - w.left - w.right).toBeGreaterThanOrEqual(
        MIN_CONVERSATION,
      );
      expect(w.left).toBeGreaterThanOrEqual(200);
      expect(w.right).toBeGreaterThanOrEqual(280);
    }
    // The dragged panel gives way first.
    expect(clampWidths({ left: 400, right: 600 }, 1280, both, "right")).toEqual(
      { left: 400, right: 460 },
    );
    expect(clampWidths({ left: 400, right: 600 }, 1280, both, "left")).toEqual({
      left: 260,
      right: 600,
    });
    // A closed panel takes no space.
    expect(
      clampWidths(
        { left: 400, right: 900 },
        1280,
        { left: false, right: true },
        "right",
      ),
    ).toEqual({
      left: 400,
      right: 860,
    });
  });

  it("does not modify the saved widths when clamping", () => {
    const saved = { left: 520, right: 900 };
    clampWidths(saved, 900, both);
    expect(saved).toEqual({ left: 520, right: 900 });
  });

  it("survives broken storage", () => {
    vi.stubGlobal("localStorage", {
      getItem: () => {
        throw new Error("blocked");
      },
      setItem: () => {
        throw new Error("blocked");
      },
    });
    expect(loadWidths()).toEqual(DEFAULT_WIDTHS);
    expect(() => saveWidths({ left: 300, right: 400 })).not.toThrow();
  });

  it("round-trips through storage", () => {
    const store = new Map<string, string>();
    vi.stubGlobal("localStorage", {
      getItem: (k: string) => store.get(k) ?? null,
      setItem: (k: string, v: string) => store.set(k, v),
    });
    saveWidths({ left: 333, right: 444 });
    // No preview width yet: half the window.
    expect(loadWidths()).toEqual({
      left: 333,
      right: 444,
      preview: 0,
      side: 0,
    });
    saveWidths({ left: 333, right: 444, preview: 900, side: 520 });
    expect(loadWidths()).toEqual({
      left: 333,
      right: 444,
      preview: 900,
      side: 520,
    });
    // The side chat's width stays inside the right panel's range.
    saveWidths({ left: 333, right: 444, side: 100 });
    expect(loadWidths().side).toBe(280);
  });
});

describe("side chat panels shown", () => {
  afterEach(() => vi.unstubAllGlobals());

  it("round-trips the sessions that show one, the latest few", () => {
    const store = new Map<string, string>();
    vi.stubGlobal("localStorage", {
      getItem: (k: string) => store.get(k) ?? null,
      setItem: (k: string, v: string) => store.set(k, v),
    });
    expect(loadSideOpen()).toEqual({});
    saveSideOpen({ a: true, b: false, c: true });
    expect(loadSideOpen()).toEqual({ a: true, c: true });
    const many = Object.fromEntries(
      Array.from({ length: 30 }, (_, i) => ["s" + i, true]),
    );
    saveSideOpen(many);
    const kept = Object.keys(loadSideOpen());
    expect(kept).toHaveLength(20);
    expect(kept[0]).toBe("s10");
  });

  it("survives broken storage", () => {
    vi.stubGlobal("localStorage", {
      getItem: () => "not json",
      setItem: () => {
        throw new Error("blocked");
      },
    });
    expect(loadSideOpen()).toEqual({});
    expect(() => saveSideOpen({ a: true })).not.toThrow();
  });
});
