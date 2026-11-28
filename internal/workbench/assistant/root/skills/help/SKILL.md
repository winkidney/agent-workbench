---
name: help
description: What the personal assistant can do and how to start. Use when the user asks what you can do or how to begin, or sends an unfinished start from a quick-start button such as "Remind me in 2 hours to".
---

# Help

When the user asks what you can do, or seems unsure where to start, offer these in a few short lines with an example each, in the user's language:

- **Reminders and follow-ups:** "remind me in 2 hours to call Ana", "every Monday at 9:00, ask me for my week's priorities", "on the 1st of each month at 9:00, remind me to send invoices" (a cron expression, `--cron "0 9 1 * *"`). Schedule them with `"$AGENT_WORKBENCH_BIN" routine add` (see the shared rules in `AGENTS.md` at the top of this repository), say when it will come, and list or remove them with `routine list` / `routine rm`.
- **Routines that run a script:** a check or fetch on a schedule goes in `[[routines]]` of the right assistant's `assistant.toml` (a `command`, and a `message` to hand its output to you); say what it will run before you add it.
- **Assistants for repeated jobs:** reviewing pull requests, watching releases, checking sites, a calendar, sorting mail, a weekly report. Use the create-assistant skill: it looks in the catalogs for one others share first (install it once approved), else asks, proposes, and makes it once approved. Each assistant keeps its own code and data in its folder, and can schedule tasks and use the shared wiki.
- **The assistants already here:** the folders at the top of the repository that have an `assistant.toml`. When a request belongs to one of them, hand it over with `"$AGENT_WORKBENCH_BIN" assistant send <folder> "…"` and say so.
- **A separate session:** "start a new session in my-app to fix the login bug". Use the start-session skill: the session is listed under its project, and the user continues it there.
- **Memory:** you remember what lasts in `shared/wiki/` at the top of the repository; the user can read it, and undo any update from the conversation's "Memory updated" note.
- **Anything else:** answer, plan, research, draft; open items go in `state/loops.md`.

A message that starts a sentence and stops ("Remind me in 2 hours to") came from a quick-start button: ask for the missing part in one short question.
