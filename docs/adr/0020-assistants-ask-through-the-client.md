# ADR 0020: Assistants ask the user through the client, as a wizard

- Date: 2026-11-23
- Status: accepted; amends 0015 (decision 5, the setup wizard)

## Context

ADR 0015 made setup a wizard: one question at a time, with the choices offered. Outside setup, assistants still asked in plain text: a question at the end of a long answer, or several at once ("tell me the address, a name for it, and when you have saved the password"). The user had to find the question, answer it by typing, and keep track of what was still missing. In use, the user asked that every request for information come the same way, as a step-by-step wizard with the choices the client can show.

## Decision

1. **Every question goes through the client's question tool.** Whenever an assistant needs something from the user (a choice, a fact, a name, a confirmation), in setup or any other task, it asks with AskUserQuestion for Claude (2–4 options, the recommended one first; the user can always type their own answer), or a short numbered list for Codex. Not as an open question at the end of its text.
2. **One question per step.** When several things are needed, the assistant says which step this is ("2 of 4"), asks one, acts on the answer, then asks the next. It does the configuring itself with scripts.
3. **Steps done outside the conversation are confirmed the same way.** Saving a password or signing in ends with a question such as "Done" / "Ran into a problem", and the assistant then checks (`secret.py check`, or running its check).
4. **Secrets are never asked this way.** The sign-in rule of ADR 0015 is unchanged: secrets are given outside the conversation.

The rule replaces "Setup is a wizard" in the shared rules (`internal/workbench/assistant/root/AGENTS.md`, shipped to every assistant's repository).

## Consequences

- Questions look and answer the same in every assistant, and the user sees how far a multi-step request has come.
- A request that needs several facts takes more turns than one message asking for all of them.
- Codex has no question tool; its numbered lists are a convention the rules ask for, not something the page shows.
