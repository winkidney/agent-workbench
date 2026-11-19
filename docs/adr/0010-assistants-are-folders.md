# ADR 0010: An assistant is a folder in a repository of assistants

- Date: 2026-11-19
- Status: accepted

## Context

Some work is done the same way every time: a weekly report from the same sources, a mail triage with the same rules, a check with the same script. People want to say "do it" (or have a routine say it) and get it done their way, either to the end or up to a step they approve. They also want one assistant that knows them and carries on from one day to the next. Each such job has its own procedure, scripts and schedule, and these have to be easy to keep and change.

The CLIs already have most of what this needs. Both read instruction files from the session's folder and the folders above it (Claude Code `CLAUDE.md`, Codex `AGENTS.md` from the repository root down), both find skills next to them (Claude Code in `.claude/skills` of the folder and its parents, Codex in `.agents/skills` of each folder up to the repository root; checked with Claude Code 2.1.328 and Codex 0.170), and Claude can ask questions in a card the workbench already shows. A plugin system inside the workbench would duplicate this and be harder to keep.

## Decision

1. **An assistant is a folder with an `assistant.toml`, in one repository of assistants.** The folder holds its procedure (`AGENTS.md`, with `CLAUDE.md` importing it), skills (`.agents/skills`, with `.claude/skills` a link to it), scripts (`tools/`), what is in progress (`state/loops.md`, `state/handoff.md`) and what it did (`log.md`). The repository's top holds the rules every assistant shares (`AGENTS.md`), shared skills and a shared wiki (`shared/wiki/`). Everything is plain files in git: reviewed, changed and undone like code; the assistant can change its own procedure when asked.
2. **The workbench reads only `assistant.toml`:** a name, the agent, how topics are kept (`sessions`), whether it keeps the shared wiki (`memory`), and its routines. Unknown keys are errors, so a typo does not pass silently; a file with an error is shown with it and its routines stay as they were until it is fixed.
3. **Setting up writes only what is missing.** Settings → Assistants makes the repository (with `git init` and a commit when git is there), its shared files and a first, personal assistant (`main`: one ongoing conversation, keeps the wiki, a daily check and a weekly wiki check); **New assistant** adds a folder from a template. Neither ever overwrites a file.
4. **Topics.** A `per-task` assistant starts a new session for every request and every routine run. A `continuous` assistant keeps one ongoing session; a new one starts with the first use after 04:00 the next day, after a turn leaves the context 60% full, or with **New topic**, and only while the session is not busy. What carries over is in the files (`state/handoff.md`, the wiki), which the procedure says to read first.
5. **Approval is part of the procedure, not of the workbench.** The shared rules say: before a step the procedure marks for approval, Claude asks with AskUserQuestion (Approve, Change, Reject), which shows as a card, marks the session as needing you and notifies; Codex, which cannot ask in its Default mode (`default_mode_request_user_input` is still in development in 0.170), ends its turn with a line starting `Needs your approval:`, and the routine run's end notifies. Work with no such step is done to the end.
6. **Routines in `assistant.toml` are routines (ADR 0009) of that assistant.** A message goes to the assistant (its ongoing topic, or a new session), a command runs in its folder. They keep their id, runs and on/off switch across edits of the file; the page can switch them off or run them now, and changes them only in the file.
7. **Credentials stay out.** The shared rules and the template keep secrets out of the repository and the conversation: scripts read them at run time from `~/.config/assistant/secrets/`, and Claude Code is denied reading that folder (`.claude/settings.json`). Codex's sandbox does not restrict reading, so that denial is a second line, not the guarantee.

## Consequences

- An assistant's behavior is in files anyone can read and diff; the workbench has no assistant logic to keep in step with the CLIs.
- The sidebar lists assistants above Pinned and keeps sessions in their folders out of Projects; a session waiting for you still shows in Active.
- A `continuous` assistant's new topic forgets the conversation, not the work: only what the procedure wrote down carries over, so the procedure must write it down.
- Claude Code reads `.claude/skills` through a link; where links cannot be made (Windows without the right), a note says to copy the folder.
- Codex assistants approve in prose until Codex asks questions in Default mode.
