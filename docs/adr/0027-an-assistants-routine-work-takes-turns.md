# ADR 0027: An assistant's routine work takes turns, and background runs keep to their own files

- Date: 2026-11-27
- Status: accepted; amends 0009 (routine runs), 0011 (what a background run commits and undoes) and 0021 (Approve)

## Context

The mail assistant checks mail every half hour and waits for approval (ADR 0021). When three checks had piled up, **Approve** answered each waiting run in turn. Each started its own conversation of a per-task assistant at once: three agents sorted into the same `state/inbox.md` and asked the user the same questions side by side.

Other assistants have the same problem without approval. The mail assistant's daily summary can start while a check's conversation is still asking questions. The bilibili assistant runs a background job at 12:00, which may take up to 15 minutes, and a conversation at 12:15 that reads what the job wrote. A code reviewer's half-hourly run can start while the previous review is still going.

Background runs had a second, repository-wide problem. A run committed everything changed in the repository before it started, then undid every change outside its assistant's own files and committed the rest. A conversation of another assistant, or the user's own session, that edited files in the meantime had its work undone, or committed under the background run's name. **Undo** committed everything first too.

## Decision

1. **Approve answers every waiting run at once.** `POST /api/routines/{id}/approve` (and `…/dismiss`) changes all waiting runs together. The oldest run carries the outputs of the others, oldest first, each under a `--- run of <time> ---` heading and cut to its own share of the hand-off, so the newest does not push out the oldest. The others end as `merged` (`mergedInto`), without notifying. The per-run endpoints stay for pages loaded earlier.
2. **Each assistant has a lane for its routine work.** A routine run that hands work to an assistant, in a session or in the background, holds that assistant's lane from when it is sent until it ends (its turn ends, its background run finishes). A memory update holds it while it runs.
   - A run that comes while the lane is held, or while an older run waits, is `held`. When the lane frees, the oldest held run goes. Every other held run of the same routine is merged into it, as in 1.
   - Who holds a lane is read from the runs in the store, so held runs outlast a restart. The scheduler sends them on its next tick.
   - The scheduler stops waiting for a sent run whose session no longer has it (the session was deleted, or its engine exited without reporting the turn's end) after two minutes. It keeps waiting while the run's message is in the session's queue, including a queue paused after a failed turn: resuming it later must not run beside the next run.
   - The assistant's card lists held runs with what they wait for (that routine's session opens from it), **Start now** (run the oldest beside it) and **Dismiss**. The heartbeat shows held runs as under way and merged runs as quiet.
   - Sessions the user starts take no part: they neither wait nor make routines wait. A continuous assistant (Moss) already gets its routine messages one after another in its ongoing conversation, which the user shares. The lane only adds that its memory updates and background runs wait for them, and the other way round.
   - A memory update that waits for a lane is not a run. If the workbench restarts meanwhile it is lost, and the next topic's update covers what it would have read.
3. **A background run commits and undoes only the files it wrote.** Claude runs with `--output-format stream-json`. The file each Edit and Write call names is the run's: those it may change are committed alone (`git commit -- <paths>`), and the others are put back.
   - A file the run wrote over is put back as it was before the run. That is the user's uncommitted edit if there was one; otherwise the last commit.
   - Before the run, only the places it may write are committed on their own. Changes elsewhere stay uncommitted and untouched.
   - An ingest's commit also takes the record it read.
   - **Undo** reverts the run's commit alone, and refuses while one of its files has an uncommitted change.
   - Other commits of the whole repository (setting up, a new assistant, the language, backing up) wait for the background run in progress.
   - Codex does not report what it writes (its sandbox can write with any command), so for Codex everything is as before: the whole repository is committed first, and every change during the run counts as its own.

## Consequences

- Approving three piled-up checks starts one conversation with all their mail.
- An assistant's scheduled work no longer runs beside itself. A conversation that waits for the user's answers can hold the lane for hours; the card says so and offers **Start now**.
- A background run no longer commits or undoes a session's work elsewhere in the repository. With Codex it still can.
- `held` and `merged` are new run statuses; pages from before this change show them only in the run list.
