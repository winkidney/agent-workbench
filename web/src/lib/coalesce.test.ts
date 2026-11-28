import { describe, expect, it } from "vitest";
import { coalesced } from "./coalesce";

const tick = () => new Promise((r) => setTimeout(r, 0));

describe("coalesced", () => {
  it("runs once more after a run when asked meanwhile, however often", async () => {
    const done: (() => void)[] = [];
    let calls = 0;
    const run = coalesced(() => {
      calls++;
      return new Promise<void>((r) => done.push(r));
    });
    run();
    run();
    run();
    run();
    expect(calls).toBe(1);
    done[0]();
    await tick();
    // The calls during the first make one more, not three, and none is lost.
    expect(calls).toBe(2);
    done[1]();
    await tick();
    expect(calls).toBe(2);
    run();
    expect(calls).toBe(3);
  });
  it("goes on after a failed run", async () => {
    let calls = 0;
    const run = coalesced(() => {
      calls++;
      return Promise.reject(new Error("offline"));
    });
    run();
    await tick();
    run();
    expect(calls).toBe(2);
  });
});
