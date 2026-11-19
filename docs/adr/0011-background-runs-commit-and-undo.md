# ADR 0011: Background runs keep the wiki, read outside content, and commit what they change

- Date: 2026-11-19
- Status: accepted

## Context

An assistant that keeps the shared wiki (ADR 0010) has to fold what each conversation taught it into the wiki, as the LLM Wiki method's ingest step does. Asking for this at the end of every conversation is tedious, and doing it inside the conversation spends that session's context. Some routines also bring in outside content, such as new mail, that should be sorted without the user present and without it reaching a session that has more tools than it needs. Both kinds of work change files that the user did not watch change, so each change has to be visible and easy to take back.

## Decision

1. **A background run is a one-off agent run in an assistant's folder that no session shows.** Claude runs with `-p --no-session-persistence --permission-mode acceptEdits --tools Read,Edit,Write,Glob,Grep` (only the file tools exist in the run; allowing them alone was not enough, because acceptEdits and the user's own allow rules let Bash run `touch`) and with no MCP servers. Codex runs `exec --ephemeral --sandbox workspace-write` (no network). Both can write in the repository of assistants (`--add-dir`) and get the prompt on stdin. A run is told first that nobody can answer it and that it must end with a `Summary:` line. It times out after 15 minutes, and only one runs at a time in the repository.
2. **The workbench commits what a run changed.** Before the run it commits anything already changed, as "chore: keep changes made before …", so that the run's own commit (`ingest: <title>` or `routine: <name>`) holds only the run's changes. Each run is recorded in `assistant_changes` (kept 90 days) with its commit, files and summary, and an `assistant` event tells the page.
3. **Undo is `git revert`.** It makes a new commit. If later changes touch the same lines, the revert is aborted and the error is shown; the repository stays as it was.
4. **Ingest.** When a session in the folder of an assistant with `memory = "wiki"` goes to sleep (its engine stopped after idling), or gives way to a new topic, the turns not yet ingested are written to `raw/sessions/<date>-<id>-<first turn>.md`. The file holds the user's words, the replies, and one line per tool call. Tool output and subagents are left out, and anything that looks like a secret is replaced with `[redacted]`. A record is never rewritten. A background run then ingests it as the assistant's `AGENTS.md` says. How far each session has been ingested is kept in the `assistant:ingested:<session>` setting; it moves only when the run succeeds, and a failed ingest can be retried from the same record.
5. **Background routines.** `background = true` on a routine in `assistant.toml` (a message is then required) hands the message, together with what its command printed, to a background run instead of a session. The run's summary is the routine run's output. The run notifies only when it changed something or failed.

## Consequences

- The wiki follows the conversations without the user asking, and each update is one commit they can read and undo: the conversation's banner shows "Memory updated", and Settings → Assistants lists recent changes with Undo and, for a failed ingest, Retry.
- Outside content goes to a run that can only edit files (Claude) or has no network (Codex), and the user sees what it wrote in the repository. Codex's sandbox still lets it read files outside the repository, so keeping secrets out of reach is still the job of the rules in ADR 0010.
- Background runs need git; without it they fail with that reason. Changes the user had not committed are committed before a run, under their own message.
- An ingest costs one agent run per resting conversation that has new turns.
- The first id of a session the workbench starts is replaced by its native id. A continuous assistant's topic now follows that change; before, it lost the topic and the next message started a new one.
