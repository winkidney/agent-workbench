import { describe, expect, it } from "vitest";
import { freshPlanMessage, pendingPlan } from "./plans";
import { applyEvents, emptyState } from "./reducer";
import type { WorkbenchEvent } from "./model";

const plan = (key: string, extra = {}) => ({
  key,
  kind: "plan" as const,
  text: "# Plan\n\n- Do it",
  ...extra,
});
const turn = (status: string) => ({
  key: "turn:" + status,
  kind: "turn" as const,
  text: status,
});
const user = { key: "u", kind: "user" as const, text: "hi" };
const reply = { key: "a", kind: "assistant" as const, text: "done" };

describe("pendingPlan", () => {
  it("offers the plan that ended a completed turn", () => {
    expect(pendingPlan([user, plan("p"), turn("completed")])).toBe("p");
    // Later plans replace earlier ones; notices may follow the turn.
    expect(
      pendingPlan([
        plan("old"),
        turn("completed"),
        plan("new"),
        reply,
        turn("completed"),
        { key: "n", kind: "notice", text: "warning" },
      ]),
    ).toBe("new");
  });

  it("waits for the turn and stops once a message is sent", () => {
    expect(pendingPlan([plan("p")])).toBeUndefined();
    expect(pendingPlan([plan("p", { streaming: true })])).toBeUndefined();
    expect(pendingPlan([plan("p"), turn("completed"), user])).toBeUndefined();
    expect(pendingPlan([plan("p"), turn("interrupted")])).toBeUndefined();
  });

  it("ignores progress step lists and empty plans", () => {
    expect(
      pendingPlan([
        plan("steps", { steps: [{ text: "a", status: "done" }] }),
        turn("completed"),
      ]),
    ).toBeUndefined();
    expect(
      pendingPlan([plan("empty", { text: " " }), turn("completed")]),
    ).toBeUndefined();
  });

  it("starts a fresh session from the plan as Codex does", () => {
    expect(freshPlanMessage("# Plan")).toBe(
      "A previous agent produced the plan below to accomplish the user's task. " +
        "Implement the plan in a fresh context. Treat the plan as the source of " +
        "user intent, re-read files as needed, and carry the work through " +
        "implementation and verification.\n\n# Plan",
    );
  });
});

describe("Codex's proposed plan in the transcript", () => {
  let seq = 0;
  const native = (data: Record<string, unknown>): WorkbenchEvent => ({
    seq: ++seq,
    session: "s",
    kind: "native",
    data,
    time: new Date(0).toISOString(),
  });
  it("waits for a decision once the plan-mode turn completes", () => {
    const planItem = {
      id: "t-plan",
      type: "plan",
      text: "# AW upgrade\n\n- Stop the old runtime",
    };
    let s = applyEvents(
      emptyState(),
      [
        native({
          method: "item/started",
          params: { item: { ...planItem, text: "" } },
        }),
        native({ method: "item/completed", params: { item: planItem } }),
      ],
      { agent: "codex", history: false },
    );
    const list = () => s.keys.map((k) => s.blocks.get(k)!);
    expect(pendingPlan(list())).toBeUndefined();
    s = applyEvents(
      s,
      [
        native({
          method: "turn/completed",
          params: { turn: { id: "t", status: "completed", durationMs: 1000 } },
        }),
      ],
      { agent: "codex", history: false },
    );
    expect(pendingPlan(list())).toBe("t-plan");
  });
});
