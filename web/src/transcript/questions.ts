// Codex async questions (request_user_input_async), as Codex 0.167 and its
// TUI and desktop app handle them.
//
// The model posts questions as an agent message with delivery "async" and a
// `questions` list ({title, options?}), then keeps working or waits
// (clock.sleep, which ends early when new input arrives). The answer is an
// ordinary user message, steered into the running turn or sent as a new one,
// whose text is the reply envelope below. Each question is identified by
// JSON ["request_user_input_async", <message id>, <index>]. An envelope in a
// later user message answers those questions (also when another client sent
// it); any other user message skips the open ones, and so does the end of the
// turn that asked (Codex only waits for answers within it).
// Sources: codex-rs core/src/tools/handlers/request_user_input_async.rs,
// context-fragments/src/answered_question.rs, tui/src/async_question_reply.rs.

export type AsyncQuestion = { title: string; options?: string[] };

export type QuestionReply = {
  questionItemId: string;
  question: string;
  answer: string;
};

const START = "<send_user_message_question_reply>";
const END = "</send_user_message_question_reply>";
const IDE_PREFIX = "# Context from my IDE setup:\n";
const IDE_REQUEST = "\n## My request for Codex:\n";
// As the TUI bounds model-authored suggestions and titles.
const MAX_OPTIONS = 32;
const MAX_TEXT_BYTES = 512;

export function questionItemId(messageId: string, index: number): string {
  return JSON.stringify(["request_user_input_async", messageId, index]);
}

// The questions of an async agent message, or undefined.
export function asyncQuestions(item: {
  delivery?: unknown;
  questions?: unknown;
}): AsyncQuestion[] | undefined {
  if (item.delivery !== "async" || !Array.isArray(item.questions)) return;
  const out: AsyncQuestion[] = [];
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
    out.push({ title, options: opts.length ? opts : undefined });
  }
  return out.length ? out : undefined;
}

const encoder = new TextEncoder();
const bytes = (s: string) => encoder.encode(s).length;

// The question as the reply repeats it: at most 512 bytes, on one line.
function boundTitle(title: string): string {
  let t = title;
  while (bytes(t) > MAX_TEXT_BYTES) t = t.slice(0, -1);
  return t.replace(/[\n\r]/g, " ");
}

// The user message that answers questions.
export function replyText(replies: QuestionReply[]): string {
  const body = replies.map((r) => ({
    answer: r.answer,
    question: boundTitle(r.question),
    questionItemId: r.questionItemId,
  }));
  return `${START}\n${JSON.stringify(body)}\n${END}`;
}

// The answers a user message carries, if it is a reply envelope.
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

export type AskState = {
  status: "open" | "answered" | "skipped" | "ended";
  // Answers given so far, by question index.
  answers: Record<number, string>;
};

// Where each question message stands, from the user messages after it.
export function askStates(
  blocks: {
    key: string;
    kind: string;
    questions?: AsyncQuestion[];
    text?: string;
  }[],
): Map<string, AskState> {
  const states = new Map<string, AskState>();
  const open: { key: string; count: number }[] = [];
  for (const b of blocks) {
    if (b.kind === "assistant" && b.questions?.length) {
      states.set(b.key, { status: "open", answers: {} });
      open.push({ key: b.key, count: b.questions.length });
      continue;
    }
    if (b.kind === "turn") {
      // The turn that asked is over: Codex no longer waits for answers.
      for (const o of open.splice(0)) states.get(o.key)!.status = "ended";
      continue;
    }
    if (b.kind !== "user" || !open.length) continue;
    const replies = parseReply(b.text);
    if (!replies) {
      // Another message instead of an answer skips what is still open.
      for (const o of open.splice(0)) states.get(o.key)!.status = "skipped";
      continue;
    }
    for (const r of replies) {
      for (const o of open) {
        const s = states.get(o.key)!;
        for (let i = 0; i < o.count; i++)
          if (r.questionItemId === questionItemId(o.key, i))
            s.answers[i] = r.answer;
        // Older desktop replies name the whole message.
        if (r.questionItemId === o.key)
          for (let i = 0; i < o.count; i++) s.answers[i] ??= r.answer;
      }
    }
    for (let i = open.length - 1; i >= 0; i--) {
      const s = states.get(open[i].key)!;
      if (Object.keys(s.answers).length >= open[i].count) {
        s.status = "answered";
        open.splice(i, 1);
      }
    }
  }
  return states;
}
