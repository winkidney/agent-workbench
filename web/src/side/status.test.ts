import { describe, expect, it } from "vitest";
import { canSteer, echoed, lastQuestion, liveSide, sideVerb } from "./status";
import { emptyState, withSending } from "../transcript/reducer";

describe("side chat status", () => {
  it("finds the live side chat among read-only records", () => {
    const list = [
      { id: "a", status: "closed", created: "1" },
      { id: "b", status: "running", created: "2" },
      { id: "c", status: "interrupted", created: "3" },
    ];
    expect(liveSide(list)?.id).toBe("b");
    expect(liveSide(list.filter((x) => x.id !== "b"))).toBeUndefined();
  });

  it("says what a running question waits on", () => {
    expect(sideVerb("codex", "waking")).toBe("Waking Codex");
    expect(sideVerb("claude", "waking")).toBe("Waking Claude");
    expect(sideVerb("claude", "answering")).toBe("Waiting for Claude's answer");
    expect(sideVerb("codex", "answering")).toBe("Codex is working");
  });

  it("steers only a Codex answer under way", () => {
    expect(canSteer("codex", "running", "answering")).toBe(true);
    expect(canSteer("codex", "running", "waking")).toBe(false);
    expect(canSteer("codex", "idle")).toBe(false);
    expect(canSteer("claude", "running", "answering")).toBe(false);
  });

  it("gives back the last question, not a steer", () => {
    let s = withSending(emptyState(), "the question", "q");
    s = withSending(s, "a steer", "st");
    s.blocks.set("st", { ...s.blocks.get("st")!, steered: true });
    expect(lastQuestion(s)).toBe("the question");
    expect(lastQuestion(emptyState())).toBe("");
  });

  it("knows when the server echoed a message sent from the page", () => {
    const before = emptyState();
    const shown = withSending(before, "why?", "sending:1");
    // The page's own copy does not count.
    expect(echoed(shown, 0, "why?")).toBe(false);
    const server = withSending(before, "why? ", "server");
    server.blocks.set("server", {
      ...server.blocks.get("server")!,
      sending: false,
      local: false,
    });
    expect(echoed(server, 0, "why?")).toBe(true);
    expect(echoed(server, 1, "why?")).toBe(false);
  });
});
