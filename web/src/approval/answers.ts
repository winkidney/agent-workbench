// Answers to an agent's questions, with the note a user may add to the
// option they picked, in each CLI's own form.

// Codex keeps a note among the answers, as its own client does.
export const CODEX_NOTE = "user_note: ";

export function codexAnswers(
  answers: Record<string, string[]>,
  notes: Record<string, string>,
): Record<string, { answers: string[] }> {
  return Object.fromEntries(
    Object.entries(answers).map(([id, values]) => {
      const note = notes[id]?.trim();
      return [id, { answers: note ? [...values, CODEX_NOTE + note] : values }];
    }),
  );
}

// Claude Code takes notes as AskUserQuestion's annotations, by question.
export function claudeAnswers(
  answers: Record<string, string[]>,
  notes: Record<string, string>,
): {
  answers: Record<string, string>;
  annotations?: Record<string, { notes: string }>;
} {
  const annotations = Object.fromEntries(
    Object.keys(answers)
      .filter((q) => notes[q]?.trim())
      .map((q) => [q, { notes: notes[q].trim() }]),
  );
  return {
    answers: Object.fromEntries(
      Object.entries(answers).map(([q, values]) => [q, values.join(", ")]),
    ),
    ...(Object.keys(annotations).length ? { annotations } : {}),
  };
}

// An answer sent later as a message: the note on its own line.
export const withNote = (answer: string, note?: string) =>
  note?.trim() ? `${answer}\nNote: ${note.trim()}` : answer;
