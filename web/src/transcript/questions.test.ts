import { describe, expect, it } from "vitest";
import {
  askStates,
  asyncQuestions,
  claudeAnswerText,
  claudeQuestions,
  parseAnswers,
  parseClaudeAnswers,
  parseReply,
  questionItemId,
  replyText,
} from "./questions";

// Recorded AskUserQuestion input and answered result (Claude Code 2.1.322).
const claudeInput = {
  questions: [
    {
      header: "Color",
      multiSelect: false,
      question: "Which color should the button be?",
      options: [
        { label: "Red", description: "Make the button red." },
        { label: "Green", description: "Make the button green." },
      ],
    },
    {
      header: "Extras",
      multiSelect: true,
      question: "Which extras should the page have?",
      options: [{ label: "Dark mode" }, { label: "Search" }],
    },
  ],
};
const claudeAnswered =
  'The user answered: "Which color should the button be?"="Green", "Which extras should the page have?"="Dark mode, Search, Breadcrumbs". Read the answers carefully — they may request clarification, changes, or that you not proceed — and follow what they actually say.';

describe("question formats", () => {
  it("identifies Codex questions as Codex and its desktop app do", () => {
    expect(questionItemId("call_1", 0)).toBe(
      '["request_user_input_async","call_1",0]',
    );
  });

  it("reads Codex's async questions only", () => {
    const questions = [
      { title: "Which color?", options: ["Red", "Green", " ", 3] },
      { title: "What should it say?", options: null },
      { title: " " },
    ];
    expect(asyncQuestions({ delivery: "async", questions })).toEqual([
      {
        title: "Which color?",
        options: [{ label: "Red" }, { label: "Green" }],
      },
      { title: "What should it say?", options: undefined },
    ]);
    expect(asyncQuestions({ delivery: null, questions })).toBeUndefined();
    const many = Array.from({ length: 40 }, (_, i) => "o" + i);
    expect(
      asyncQuestions({
        delivery: "async",
        questions: [{ title: "q", options: many }],
      })![0].options,
    ).toHaveLength(32);
  });

  it("reads Claude's AskUserQuestion input", () => {
    expect(claudeQuestions(claudeInput)).toEqual([
      {
        title: "Which color should the button be?",
        header: "Color",
        multi: false,
        options: [
          { label: "Red", description: "Make the button red." },
          { label: "Green", description: "Make the button green." },
        ],
      },
      {
        title: "Which extras should the page have?",
        header: "Extras",
        multi: true,
        options: [
          { label: "Dark mode", description: undefined },
          { label: "Search", description: undefined },
        ],
      },
    ]);
    expect(claudeQuestions({})).toBeUndefined();
  });

  it("writes the Codex reply envelope exactly", () => {
    const text = replyText([
      {
        questionItemId: questionItemId("call_1", 0),
        question: "Which\ncolor?",
        answer: 'Blue, "dark"',
      },
    ]);
    expect(text).toBe(
      '<send_user_message_question_reply>\n[{"answer":"Blue, \\"dark\\"","question":"Which color?","questionItemId":"[\\"request_user_input_async\\",\\"call_1\\",0]"}]\n</send_user_message_question_reply>',
    );
    expect(parseReply(text)?.[0].answer).toBe('Blue, "dark"');
    const long = parseReply(
      replyText([
        { questionItemId: "x", question: "é".repeat(400), answer: "a" },
      ]),
    )!;
    expect(
      new TextEncoder().encode(long[0].question).length,
    ).toBeLessThanOrEqual(512);
    for (const bad of [
      undefined,
      "Blue",
      "<send_user_message_question_reply>not json</send_user_message_question_reply>",
      'say <send_user_message_question_reply>[{"questionItemId":"m","question":"q","answer":"a"}]</send_user_message_question_reply>',
    ])
      expect(parseReply(bad)).toBeUndefined();
  });

  it("writes and reads late answers for Claude", () => {
    const text = claudeAnswerText([
      { question: "Which color?", answer: "Green" },
      { question: "Extras?", answer: "Dark mode, Search\nand a footer" },
    ]);
    expect(text).toBe(
      "Answers to your questions:\n\n> Which color?\nGreen\n\n> Extras?\nDark mode, Search\nand a footer",
    );
    expect(parseClaudeAnswers(text)).toEqual([
      { question: "Which color?", answer: "Green" },
      { question: "Extras?", answer: "Dark mode, Search\nand a footer" },
    ]);
    expect(parseAnswers(text)).toHaveLength(2);
    expect(
      parseClaudeAnswers("Answers to your questions: none"),
    ).toBeUndefined();
    expect(parseClaudeAnswers("Hello")).toBeUndefined();
  });
});

describe("askStates", () => {
  const codexAsk = (key: string, n: number) => ({
    key,
    kind: "assistant",
    questions: Array.from({ length: n }, (_, i) => ({ title: "q" + i })),
  });
  const envelope = (key: string, pairs: [string, number, string][]) => ({
    key,
    kind: "user",
    text: replyText(
      pairs.map(([m, i, answer]) => ({
        questionItemId: questionItemId(m, i),
        question: "q" + i,
        answer,
      })),
    ),
  });
  const claudeAsk = (key: string, status: string, output = "") => ({
    key,
    kind: "tool",
    tool: { name: "AskUserQuestion", input: claudeInput, status, output },
  });
  const turn = { key: "turn:t", kind: "turn" };

  it("keeps Codex's questions open while its turn waits", () => {
    const s = askStates([codexAsk("a", 2), envelope("u", [["a", 0, "Blue"]])]);
    expect(s.get("a")).toEqual({ status: "open", answers: { 0: "Blue" } });
    expect(
      askStates([
        codexAsk("a", 2),
        envelope("u1", [["a", 0, "Blue"]]),
        envelope("u2", [["a", 1, "Buy now"]]),
      ]).get("a"),
    ).toEqual({ status: "answered", answers: { 0: "Blue", 1: "Buy now" } });
  });

  it("leaves unanswered questions answerable after the turn or another message", () => {
    for (const after of [turn, { key: "x", kind: "user", text: "hi" }]) {
      expect(askStates([codexAsk("a", 1), after]).get("a")!.status).toBe(
        "unanswered",
      );
      expect(
        askStates([
          codexAsk("a", 1),
          after,
          envelope("late", [["a", 0, "Blue"]]),
        ]).get("a"),
      ).toEqual({ status: "answered", answers: { 0: "Blue" } });
    }
    // A reply naming the whole message answers all of it.
    expect(
      askStates([
        codexAsk("a", 2),
        {
          key: "u",
          kind: "user",
          text: '<send_user_message_question_reply>[{"questionItemId":"a","question":"q","answer":"yes"}]</send_user_message_question_reply>',
        },
      ]).get("a")!.status,
    ).toBe("answered");
  });

  it("follows Claude's AskUserQuestion", () => {
    // Waiting for its approval card; the end of the turn leaves it unanswered.
    expect(askStates([claudeAsk("t", "running")]).get("t")!.status).toBe(
      "pending",
    );
    expect(askStates([claudeAsk("t", "running"), turn]).get("t")!.status).toBe(
      "unanswered",
    );
    // Answered through the card: the answers come from Claude's result.
    expect(askStates([claudeAsk("t", "ok", claudeAnswered)]).get("t")).toEqual({
      status: "answered",
      answers: { 0: "Green", 1: "Dark mode, Search, Breadcrumbs" },
    });
    // Declined, then answered later as a message.
    const late = {
      key: "u",
      kind: "user",
      text: claudeAnswerText([
        { question: "Which color should the button be?", answer: "Red" },
        { question: "Which extras should the page have?", answer: "Search" },
      ]),
    };
    expect(askStates([claudeAsk("t", "declined")]).get("t")!.status).toBe(
      "unanswered",
    );
    expect(askStates([claudeAsk("t", "declined"), late]).get("t")).toEqual({
      status: "answered",
      answers: { 0: "Red", 1: "Search" },
    });
    // A late answer goes to the latest question asked that way.
    const s = askStates([
      claudeAsk("old", "failed"),
      claudeAsk("new", "interrupted"),
      late,
    ]);
    expect(s.get("new")!.status).toBe("answered");
    expect(s.get("old")!.status).toBe("unanswered");
  });
});
