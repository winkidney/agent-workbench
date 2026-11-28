# ADR 0031: Assistants install from catalogs; an assistant's definition and its owner's data are apart

- Date: 2026-11-28
- Status: accepted; amends 0010 (assistants are folders), 0013 (setting up) and 0015 (settings); extends 0016 (Moss's definition) to every assistant

## Context

An assistant was one folder in one repository: its procedure, scripts and skills beside the owner's state, log, records, config and choices, committed together. Sharing one meant copying the folder and taking the owner's data out of it by hand, and whoever took a copy never got the author's later changes. The owner's own repository held copies of definitions that were not theirs to keep.

## Decision

1. **Definition and data, by one rule** (`definitionFile`, defs.go). An assistant's definition is `assistant.toml`, `AGENTS.md`, `config.example.toml`, `icon.png`, `.gitignore`, `.claude/settings.json`, `tools/`, `.agents/` and `examples/`; everything else in its folder is the owner's (`settings.toml`, `config.toml`, `state/`, `log.md`, any records). Caches (`__pycache__`, `.pyc`) are neither. `CLAUDE.md` (`@AGENTS.md`) and `.claude/skills` (a link to `.agents/skills`) are made where an assistant is installed, so one procedure and one set of skills serve Claude Code and Codex alike; they are never distributed.
2. **The owner's choices are in `settings.toml`**, beside `assistant.toml`, and a key set there takes precedence: display name, agent, topics, on or off, set up (`ready`), icon, session options, and each routine's switch (`[routines."<name>"] enabled`). The page and the assistants write there; `assistant.toml` is the author's (home card, routines' commands and messages, defaults such as `ready = false`). An older procedure that still edits `assistant.toml` works, and that edit counts as the owner's change. `agents` in `assistant.toml` names the agents its author made it for (both when left out).
3. **A catalog is a git repository of assistants:** each top-level folder with an `assistant.toml`. The default is `winkidney/agent-workbench-assistants`; the user can add others. The workbench keeps a clone of each in its data directory. **Add from a catalog** (Settings → Assistants) lists them and installs one: its definition files, then `config.toml` from `config.example.toml`, empty state and log, the agent chosen in `settings.toml`, and a commit. Its routines stay off until it is set up. The page says that its scripts will run on this computer, and, for Codex, when its Claude settings deny it tools that no Codex sandbox replaces.
4. **The lock file and the repository of data.** `assistants.lock.toml` records each installed assistant's catalog, folder there, commit and the digest of each definition file. A definition file whose content matches is listed in `.git/info/exclude` (not committed, and removed from the index if it was); a file the owner changed is committed as theirs. This is refreshed on every commit the workbench makes and at start. A repository cloned on another computer gets the missing definitions back from the catalogs at the locked commits, at start and when assistants are set up there; an assistant whose catalog cannot be reached says so.
5. **Binding an older repository.** "Bind my existing assistants to this catalog" first shows, per assistant the catalog also has, which files match (they leave the repository), which differ (kept as the owner's), which are added, and which settings move out of `assistant.toml`. `assistant.toml` becomes the catalog's only when what differs is the owner's settings. It refuses a repository with uncommitted changes, and leaves Moss alone (0016). A file can be taken from the catalog instead of kept.
6. **Phases.** Updates (a newer catalog commit, shown first, the owner's changed files kept or replaced file by file) and Publish (copying an assistant's definition into a checkout of a catalog, checked for personal data) follow in their own changes.

## Consequences

- The owner's repository holds their data, their choices, their changes to definitions, and a lock: what is theirs to back up.
- Sharing an assistant is publishing a folder of definition files; others install it, and the owner of each copy keeps their data apart from it.
- Installing runs someone else's scripts: routines stay off until set up, and the page says so; nothing verifies a catalog's author beyond its git address.
- Moss and the shared rules still come with Agent Workbench.
