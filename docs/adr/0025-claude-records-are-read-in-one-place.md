# ADR 0025: Claude's records are read in one place, live and from the transcript

- Date: 2026-11-27
- Status: accepted

## Context

Claude Code writes the same event two ways: as stream-json on a running CLI's stdout, and as a line of the session's transcript (JSONL). A subagent's report is a `system/task_notification` with `handback_report` live; in the transcript it is a `queued_command` attachment whose `origin` says it is a peer message (taken in mid-turn) or a meta `user` record (between turns). A background task's end is a `task_notification` live and a `queued_command` with `commandMode: "task-notification"` or a `user` record in the transcript.

The workbench read them in three places, each knowing some shapes: the history reader rebuilt records from a list of fields and turned two kinds of attachment into user records, the live path sent records as they were, and the page told the shapes apart, with patterns on the raw text as a fallback. So the same report showed differently live and later, or not at all: live the page left `task_notification` out; history dropped a mid-turn report's origin, so it showed as raw `<agent-message>` text (fixed in v0.7.18), and left out task notifications taken in mid-turn. After a turn the page replaces its live view with the transcript's records only when the last live record is among them, and live and transcript records of one event have different uuids.

## Decision

1. **One reader on the server.** `claudeView` (`claude_records.go`) reads every Claude record before the page gets it, live (`receive`) and from the transcript (history pages, and the transcripts of sessions another client runs). It makes:
   - `workbench_peer`: a message from another session or a subagent's report (`from`, `name` when known, `handback`, `body` as Markdown, taken out of the `<agent-message>` without the hand-back preamble when the origin does not carry it);
   - `workbench_task`: a background task the model was told had ended (`taskId`, `status`, `summary`, `toolUses`, `tokens`, `durationMs`, `detail`; a plain line such as a plugin's notice becomes the summary);
   - the user's own message taken in mid-turn, as a steered user record.
   Every other record passes as it is. The page renders these two records and no longer reads the raw text.
2. **The same key live and in the transcript.** A subagent's report is `peer:<task id>:<hash of its text>`: both shapes carry the task id and the text, and a resumed subagent reports again with the same id. Live shows only reports: most other `task_notification`s never reach the model or the transcript (of 254 such records stored on this computer, 30 did), so a task notification shows from the transcript, keyed by its record (`task:<uuid>`), once the turn's record replaces the live view.
3. **Shapes not read are noticed.** A live record type outside the known list, and a `queued_command` of a mode or origin `claudeView` does not read, is logged once per process by its type only.
4. **One set of cases for both sides.** `internal/workbench/testdata/claude` holds each event as written live and in the transcript. The Go test reads history from the transcript case and checks it against `<case>.view.json`, and checks that live shows the same records under the same keys. The page's test (`web/src/transcript/records.test.ts`) renders the same `view.json` files, and checks that a live report is replaced by the turn's record.

## Consequences

- A subagent's report shows live as Markdown and stays one card when the turn's record replaces the live view. Task notifications taken in mid-turn now show in history, as do plugin notices sent the same way.
- A new record shape from the CLI is handled in `claude_records.go`, with a case in `testdata/claude`; the page changes only for a new kind of line.
- Codex is not affected: its history (`thread/turns/list`) and its live records use the same items.
- Live task notifications other than reports still show only after the turn, from the transcript.
