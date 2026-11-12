import { describe, expect, it } from "vitest";
import { goalDone, goalIntent, goalLabel, goalMeta, type Goal } from "./goal";

const goal = (status: string, more: Partial<Goal> = {}): Goal => ({
  objective: "ship the release",
  status,
  updated: "2026-11-12T10:00:00Z",
  ...more,
});

describe("goal", () => {
  it("uses each CLI's words for the status", () => {
    expect(goalLabel("codex", "active")).toBe("Pursuing goal");
    expect(goalLabel("claude", "active")).toBe("Goal active");
    expect(goalLabel("codex", "blocked")).toBe("Goal stalled");
    expect(goalLabel("codex", "complete")).toBe("Goal achieved");
    expect(goalLabel("claude", "met")).toBe("Goal met");
    expect(goalDone("complete") && goalDone("met") && goalDone("failed")).toBe(
      true,
    );
    expect(goalDone("paused")).toBe(false);
  });

  it("summarizes time, checks and tokens", () => {
    expect(
      goalMeta(
        goal("active", { seconds: 75, tokensUsed: 1200, tokenBudget: 50000 }),
      ),
    ).toBe("1m 15s · 1.2k / 50k tokens");
    expect(goalMeta(goal("active", { checks: 2 }))).toBe("2 checks");
    expect(goalMeta(goal("active"))).toBe("");
  });

  it("prefills /goal edit and asks before replacing an unfinished goal", () => {
    expect(goalIntent("codex", "edit", goal("active"))).toEqual({
      kind: "prefill",
      text: "/goal edit ship the release",
    });
    expect(goalIntent("codex", "edit", null)).toBeNull();
    expect(goalIntent("codex", "write the docs", goal("paused"))).toEqual({
      kind: "confirm",
      args: "write the docs",
    });
    // Finished goals, subcommands and Claude go straight through.
    expect(goalIntent("codex", "write the docs", goal("complete"))).toBeNull();
    expect(goalIntent("codex", "write the docs", null)).toBeNull();
    for (const args of ["", "pause", "Resume", "clear", "edit new text"])
      expect(goalIntent("codex", args, goal("active"))).toBeNull();
    expect(goalIntent("claude", "tests pass", goal("active"))).toBeNull();
  });
});
