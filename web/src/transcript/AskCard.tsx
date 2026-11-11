import { useState } from "react";
import { MessageCircleQuestion } from "lucide-react";
import { Button } from "../components/ui/button";
import { useEnv } from "./media";
import {
  claudeAnswerText,
  questionItemId,
  replyText,
  type AskQuestion,
  type AskState,
} from "./questions";

const names = { claude: "Claude", codex: "Codex" } as const;

// Questions an agent asked (questions.ts), answered in place. Codex's are
// open while its turn waits; any question left unanswered keeps an Answer
// button, since agents often end the turn and expect the answer next. The
// answers go out as a message, into the running turn if there is one:
// Codex's reply envelope, or a readable list for Claude.
export function AskCard({
  agent,
  id,
  questions,
  state,
}: {
  agent: "claude" | "codex";
  id: string;
  questions: AskQuestion[];
  state: AskState;
}) {
  const env = useEnv();
  // Codex preselects the first option, as its own clients do.
  const [picked, setPicked] = useState<Record<number, string[]>>(() =>
    agent === "codex"
      ? Object.fromEntries(
          questions.flatMap((q, i) =>
            q.options?.length ? [[i, [q.options[0].label]]] : [],
          ),
        )
      : {},
  );
  const [typed, setTyped] = useState<Record<number, string>>({});
  const [answering, setAnswering] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const open = state.status === "open";
  const formShown = open || answering;
  const answerOf = (i: number) => {
    const text = typed[i]?.trim() ?? "";
    if (questions[i].multi)
      return [...(picked[i] ?? []), ...(text ? [text] : [])].join(", ");
    return text || (picked[i]?.[0] ?? "");
  };
  const remaining = questions
    .map((_, i) => i)
    .filter((i) => state.answers[i] === undefined);
  const complete = remaining.every((i) => answerOf(i));

  const choose = (i: number, label: string) => {
    const q = questions[i];
    setPicked((p) => {
      const cur = p[i] ?? [];
      if (!q.multi) return { ...p, [i]: [label] };
      return {
        ...p,
        [i]: cur.includes(label)
          ? cur.filter((x) => x !== label)
          : [...cur, label],
      };
    });
    if (!q.multi) setTyped((t) => ({ ...t, [i]: "" }));
  };

  const submit = async () => {
    if (!env.answer || !complete) return;
    setBusy(true);
    setError("");
    try {
      await env.answer(
        agent === "codex"
          ? replyText(
              remaining.map((i) => ({
                questionItemId: questionItemId(id, i),
                question: questions[i].title,
                answer: answerOf(i),
              })),
            )
          : claudeAnswerText(
              remaining.map((i) => ({
                question: questions[i].title,
                answer: answerOf(i),
              })),
            ),
      );
      setAnswering(false);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  const status =
    state.status === "answered"
      ? "Answered"
      : state.status === "unanswered"
        ? "Not answered"
        : "";
  return (
    <div
      className={"approval ask-card" + (formShown ? "" : " settled")}
      role="group"
      aria-label={`${names[agent]}'s questions`}
    >
      <div className="approval-title">
        <MessageCircleQuestion size={15} />
        <strong>
          {open
            ? `${names[agent]} is waiting for your answer`
            : `${names[agent]} asked`}
        </strong>
        {status && <span className="ask-status">{status}</span>}
      </div>
      {formShown ? (
        <form
          onSubmit={(e) => {
            e.preventDefault();
            void submit();
          }}
        >
          {questions.map((q, i) =>
            state.answers[i] !== undefined ? (
              <div key={i} className="ask-answered">
                <span className="ask-question">{q.title}</span>
                <span className="ask-answer">{state.answers[i]}</span>
              </div>
            ) : (
              <fieldset key={i} className="question">
                {q.header && (
                  <span className="question-header">{q.header}</span>
                )}
                <legend>{q.title}</legend>
                {q.options?.map((o, j) => {
                  const on =
                    (picked[i] ?? []).includes(o.label) &&
                    (q.multi || !typed[i]?.trim());
                  return (
                    <button
                      key={j}
                      type="button"
                      className={"approval-option" + (on ? " selected" : "")}
                      aria-pressed={on}
                      onClick={() => choose(i, o.label)}
                    >
                      <span className="pointer">
                        {q.multi ? (on ? "☒" : "☐") : on ? "◉" : "○"}
                      </span>
                      <span className="num">{j + 1}.</span>
                      <span>{o.label}</span>
                      {o.description && (
                        <span className="hint">{o.description}</span>
                      )}
                    </button>
                  );
                })}
                <input
                  aria-label={`Your answer to ${q.title}`}
                  placeholder={
                    q.options?.length
                      ? q.multi
                        ? "Add another answer…"
                        : "Or type an answer…"
                      : "Your answer"
                  }
                  value={typed[i] ?? ""}
                  onChange={(e) =>
                    setTyped((t) => ({ ...t, [i]: e.target.value }))
                  }
                />
              </fieldset>
            ),
          )}
          <div className="approval-actions">
            <Button
              type="submit"
              size="sm"
              disabled={busy || !complete || !env.answer}
            >
              {busy ? "Sending…" : "Send answers"}
            </Button>
            {!open && (
              <Button
                type="button"
                size="sm"
                variant="outline"
                disabled={busy}
                onClick={() => setAnswering(false)}
              >
                Cancel
              </Button>
            )}
            {!env.answer && (
              <span className="ask-note">
                Answer in the client running this session.
              </span>
            )}
          </div>
          {error && (
            <p role="alert" className="error-text">
              {error}
            </p>
          )}
        </form>
      ) : (
        <>
          {questions.map((q, i) => (
            <div key={i} className="ask-answered">
              <span className="ask-question">{q.title}</span>
              <span className="ask-answer">
                {state.answers[i] ??
                  (state.status === "answered" ? "Answered" : "Not answered")}
              </span>
            </div>
          ))}
          {state.status === "unanswered" && env.answer && (
            <div className="approval-actions">
              <Button
                type="button"
                size="sm"
                variant="outline"
                onClick={() => setAnswering(true)}
              >
                Answer
              </Button>
            </div>
          )}
        </>
      )}
    </div>
  );
}
