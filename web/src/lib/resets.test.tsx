import { it, expect } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { bankedResets, resetUsable } from "./resets";
import { BankedResets } from "../components/BankedResets";

it("separates missing reset data, zero, and capped details", () => {
  expect(bankedResets({}).count).toBeUndefined();
  expect(
    bankedResets({ rateLimitResetCredits: { availableCount: 0, credits: [] } }),
  ).toMatchObject({ count: 0, partial: false });
  expect(
    bankedResets({
      rateLimitResetCredits: { availableCount: 3, credits: null },
    }),
  ).toMatchObject({ count: 3, partial: true });
});
it("orders expiry and disables expired, unknown or redeemed resets", () => {
  const make = (
    expiresAt: number | null,
    status = "available",
    resetType = "codexRateLimits",
  ) => ({ id: "a", expiresAt, status, resetType });
  expect(resetUsable(make(Date.now() / 1000 + 3600))).toBe(true);
  expect(resetUsable(make(null))).toBe(true);
  expect(resetUsable(make(1))).toBe(false);
  expect(resetUsable(make(null, "redeemed"))).toBe(false);
  expect(resetUsable(make(null, "available", "unknown"))).toBe(false);
  expect(
    bankedResets({
      rateLimitResetCredits: {
        availableCount: 2,
        credits: [make(null), make(1)],
      },
    }).credits[0].expiresAt,
  ).toBe(1);
});
it("links Claude to official Usage without reporting a fake zero", () => {
  const html = renderToStaticMarkup(
    <BankedResets agent="claude" onRefresh={() => {}} />,
  );
  expect(html).toContain("https://claude.ai/settings/usage");
  expect(html).not.toContain("0 resets");
  expect(html).not.toContain("Use reset</");
});
