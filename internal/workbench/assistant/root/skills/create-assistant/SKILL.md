---
name: create-assistant
description: Create a new assistant in this repository for a job the user wants done the same way again (a report, a check, a triage). Use when the user asks for a new assistant, or a recurring job deserves its own procedure, tools and schedule.
---

# Create an assistant

An assistant is a folder in this repository with `assistant.toml` (name, agent, topics, routines), `AGENTS.md` (its procedure), `tools/` (scripts), `.agents/skills/` and `state/`. Agent Workbench lists it in the sidebar and runs its routines.

## 1. Understand the job

Ask only what you cannot tell from the conversation:

- What it does, from what inputs, producing what, and for whom.
- When: on request only, or on a schedule (daily at a time, every so often).
- Which steps must wait for the user's approval (anything sent to other people, money, deletions).
- Claude or Codex (default: the agent you are), and whether every request is a new session (`per-task`, most jobs) or it keeps one ongoing conversation (`continuous`).
- For mail: the `mail` template already reads mail over IMAP and sorts it.

## 2. Propose it and get approval

Before creating anything, show the plan in a few lines: name and folder, agent, topics, the procedure's steps, the scripts it needs, its routines (schedule and message; say plainly if a routine runs a command), and the steps that need approval. Then ask:

- Claude: AskUserQuestion with the options Approve, Change and Reject.
- Codex: end your turn with a line that starts `Needs your approval:`.

Do not go on without approval. On Change, revise and ask again.

## 3. Create it

```sh
"$AGENT_WORKBENCH_BIN" assistant new --name "Weekly report" [--folder weekly-report] [--agent claude|codex] [--sessions per-task|continuous] [--from mail]
```

It makes the folder from the template, commits it and prints its path. It never overwrites a folder that exists.

## 4. Fill it in

In the new folder:

- `AGENTS.md`: replace the placeholder with the approved procedure: what it does, numbered steps, the inputs it reads, where it writes results, and the steps that need approval.
- `tools/`: scripts for the deterministic parts (fetch, convert, check), Python run with `uv run` and dependencies declared inline, each with a test. A script that finds nothing new prints nothing.
- `.agents/skills/<name>/SKILL.md`: a procedure worth reusing on its own.
- `assistant.toml`: the approved `[[routines]]` (see the comments in the file). A routine with a `command` only if the approved plan said so.
- Credentials: never in the repository. Scripts read them from `~/.config/assistant/secrets/`; tell the user which file to create and how, and never ask for the secret itself.

## 5. Finish

Commit the folder: `git add <folder> && git commit -m "Set up <name>"`. Tell the user it is in the sidebar under Assistants, what to try first, and anything they must do themselves (a password, switching a routine on). Add a line to your own `log.md`.
