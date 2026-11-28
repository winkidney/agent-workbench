import { describe, expect, it } from "vitest";
import {
  busySessions,
  lastUpdate,
  restartEffect,
  updateUnderWay,
} from "./update";

describe("install and restart", () => {
  it("names the sessions a restart touches", () => {
    const s = (id: string, status: string, attention?: string) => ({
      id,
      status,
      attention,
    });
    expect(
      busySessions([
        s("a", "running"),
        s("b", "idle"),
        s("c", "sleeping", "question"),
        s("d", "waiting"),
        s("e", "sleeping"),
        s("f", "starting"),
      ]).map((x) => x.id),
    ).toEqual(["a", "c", "d", "f"]);
  });
  it("says what a restart does to them", () => {
    expect(restartEffect("systemd")).toContain("keep running");
    expect(restartEffect("direct")).toContain("stops their current turns");
    expect(restartEffect(undefined)).toContain("stops their current turns");
  });
  it("says how the last update went", () => {
    const at = "2026-11-28T00:00:00Z";
    expect(
      lastUpdate(
        { state: "done", from: "v0.7.24", to: "v0.7.25", at },
        "0.7.25",
      ),
    ).toEqual({ text: "Updated from v0.7.24 to v0.7.25." });
    // Already the newest, or not what runs now: nothing to say.
    expect(
      lastUpdate(
        { state: "done", from: "v0.7.25", to: "v0.7.25", at },
        "v0.7.25",
      ),
    ).toBeNull();
    expect(
      lastUpdate(
        { state: "done", from: "v0.7.24", to: "v0.7.25", at },
        "v0.7.26",
      ),
    ).toBeNull();
    expect(
      lastUpdate(
        {
          state: "failed",
          to: "v0.7.25",
          error: "checksum",
          log: "/d/update.log",
          at,
        },
        "v0.7.24",
      ),
    ).toEqual({
      text: "The update to v0.7.25 failed: checksum. Its log: /d/update.log",
      warn: true,
    });
    expect(
      lastUpdate(
        {
          state: "rolled-back",
          from: "v0.7.24",
          to: "v0.7.25",
          error: "exit 1",
          at,
        },
        "v0.7.24",
      )?.text,
    ).toBe("v0.7.25 did not start (exit 1), so v0.7.24 was put back.");
    const going = { state: "verifying", to: "v0.7.25", at } as const;
    expect(lastUpdate(going, "v0.7.24")?.text).toBe(
      "Updating to v0.7.25: checking the download…",
    );
    expect(updateUnderWay(going)).toBe(true);
    expect(updateUnderWay({ state: "done", at })).toBe(false);
    expect(lastUpdate(undefined, "v0.7.24")).toBeNull();
  });
});
