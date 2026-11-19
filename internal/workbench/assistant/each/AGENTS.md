# {{.Name}}


What this assistant does, in a sentence or two. (Write it with the user: see the create-assistant skill in `../.agents/skills/`.)

## How work arrives

- On request: a message in one of its sessions.
- On a schedule: its `[[routines]]` in `assistant.toml` (a message, a script, or a script whose output is handed to you).

## Setting up

What to ask the user the first time, step by step. Credentials (passwords, tokens, keys, private URLs) never go in this repository: the user saves them in `~/.config/assistant/secrets/` (mode 600), or the assistant uses a tool's existing sign-in; they are never given in the conversation. Run the check once, then set `enabled = true` on the routines, `ready = true` at the top of `assistant.toml` (with its `description`, `capabilities` and `examples` filled in, in the user's language), and run `"$AGENT_WORKBENCH_BIN" assistant reload`.

## Procedure

1. Read what you need: `state/loops.md`, `state/handoff.md`, and the inputs this task names.
2. Do the work. Use the scripts in `tools/` and the skills in `.agents/skills/` where they fit.
3. If a step needs approval, ask before it (see Approval in `../AGENTS.md`).
4. Report the result, update `state/`, and add a line to `log.md`.

## Steps that need approval

- (none yet: list them here, for example "sending anything to another person")

## Base capabilities

- **Scheduled tasks:** `"$AGENT_WORKBENCH_BIN" routine add --in 2h|--at "YYYY-MM-DD HH:MM"|--daily HH:MM [--days mon,fri]|--every 1d --message "…" [--to this|<assistant>]`; `routine list`, `routine rm <id>`. Messages only; a routine that runs a script goes in `assistant.toml`.
- **The shared wiki (LLM Wiki):** read `../shared/wiki/index.md` and the pages it points to when knowing the user matters. To keep something lasting (a preference, a fact about a project), hand it to the personal assistant, which keeps the wiki: `"$AGENT_WORKBENCH_BIN" assistant send main "Remember: …"`. Do not edit `../shared/wiki/` yourself.

## Where things live

Everything this assistant is stays in this folder: `assistant.toml` (settings, routines), `AGENTS.md` (this procedure), `config.toml` (settings its scripts read, no secrets), `tools/` (scripts, each with a test), `.agents/skills/` (procedures), `state/` (what is in progress: kept in git), `raw/` (fetched data and scripts' bookkeeping: not in git), `log.md`.


Shared rules: `../AGENTS.md` (read it if your CLI has not already).
