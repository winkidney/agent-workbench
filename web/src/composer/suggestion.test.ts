import { describe, expect, it } from "vitest";
import { shownSuggestion } from "./suggestion";

describe("prompt suggestions", () => {
  const idle = {
    suggestion: " run the tests ",
    prompt: "",
    running: false,
    blocked: false,
  };

  it("shows in an empty prompt between turns", () => {
    expect(shownSuggestion(idle)).toBe("run the tests");
  });

  it("gives way to typing, a turn, and a session the page cannot write to", () => {
    expect(shownSuggestion({ ...idle, prompt: "r" })).toBeNull();
    // A space typed is still typing.
    expect(shownSuggestion({ ...idle, prompt: " " })).toBeNull();
    expect(shownSuggestion({ ...idle, running: true })).toBeNull();
    expect(shownSuggestion({ ...idle, blocked: true })).toBeNull();
    expect(shownSuggestion({ ...idle, suggestion: "  " })).toBeNull();
    expect(shownSuggestion({ ...idle, suggestion: undefined })).toBeNull();
  });
});
