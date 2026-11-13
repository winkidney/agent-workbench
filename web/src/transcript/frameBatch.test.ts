import { describe, expect, it } from "vitest";
import { frameBatch } from "./frameBatch";

// Frames run only when the test says.
function frames() {
  const waiting = new Map<number, () => void>();
  let next = 1;
  return {
    raf: (cb: () => void) => {
      waiting.set(next, cb);
      return next++;
    },
    cancel: (id: number) => void waiting.delete(id),
    run: () => {
      const all = [...waiting.values()];
      waiting.clear();
      all.forEach((cb) => cb());
    },
    get count() {
      return waiting.size;
    },
  };
}

describe("live events by frame", () => {
  it("runs what came in one frame together", () => {
    const f = frames();
    const runs: number[][] = [];
    const b = frameBatch<number>((xs) => runs.push(xs), f.raf, f.cancel);
    b.push(1);
    b.push(2);
    expect(f.count).toBe(1);
    f.run();
    expect(runs).toEqual([[1, 2]]);
    b.push(3);
    expect(f.count).toBe(1);
    f.run();
    expect(runs).toEqual([[1, 2], [3]]);
  });

  it("keeps working after a reset while a frame was pending", () => {
    const f = frames();
    const runs: number[][] = [];
    const b = frameBatch<number>((xs) => runs.push(xs), f.raf, f.cancel);
    // A session switch while another session streams (or a hidden tab).
    b.push(1);
    b.reset();
    expect(f.count).toBe(0);
    expect(b.pending).toBe(0);
    // The next session's events are applied.
    b.push(2);
    expect(f.count).toBe(1);
    f.run();
    expect(runs).toEqual([[2]]);
  });

  it("applies what waited at once when asked", () => {
    const f = frames();
    const runs: number[][] = [];
    const b = frameBatch<number>((xs) => runs.push(xs), f.raf, f.cancel);
    b.push(1);
    b.flush();
    expect(runs).toEqual([[1]]);
    expect(f.count).toBe(0);
    b.flush();
    expect(runs).toEqual([[1]]);
    b.push(2);
    f.run();
    expect(runs).toEqual([[1], [2]]);
  });
});
