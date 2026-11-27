# ADR 0021: Outside content waits for the user's approval, and background runs change only their own files

- Date: 2026-11-23
- Status: accepted; amends 0009 (a routine's run can wait) and 0011 (what a background run may change); Approve and the background run's commit and undo amended by [0027](0027-an-assistants-routine-work-takes-turns.md)

## Context

A mail assistant fetched new mail every half hour and handed what its script printed (sender and subject lines) straight to a background run that sorts it (ADR 0011). That run had only the file tools and no network, but it could write anywhere in the repository of assistants. A mail written to steer it could have it change a script a routine runs later (`tools/fetch.py`, which has the network and the mail password), add a command routine to an `assistant.toml`, rewrite another assistant's `AGENTS.md`, or put instructions into the shared wiki that every later conversation reads. Nothing looked at the commit before the script ran again. Subjects also reached the run inside the routine's message, outside the file that marks mail as data.

The user asked that the senders and subjects be shown to them first, by the workbench and not by an agent, and that an agent see the mail only after they approve.

## Decision

1. **A routine can wait for approval.** `approve = true` on a routine in `assistant.toml` (it needs a command and a message) keeps what the command printed: the run's status becomes `waiting`, it notifies, and nothing is sent. `preview = "<file>"`, a path inside the assistant's folder, names a file the command writes for the user to look at first.
   - The assistant's home card shows each routine with waiting runs: the first line of the output, **Preview** (opens the file in the preview panel, rendered as Markdown with no HTML run), **Approve** and **Dismiss**. Both act on every waiting run of the routine, oldest first. (Amended by [0023](0023-the-preview-is-read-before-approve.md): the card shows the preview itself, and Approve waits until it was opened.)
   - `POST /api/routines/{id}/runs/{run}/approve` sends the routine's message with the run's output, as an unapproved routine would have; `…/dismiss` ends the run as skipped. A run is answered once; a second answer is refused.
   - A waiting run survives restarts and is not pushed out by newer runs; it is dropped after a month like any run.
   - The page cannot set a preview: only an assistant's file names one.
2. **A background run changes only its own files.** It may change its assistant's `state/`, `drafts/` and `log.md`, and `shared/wiki/` if the assistant keeps the wiki (`memory = "wiki"`). After the run, every other change is undone before the commit: a changed or deleted file is restored from the last commit, a new file is removed. The change and its routine run fail with the files it undid (`blocked`), so the user hears about it. The preamble every background run is given says so.
3. **An assistant's commands and background runs take turns.** A command from an assistant's routine waits while a background run changes the repository, and the other way round, so a file a run changed is undone before any command can run it.

## Consequences

- A mail (or any outside content a routine fetches) reaches an agent only after the user saw who sent it and approved. Approving still hands the content to an agent: what it says can still steer that run, now one the user started and one that cannot change scripts, settings or procedures.
- Background runs can no longer write elsewhere, such as a `reviews/` folder; an assistant that needs that does the work in a session.
- Files git ignores are not checked: a run could still write one. The assistants' own scripts and settings are tracked, so they are covered.
- Codex in its workspace sandbox can still read files outside the repository (ADR 0011).
- The mail assistant's script prints only how many mails there are and their files; senders and subjects go into the preview, escaped so a subject cannot become a link, an image or HTML, and each mail's file keeps its headers and body in a code block.
