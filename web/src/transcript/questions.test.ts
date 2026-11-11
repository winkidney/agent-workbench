import { describe, expect, it } from "vitest";
import {
  askStates,
  asyncQuestions,
  parseReply,
  questionItemId,
  replyText,
} from "./questions";

describe("Codex async questions", () => {
  it("identifies questions as Codex and its desktop app do", () => {
    expect(questionItemId("call_1", 0)).toBe(
      '["request_user_input_async","call_1",0]',
    );
  });

  it("reads the questions of an async message only", () => {
    const questions = [
      { title: "Which color?", options: ["Red", "Green", " ", 3] },
      { title: "What should it say?", options: null },
      { title: " " },
    ];
    expect(asyncQuestions({ delivery: "async", questions })).toEqual([
      { title: "Which color?", options: ["Red", "Green"] },
      { title: "What should it say?", options: undefined },
    ]);
    expect(asyncQuestions({ delivery: null, questions })).toBeUndefined();
    expect(
      asyncQuestions({ delivery: "async", questions: null }),
    ).toBeUndefined();
    const many = Array.from({ length: 40 }, (_, i) => "o" + i);
    expect(
      asyncQuestions({
        delivery: "async",
        questions: [{ title: "q", options: many }],
      })![0].options,
    ).toHaveLength(32);
  });

  it("writes the reply envelope Codex parses", () => {
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
    expect(parseReply(text)).toEqual([
      {
        questionItemId: '["request_user_input_async","call_1",0]',
        question: "Which color?",
        answer: 'Blue, "dark"',
      },
    ]);
    // Long questions are cut to 512 bytes, as the CLI does.
    const long = parseReply(
      replyText([
        { questionItemId: "x", question: "é".repeat(400), answer: "a" },
      ]),
    )!;
    expect(
      new TextEncoder().encode(long[0].question).length,
    ).toBeLessThanOrEqual(512);
  });

  it("parses replies only from complete envelopes", () => {
    const one =
      '<send_user_message_question_reply>{"questionItemId":"m","question":"q","answer":"a"}</send_user_message_question_reply>';
    expect(parseReply(one)).toEqual([
      { questionItemId: "m", question: "q", answer: "a" },
    ]);
    expect(
      parseReply(
        "# Context from my IDE setup:\nfoo\n## My request for Codex:\n" + one,
      ),
    ).toHaveLength(1);
    for (const text of [
      undefined,
      "Blue",
      "<send_user_message_question_reply>not json</send_user_message_question_reply>",
      'say <send_user_message_question_reply>[{"questionItemId":"m","question":"q","answer":"a"}]</send_user_message_question_reply>',
      '<send_user_message_question_reply>[{"question":"q","answer":"a"}]</send_user_message_question_reply>',
    ])
      expect(parseReply(text)).toBeUndefined();
  });

  it("follows each question message to answered or skipped", () => {
    const ask = (key: string, n: number) => ({
      key,
      kind: "assistant",
      questions: Array.from({ length: n }, (_, i) => ({ title: "q" + i })),
    });
    const reply = (key: string, ids: [string, number, string][]) => ({
      key,
      kind: "user",
      text: replyText(
        ids.map(([m, i, answer]) => ({
          questionItemId: questionItemId(m, i),
          question: "q" + i,
          answer,
        })),
      ),
    });
    // Open until every question has an answer, from one or more replies.
    let s = askStates([ask("a", 2), reply("u1", [["a", 0, "Blue"]])]);
    expect(s.get("a")).toEqual({ status: "open", answers: { 0: "Blue" } });
    s = askStates([
      ask("a", 2),
      reply("u1", [["a", 0, "Blue"]]),
      reply("u2", [["a", 1, "Buy now"]]),
    ]);
    expect(s.get("a")).toEqual({
      status: "answered",
      answers: { 0: "Blue", 1: "Buy now" },
    });
    // Another message instead skips what is open; later ones stay apart.
    s = askStates([
      ask("a", 1),
      { key: "u", kind: "user", text: "never mind" },
      ask("b", 1),
    ]);
    expect(s.get("a")!.status).toBe("skipped");
    expect(s.get("b")!.status).toBe("open");
    // The end of the turn that asked closes it (Codex stops waiting).
    s = askStates([
      ask("a", 1),
      { key: "turn:t", kind: "turn" },
      reply("u", [["a", 0, "late"]]),
    ]);
    expect(s.get("a")!.status).toBe("ended");
    // A reply naming the whole message answers all of it.
    s = askStates([
      ask("a", 2),
      {
        key: "u",
        kind: "user",
        text: '<send_user_message_question_reply>[{"questionItemId":"a","question":"q","answer":"yes"}]</send_user_message_question_reply>',
      },
    ]);
    expect(s.get("a")!.status).toBe("answered");
  });
});
