import { describe, expect, it } from "vitest";
import { MAX_SCALE, MIN_SCALE, fit, fitScale, pan, step, zoomAt } from "./zoom";

describe("viewer zoom", () => {
  const stage = { w: 1000, h: 800 };

  it("fits large images and keeps small ones at natural size", () => {
    expect(fitScale(stage, { w: 4000, h: 2000 })).toBe(0.25);
    expect(fitScale(stage, { w: 200, h: 100 })).toBe(1);
    expect(fit(stage, { w: 200, h: 100 })).toEqual({ s: 1, x: 400, y: 350 });
  });

  it("keeps the point under the cursor fixed while zooming", () => {
    const v = { s: 0.5, x: 100, y: 50 };
    const [px, py] = [300, 250];
    // Image pixel under the cursor before and after.
    const before = [(px - v.x) / v.s, (py - v.y) / v.s];
    const z = zoomAt(v, 2, px, py);
    expect(z.s).toBe(1);
    expect([(px - z.x) / z.s, (py - z.y) / z.s]).toEqual(before);
  });

  it("clamps the scale and pans by the drag distance", () => {
    expect(zoomAt({ s: 10, x: 0, y: 0 }, 4, 0, 0).s).toBe(MAX_SCALE);
    expect(zoomAt({ s: 0.2, x: 0, y: 0 }, 0.1, 0, 0).s).toBe(MIN_SCALE);
    expect(pan({ s: 2, x: 5, y: 5 }, -10, 20)).toEqual({ s: 2, x: -5, y: 25 });
  });

  it("wraps around the gallery", () => {
    expect(step(0, -1, 5)).toBe(4);
    expect(step(4, 1, 5)).toBe(0);
    expect(step(2, 1, 5)).toBe(3);
    expect(step(0, 1, 0)).toBe(0);
  });
});
