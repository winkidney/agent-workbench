import { useState } from "react";
import { MessageCircleQuestion } from "lucide-react";
import { Button } from "../components/ui/button";
import { useEnv } from "./media";
import {
  questionItemId,
  replyText,
  type AskState,
  type AsyncQuestion,
} from "./questions";

const settled: Record<AskState["status"], string> = {
  open: "",
  answered: "Answered",
  skipped: "Skipped",
  ended: "Not answered",
};

// Codex's async questions (questions.ts), answered in place: an option (the
// first is preselected, as in Codex) or a typed answer per question. The
// answers go out as an ordinary message, into the running turn if there is
// one, which wakes Codex while it waits.
export function AskCard({
  id,
  questions,
  state,
}: {
  id: string;
  questions: AsyncQuestion[];
  state: AskState;
}) {
  const env = useEnv();
  const [picked, setPicked] = useState<Record<number, number>>(() =>
    Object.fromEntries(
      questions.map((q, i) => [i, q.options?.length ? 0 : -1]),
    ),
  );
  const [typed, setTyped] = useState<Record<number, string>>({});
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const answerOf = (i: number) =>
    typed[i]?.trim() ||
    (picked[i] >= 0 ? (questions[i].options?.[picked[i]] ?? "") : "");
  const pending = questions
    .map((_, i) => i)
    .filter((i) => state.answers[i] === undefined);
  const complete = pending.every((i) => answerOf(i));
  const open = state.status === "open";

  const submit = async () => {
    if (!env.answer || !complete) return;
    setBusy(true);
    setError("");
    try {
      await env.answer(
        replyText(
          pending.map((i) => ({
            questionItemId: questionItemId(id, i),
            question: questions[i].title,
            answer: answerOf(i),
          })),
        ),
      );
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div
      className={"approval ask-card" + (open ? "" : " settled")}
      role="group"
      aria-label="Codex's questions"
    >
      <div className="approval-title">
        <MessageCircleQuestion size={15} />
        <strong>
          {open ? "Codex is waiting for your answer" : "Codex asked"}
        </strong>
        {!open && <span className="ask-status">{settled[state.status]}</span>}
      </div>
      {open ? (
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
                <legend>{q.title}</legend>
                {q.options?.map((o, j) => {
                  const on = picked[i] === j && !typed[i]?.trim();
                  return (
                    <button
                      key={j}
                      type="button"
                      className={"approval-option" + (on ? " selected" : "")}
                      aria-pressed={on}
                      onClick={() => {
                        setPicked((p) => ({ ...p, [i]: j }));
                        setTyped((t) => ({ ...t, [i]: "" }));
                      }}
                    >
                      <span className="pointer">{on ? "◉" : "○"}</span>
                      <span className="num">{j + 1}.</span>
                      <span>{o}</span>
                    </button>
                  );
                })}
                <input
                  aria-label={`Your answer to ${q.title}`}
                  placeholder={
                    q.options?.length ? "Or type an answer…" : "Your answer"
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
        questions.map((q, i) => (
          <div key={i} className="ask-answered">
            <span className="ask-question">{q.title}</span>
            <span className="ask-answer">
              {state.answers[i] ?? "Not answered"}
            </span>
          </div>
        ))
      )}
    </div>
  );
}
