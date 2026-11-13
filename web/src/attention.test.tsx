import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import {
  needsYou,
  newlyWaiting,
  notificationFor,
  titleWith,
} from "./attention";
import { StatusIcon } from "./components/AgentIcon";
import { buildSidebar, emptyPrefs, type Session } from "./sidebar/tree";
import { applyEvents, emptyState } from "./transcript/reducer";
import { reviewTarget } from "./transcript/codex";
import { optionName } from "./agentOptions";

const w = (id: string, attention?: string) => ({
  id,
  agent: id.startsWith("codex") ? "codex" : "claude",
  title: "Session " + id,
  attention,
});

describe("sessions that need you", () => {
  it("counts them in the tab title", () => {
    expect(titleWith("workbench.", 0)).toBe("workbench.");
    expect(titleWith("workbench.", 2)).toBe("(2) workbench.");
    expect(needsYou({ attention: "approval" })).toBe(true);
    expect(needsYou({ attention: "" })).toBe(false);
    expect(needsYou({ attention: "something-new" })).toBe(false);
  });

  it("announces only what newly waits", () => {
    const first = [w("codex:a", "approval"), w("claude:b")];
    // The first list a page sees is not news.
    expect(newlyWaiting(null, first)).toEqual([]);
    const known = new Map([["codex:a", "approval"]]);
    expect(newlyWaiting(known, first)).toEqual([]);
    const next = [w("codex:a", "approval"), w("claude:b", "question")];
    expect(newlyWaiting(known, next).map((s) => s.id)).toEqual(["claude:b"]);
    // A different request in the same session is news; a cleared one is not.
    expect(
      newlyWaiting(known, [w("codex:a", "plan")]).map((s) => s.attention),
    ).toEqual(["plan"]);
    expect(newlyWaiting(known, [w("codex:a")])).toEqual([]);
  });

  it("writes the notification in the agent's name", () => {
    expect(notificationFor(w("codex:a", "approval"))).toEqual({
      title: "Codex needs your approval",
      body: "Session codex:a",
    });
    expect(notificationFor(w("claude:b", "question")).title).toBe(
      "Claude has a question for you",
    );
  });

  it("shows a badge instead of the working state", () => {
    const html = renderToStaticMarkup(
      <StatusIcon status="running" attention="question" />,
    );
    // The kind is not a class: .question and .approval style the cards.
    expect(html).toContain('class="status-icon needs-you"');
    expect(html).toContain('aria-label="Has a question for you"');
    expect(renderToStaticMarkup(<StatusIcon status="running" />)).toContain(
      "px-chase",
    );
  });

  it("lists them first under Active, asleep or not", () => {
    const s = (id: string, extra: Partial<Session>): Session => ({
      id,
      agent: "codex",
      cwd: "/w",
      workspace: "/w",
      title: id,
      nativeId: id.split(":")[1],
      status: "sleeping",
      updated: "2026-11-12T10:00:00Z",
      ...extra,
    });
    const m = buildSidebar(
      [
        s("codex:busy", { status: "running", updated: "2026-11-12T11:00:00Z" }),
        s("codex:asked", { status: "sleeping", attention: "question" }),
        s("codex:quiet", {}),
      ],
      emptyPrefs(),
    );
    expect(m.active.map((n) => n.session.id)).toEqual([
      "codex:asked",
      "codex:busy",
    ]);
  });
});

describe("Codex auto-review", () => {
  const native = (data: Record<string, unknown>) => ({
    seq: 0,
    session: "s",
    kind: "native",
    data,
    time: "",
  });
  const run = (...data: Record<string, unknown>[]) =>
    [
      ...applyEvents(emptyState(), data.map(native), {
        agent: "codex",
        history: false,
      }).blocks.values(),
    ].filter((b) => b.kind === "notice");

  it("shows each decision once, quietly when it approved", () => {
    const notices = run(
      {
        method: "item/autoApprovalReview/completed",
        params: {
          reviewId: "r1",
          review: {
            status: "approved",
            riskLevel: "low",
            rationale: "The user asked for this request.",
          },
          action: {
            type: "command",
            command: '/usr/bin/zsh -lc "curl -s https://example.com"',
          },
        },
      },
      {
        method: "guardianWarning",
        params: {
          message:
            "Automatic approval review approved (risk: low, authorization: high): The user asked for this request.",
        },
      },
      {
        method: "item/autoApprovalReview/completed",
        params: {
          reviewId: "r2",
          review: { status: "denied", riskLevel: "high", rationale: "" },
          action: { type: "networkAccess", host: "evil.example" },
        },
      },
    );
    expect(notices.map((b) => [b.text, b.tone])).toEqual([
      [
        "Auto-review approved: curl -s https://example.com (low risk)\nThe user asked for this request.",
        undefined,
      ],
      [
        "Auto-review denied: network access to evil.example (high risk)",
        "warn",
      ],
    ]);
  });

  it("names what was reviewed", () => {
    expect(reviewTarget({ type: "applyPatch", files: ["a", "b"] })).toBe(
      "changes to 2 files",
    );
    expect(
      reviewTarget({ type: "mcpToolCall", server: "docs", toolName: "search" }),
    ).toBe("docs · search");
    expect(optionName("permissions", "auto-review", [])).toBe("Auto-review");
  });
});
