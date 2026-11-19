# ADR 0013: Assistants are set up on first use; each picks its agent; Moss makes new ones

- Date: 2026-11-19
- Status: accepted; amends 0010

## Context

ADR 0010 left setting up assistants to a form in Settings → Assistants. That form asked for one agent, which made it look as if every assistant had to use the same one. The personal assistant was called "Assistant". A new assistant meant a form, then a procedure written by hand. People trying it for the first time did not find assistants, and did not find out that Moss could do most of this for them.

## Decision

1. **Offered where they are used, set up on confirmation.** Until assistants are set up, the sidebar shows an Assistants section with one row: "Moss · Set up". That row, Set up… in Settings → Assistants, and "Set up assistants…" in the routine dialog all open one dialog. It says what setting up makes and asks for the name (Moss) and the agent, with the folder (`~/assistants`) under a disclosure. Confirming sets everything up and opens Moss. Nothing pops up on its own.
2. **Defaults.** The agent is Claude Code if it is installed, else Codex (`defaultAgent`, also returned by `GET /api/assistants`). With neither installed, setup says so. Empty fields take these defaults.
3. **Each assistant has its own agent and name, changed in the page.** Settings → Assistants has a Claude/Codex switch and a rename on every row (`PATCH /api/assistants/{dir}`). They change only the top-level `name` and `agent` lines of `assistant.toml`: the first such line before the first table, so a routine's `name` is never touched. The file is checked before it replaces the old one, and the change is committed. A continuous assistant that switches agent starts a new topic, because its ongoing session belongs to the old agent.
4. **Moss by name.** The personal assistant is named Moss unless the user names it otherwise. Its procedure says its name is the `name` in `assistant.toml`, so a rename needs no other edit.
5. **New assistants by skill.** A shared skill, `.agents/skills/create-assistant/` at the top of the repository (found by Claude Code through the `.claude/skills` link, and by Codex up to the git root), tells an agent to:
   - find out the job;
   - propose the assistant (name, agent, topics, procedure, scripts, routines, steps needing approval) and wait for approval;
   - create it with `agent-workbench assistant new` (`POST /api/local/assistants`, under ADR 0012's rules), which never replaces a folder;
   - write its procedure and scripts, and commit.
6. **Older repositories catch up.** When the scheduler starts, `ensureShared` writes the shared files a repository lacks (such as this skill) and commits them as "chore: add shared files from Agent Workbench". It never changes a file that is there.

## Consequences

- Assistants are found in the sidebar, and the first click explains them.
- Assistants on Claude and on Codex work side by side; switching one costs its ongoing conversation, not its files.
- An agent can now make assistants. It only adds folders, after the user approved the plan, and the result is committed and can be undone like any change.
- A repository set up by v0.4.0 keeps the name "Assistant" until the user renames it.
