import { afterEach, describe, expect, it, vi } from "vitest";
import {
  DEFAULT_WIDTHS,
  MIN_CONVERSATION,
  clampWidths,
  limitWidth,
  loadWidths,
  saveWidths,
} from "./widths";

const both = { left: true, right: true };

describe("panel widths", () => {
  afterEach(() => vi.unstubAllGlobals());

  it("keeps each panel inside its range", () => {
    expect(limitWidth("left", 50)).toBe(200);
    expect(limitWidth("left", 9999)).toBe(520);
    expect(limitWidth("right", 100)).toBe(280);
    expect(limitWidth("right", 1200)).toBe(900);
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
    expect(loadWidths()).toEqual({ left: 333, right: 444 });
  });
});
