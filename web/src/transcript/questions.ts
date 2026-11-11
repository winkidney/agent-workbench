// Questions an agent asked, answered in place, now or later.
//
// Codex (0.167) asks with request_user_input_async: an agent message with
// delivery "async" and a `questions` list ({title, options?}); it may wait
// (clock.sleep ends when input arrives) or end its turn. The answer is an
// ordinary user message whose text is the reply envelope below, with each
// question identified by JSON ["request_user_input_async", <message id>,
// <index>]; Codex and its clients recognize it in later messages too.
// Sources: codex-rs core/src/tools/handlers/request_user_input_async.rs,
// context-fragments/src/answered_question.rs, tui/src/async_question_reply.rs.
//
// Claude asks with the AskUserQuestion tool, answered through the approval
// card while it waits. If it ended unanswered (declined, interrupted, the
// turn ended), the answers can still go out as a message in the readable
// format below, which the workbench recognizes the same way.
//
// Unlike the Codex TUI, which drops questions when the turn ends, an
// unanswered question stays answerable: models often end the turn and
// expect the answer as the next message.

export type AskOption = { label: string; description?: string };

export type AskQuestion = {
  title: string;
  header?: string;
  options?: AskOption[];
  // Several options may be chosen (Claude's multiSelect).
  multi?: boolean;
};

export type QuestionReply = {
  questionItemId: string;
  question: string;
  answer: string;
};

const START = "<send_user_message_question_reply>";
const END = "</send_user_message_question_reply>";
const IDE_PREFIX = "# Context from my IDE setup:\n";
const IDE_REQUEST = "\n## My request for Codex:\n";
const CLAUDE_HEADING = "Answers to your questions:";
// As the Codex TUI bounds model-authored suggestions and titles.
const MAX_OPTIONS = 32;
const MAX_TEXT_BYTES = 512;

export function questionItemId(messageId: string, index: number): string {
  return JSON.stringify(["request_user_input_async", messageId, index]);
}

const encoder = new TextEncoder();
const bytes = (s: string) => encoder.encode(s).length;

// Codex: the questions of an async agent message, or undefined.
export function asyncQuestions(item: {
  delivery?: unknown;
  questions?: unknown;
}): AskQuestion[] | undefined {
  if (item.delivery !== "async" || !Array.isArray(item.questions)) return;
  const out: AskQuestion[] = [];
  for (const q of item.questions) {
    if (!q || typeof q !== "object") continue;
    const { title, options } = q as Record<string, unknown>;
    if (typeof title !== "string" || !title.trim()) continue;
    const opts = Array.isArray(options)
      ? options
          .filter(
            (o): o is string =>
              typeof o === "string" && !!o.trim() && bytes(o) <= MAX_TEXT_BYTES,
          )
          .slice(0, MAX_OPTIONS)
      : [];
    out.push({
      title,
      options: opts.length ? opts.map((label) => ({ label })) : undefined,
    });
  }
  return out.length ? out : undefined;
}

// Claude: the questions of an AskUserQuestion tool call.
export function claudeQuestions(input: unknown): AskQuestion[] | undefined {
  const qs = (input as { questions?: unknown } | undefined)?.questions;
  if (!Array.isArray(qs)) return;
  const out: AskQuestion[] = [];
  for (const q of qs) {
    if (!q || typeof q !== "object") continue;
    const v = q as Record<string, unknown>;
    if (typeof v.question !== "string" || !v.question.trim()) continue;
    const options = Array.isArray(v.options)
      ? v.options.flatMap((o): AskOption[] => {
          const r = (o ?? {}) as Record<string, unknown>;
          return typeof r.label === "string" && r.label.trim()
            ? [
                {
                  label: r.label,
                  description:
                    typeof r.description === "string" && r.description
                      ? r.description
                      : undefined,
                },
              ]
            : [];
        })
      : [];
    out.push({
      title: v.question,
      header: typeof v.header === "string" && v.header ? v.header : undefined,
      options: options.length ? options : undefined,
      multi: v.multiSelect === true,
    });
  }
  return out.length ? out : undefined;
}

// The question as a reply repeats it: at most 512 bytes, on one line.
function oneLine(title: string): string {
  let t = title;
  while (bytes(t) > MAX_TEXT_BYTES) t = t.slice(0, -1);
  return t.replace(/[\n\r]/g, " ");
}

// Codex: the user message that answers questions.
export function replyText(replies: QuestionReply[]): string {
  const body = replies.map((r) => ({
    answer: r.answer,
    question: oneLine(r.question),
    questionItemId: r.questionItemId,
  }));
  return `${START}\n${JSON.stringify(body)}\n${END}`;
}

// Codex: the answers a user message carries, if it is a reply envelope.
export function parseReply(
  text: string | undefined,
): QuestionReply[] | undefined {
  let t = (text ?? "").trim();
  if (t.startsWith(IDE_PREFIX)) {
    const at = t.lastIndexOf(IDE_REQUEST);
    if (at < 0) return;
    t = t.slice(at + IDE_REQUEST.length).trim();
  }
  if (!t.startsWith(START) || !t.endsWith(END)) return;
  let v: unknown;
  try {
    v = JSON.parse(t.slice(START.length, -END.length));
  } catch {
    return;
  }
  const list = Array.isArray(v) ? v : [v];
  const out: QuestionReply[] = [];
  for (const r of list) {
    if (!r || typeof r !== "object") return;
    const { questionItemId, question, answer } = r as Record<string, unknown>;
    if (
      typeof questionItemId !== "string" ||
      typeof question !== "string" ||
      typeof answer !== "string"
    )
      return;
    out.push({ questionItemId, question, answer });
  }
  return out.length ? out : undefined;
}

// Claude: answers given after the question ended, as a readable message.
export function claudeAnswerText(
  pairs: { question: string; answer: string }[],
): string {
  return [
    CLAUDE_HEADING,
    ...pairs.map((p) => `> ${oneLine(p.question)}\n${p.answer.trim()}`),
  ].join("\n\n");
}

export function parseClaudeAnswers(
  text: string | undefined,
): { question: string; answer: string }[] | undefined {
  const t = (text ?? "").trim();
  if (!t.startsWith(CLAUDE_HEADING + "\n")) return;
  const out: { question: string; answer: string }[] = [];
  for (const part of t.slice(CLAUDE_HEADING.length).split(/\n\n(?=> )/)) {
    const s = part.trim();
    if (!s) continue;
    const nl = s.indexOf("\n");
    if (!s.startsWith("> ") || nl < 0) return;
    out.push({ question: s.slice(2, nl), answer: s.slice(nl + 1).trim() });
  }
  return out.length ? out : undefined;
}

// Either reply format, for showing a message as question and answer.
export function parseAnswers(
  text: string | undefined,
): { question: string; answer: string }[] | undefined {
  return parseReply(text) ?? parseClaudeAnswers(text);
}

// Claude's own answered result: `The user answered: "Q"="A", "Q2"="A2". …`
// (older versions: `User has answered your questions: …`).
function claudeResultAnswers(output: string): Map<string, string> {
  const found = new Map<string, string>();
  for (const m of output.matchAll(/"((?:[^"\\]|\\.)*)"="((?:[^"\\]|\\.)*)"/g))
    found.set(m[1], m[2]);
  return found;
}

export type AskStatus = "open" | "pending" | "answered" | "unanswered";

export type AskState = {
  // open: Codex is still waiting in this turn. pending: Claude's approval
  // card is answering it. unanswered: ended without an answer, and can
  // still be answered.
  status: AskStatus;
  // Answers so far, by question index.
  answers: Record<number, string>;
};

type AskBlock = {
  key: string;
  kind: string;
  questions?: AskQuestion[];
  text?: string;
  tool?: { name: string; input?: unknown; status: string; output?: string };
};

// Where each question stands, from what came after it.
export function askStates(blocks: AskBlock[]): Map<string, AskState> {
  const states = new Map<string, AskState>();
  const asks: {
    key: string;
    agent: "codex" | "claude";
    titles: string[];
  }[] = [];
  const settle = (a: (typeof asks)[number]) => {
    const s = states.get(a.key)!;
    if (Object.keys(s.answers).length >= a.titles.length) s.status = "answered";
  };
  for (const b of blocks) {
    if (b.kind === "assistant" && b.questions?.length) {
      states.set(b.key, { status: "open", answers: {} });
      asks.push({
        key: b.key,
        agent: "codex",
        titles: b.questions.map((q) => q.title),
      });
      continue;
    }
    if (b.kind === "tool" && b.tool?.name === "AskUserQuestion") {
      const qs = claudeQuestions(b.tool.input);
      if (!qs) continue;
      const state: AskState = { status: "pending", answers: {} };
      if (b.tool.status === "ok") {
        state.status = "answered";
        const found = claudeResultAnswers(b.tool.output ?? "");
        qs.forEach((q, i) => {
          const a = found.get(q.title);
          if (a !== undefined) state.answers[i] = a;
        });
      } else if (b.tool.status !== "running") state.status = "unanswered";
      states.set(b.key, state);
      asks.push({
        key: b.key,
        agent: "claude",
        titles: qs.map((q) => q.title),
      });
      continue;
    }
    if (b.kind === "turn") {
      // The turn is over: nothing waits in it any more.
      for (const a of asks) {
        const s = states.get(a.key)!;
        if (s.status === "open" || s.status === "pending")
          s.status = "unanswered";
      }
      continue;
    }
    if (b.kind !== "user") continue;
    const envelope = parseReply(b.text);
    const late = envelope ? undefined : parseClaudeAnswers(b.text);
    if (envelope) {
      for (const r of envelope)
        for (const a of asks) {
          if (a.agent !== "codex") continue;
          const s = states.get(a.key)!;
          a.titles.forEach((_, i) => {
            // Older desktop replies name the whole message.
            if (
              r.questionItemId === questionItemId(a.key, i) ||
              r.questionItemId === a.key
            )
              s.answers[i] ??= r.answer;
          });
        }
      asks.filter((a) => a.agent === "codex").forEach(settle);
    } else if (late) {
      // The latest question with that title, not answered yet.
      for (const r of late)
        for (let j = asks.length - 1; j >= 0; j--) {
          const a = asks[j];
          const s = states.get(a.key)!;
          const i = a.titles.indexOf(r.question);
          if (a.agent !== "claude" || i < 0 || s.status === "answered")
            continue;
          if (s.answers[i] === undefined) {
            s.answers[i] = r.answer;
            break;
          }
        }
      asks.filter((a) => a.agent === "claude").forEach(settle);
    } else {
      // Another message instead: Codex stops waiting for these.
      for (const a of asks) {
        const s = states.get(a.key)!;
        if (s.status === "open") s.status = "unanswered";
      }
    }
  }
  return states;
}
