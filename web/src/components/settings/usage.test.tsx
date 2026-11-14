import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { UsageWindow } from "./UsageTab";
import { quotaWindows } from "../../lib/quota";

describe("Usage settings", () => {
  it("lists model-specific and other windows", () => {
    const codex = quotaWindows("codex", {
      rateLimitsByLimitId: {
        codex: { primary: { windowDurationMins: 300, usedPercent: 20 } },
        model: {
          limitName: "Model A",
          primary: { windowDurationMins: 60, usedPercent: 50 },
        },
      },
    });
    expect(codex.map((q) => [q.label, q.scope, q.percent])).toEqual([
      ["5h", undefined, 20],
      ["60m", "Model A", 50],
    ]);
    const claude = quotaWindows("claude", {
      rate_limits: {
        limits: [
          {
            kind: "weekly_scoped",
            percent: 70,
            scope: { model: { display_name: "Model B" } },
          },
        ],
      },
    });
    expect(claude[0]).toMatchObject({
      label: "Weekly",
      scope: "Model B",
      percent: 70,
    });
  });
  it("shows both percentages and the reset without hover", () => {
    const html = renderToStaticMarkup(
      <UsageWindow
        window={{
          kind: "session",
          label: "5h",
          percent: 0,
          resetsAt: Date.now() / 1000 + 7200,
        }}
      />,
    );
    expect(html).toContain("0% used · 100% left");
    expect(html).toContain("Resets");
    expect(html).toContain("(in 2h 0m)");
    expect(
      renderToStaticMarkup(
        <UsageWindow window={{ kind: "session", label: "5h", percent: 100 }} />,
      ),
    ).toContain("100% used · 0% left");
  });
  it("never implies that reaching the reset has restored quota", () => {
    const html = renderToStaticMarkup(
      <UsageWindow
        window={{ kind: "session", label: "5h", percent: 80, resetsAt: 1 }}
      />,
    );
    expect(html).toContain("Awaiting updated usage");
    expect(html).not.toContain("20% left");
    expect(
      renderToStaticMarkup(
        <UsageWindow window={{ kind: "session", label: "5h" }} />,
      ),
    ).toContain("Usage unavailable");
  });
});
