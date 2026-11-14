import { describe, expect, it } from "vitest";
import { quotaSummary, resetTime } from "./quota";
const now = Date.parse("2026-11-14T00:00:00Z");
const reset = (now + 1570 * 60000) / 1000;
const window = (minutes: number, used = 18) => ({
  windowDurationMins: minutes,
  usedPercent: used,
  resetsAt: reset,
});
const claude = (
  percent: unknown,
  resets_at: unknown = new Date(reset * 1000).toISOString(),
) => ({
  rate_limits: {
    limits: [
      { kind: "session", percent, resets_at },
      { kind: "weekly_all", percent: 84, resets_at },
    ],
  },
});

describe("quota summaries", () => {
  it("shows Codex 5h and weekly account windows, excluding model-specific pools", () => {
    const result = quotaSummary(
      "codex",
      {
        rateLimitsByLimitId: {
          model: { primary: window(300, 99) },
          codex: { primary: window(300), secondary: window(10080, 84) },
        },
      },
      now,
    );
    expect(result.map(({ label, left }) => ({ label, left }))).toEqual([
      { label: "5h", left: 82 },
      { label: "Weekly", left: 16 },
    ]);
    expect(result.every((q) => q.reset?.endsWith("(in 1d 2h 10m)"))).toBe(true);
  });
  it("supports the legacy Codex response and weekly-only accounts", () => {
    expect(
      quotaSummary(
        "codex",
        { rateLimits: { primary: window(10080) } },
        now,
      ).map((q) => q.label),
    ).toEqual(["Weekly"]);
  });
  it("recognizes Claude session and weekly_all, not model-scoped weekly limits", () => {
    const usage = claude(18);
    usage.rate_limits.limits.unshift({
      kind: "weekly_scoped",
      percent: 99,
      resets_at: new Date(reset * 1000).toISOString(),
    });
    expect(quotaSummary("claude", usage, now).map((q) => q.left)).toEqual([
      82, 16,
    ]);
  });
  it("hides absent, invalid and expired windows", () => {
    expect(quotaSummary("codex", undefined, now)).toEqual([]);
    expect(quotaSummary("claude", {}, now)).toEqual([]);
    for (const percent of [undefined, null, NaN, Infinity, "10"])
      expect(
        quotaSummary("claude", claude(percent), now).map((q) => q.label),
      ).toEqual(["Weekly"]);
    for (const date of ["invalid", now / 1000, now / 1000 - 1])
      expect(quotaSummary("claude", claude(18, date), now)).toEqual([]);
  });
  it("keeps 100% and 0% remaining and bounds values", () => {
    for (const [used, left] of [
      [0, 100],
      [100, 0],
      [-5, 100],
      [110, 0],
    ])
      expect(quotaSummary("claude", claude(used), now)[0].left).toBe(left);
  });
  it("retains the account fallback when other pools are reported", () => {
    expect(
      quotaSummary(
        "codex",
        { rateLimitsByLimitId: {}, rateLimits: { primary: window(300) } },
        now,
      )[0].left,
    ).toBe(82);
  });
  it("does not invent reset times", () => {
    const q = quotaSummary("claude", claude(18, null), now)[0];
    expect(q.left).toBe(82);
    expect(q.reset).toBeUndefined();
    expect(resetTime("invalid", now)).toBeUndefined();
    expect(resetTime(reset, now)).toBe(
      resetTime(new Date(reset * 1000).toISOString(), now),
    );
    expect(resetTime(now / 1000 - 60, now)).toContain("(in 0h 0m)");
  });
});
