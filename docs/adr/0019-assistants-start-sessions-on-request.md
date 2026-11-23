# ADR 0019: An assistant starts a separate session when the user asks for one

- Date: 2026-11-23
- Status: accepted

## Context

In a conversation with Moss or another assistant, the user sometimes wants a piece of work done in a session of its own ("start a new session in my-app to fix the login bug"). The assistant could only use its CLI's own subagents, which live inside its conversation, or ask the user to start the session by hand. The user wants a session the workbench manages: listed under its project, opened and continued there like any other.

## Decision

1. **One command, `session new`.** `agent-workbench session new [--folder DIR] [--agent claude|codex] [--title T] "first message"` starts an ordinary session in the folder, with the user's settings for new sessions, and sends the message. `--folder` defaults to the current directory and the agent to the asking session's. It goes through `/api/local/sessions` like the other agents' commands (ADR 0012).
2. **Only an assistant, only when asked.** The workbench accepts it only from a session in an assistant's folder: not from a terminal or a project session. The `start-session` skill, shipped with the shared skills (ADR 0016), tells assistants to use it only when the user asks, never on their own, and not for their own sub-tasks.
3. **Labelled, then left alone.** The first message starts with `[From another session: <assistant>: <title>]`, as with `assistant send`. The assistant does not follow the session or get its replies; the user continues it.

## Consequences

- The new session is a root session in its project, not a subagent and not one of the assistant's sessions: the sidebar lists it under Projects. Subagents, an assistant's own sessions, topics, `assistant send` and routines work as before.
- No commands to send to, watch or read another session: reading a reply would mean parsing each agent's records, and the user asked only for a session to continue.
- An agent tricked by outside content could start a session with a message, as it could message an assistant; it still runs only with the user's own settings and approvals.
