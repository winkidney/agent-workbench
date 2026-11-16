import { describe, expect, it } from "vitest";
import { claudeAnswers, codexAnswers, withNote } from "./answers";

describe("answers with a note", () => {
  it("adds a Codex note as user_note among the answers", () => {
    expect(
      codexAnswers(
        { db: ["Postgres"], size: ["Small"] },
        { db: " we run it " },
      ),
    ).toEqual({
      db: { answers: ["Postgres", "user_note: we run it"] },
      size: { answers: ["Small"] },
    });
  });

  it("gives Claude the note as annotations by question", () => {
    expect(
      claudeAnswers(
        { "Which DB?": ["Postgres"], "Which size?": ["S", "M"] },
        { "Which DB?": "we run it", "Which size?": "  " },
      ),
    ).toEqual({
      answers: { "Which DB?": "Postgres", "Which size?": "S, M" },
      annotations: { "Which DB?": { notes: "we run it" } },
    });
    expect(claudeAnswers({ q: ["A"] }, {})).toEqual({ answers: { q: "A" } });
  });

  it("puts a note on its own line in a later answer", () => {
    expect(withNote("Postgres", "we run it")).toBe("Postgres\nNote: we run it");
    expect(withNote("Postgres", " ")).toBe("Postgres");
  });
});
