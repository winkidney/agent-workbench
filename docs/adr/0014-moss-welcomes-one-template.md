# ADR 0014: Moss welcomes the user; assistants come from one template and are backed up to GitHub

- Date: 2026-11-19
- Status: accepted; supersedes the mail template of 0010's follow-up

## Context

After trying assistants, the user asked for:

- a welcome from Moss in their language;
- a quick place to create routines;
- assistants that each keep their own code and data in their folder and sync through a private GitHub repository.

For a while the plan was to ship built-in assistants: code review, release watch, site watch, a calendar, and the mail template that v0.4.0 shipped. The user decided against built-ins. Agent Workbench offers one starting template. Moss creates assistants from it following the project's conventions, and the assistants the user wants live in their repository, not in the binary.

## Decision

1. **One template.** Every assistant starts from `assistant/each` (the personal assistant from its `Main` part). The template's `AGENTS.md` has these sections: how work arrives, Setting up, Procedure, Steps that need approval, Base capabilities, and Where things live. Every assistant but `main` gets a `.gitignore` for `raw/` (fetched data and bookkeeping). `main` keeps `raw/sessions/` in git.
   - The binary carries no other assistants.
   - The mail template and `--from` are gone.
2. **Conventions live in the create-assistant skill.** The shared skill sets out:
   - the layout and which files are in git;
   - the sections of `AGENTS.md`;
   - scripts: `uv run`, inline dependencies, a test each; silent when there is nothing new; non-zero on failure;
   - routines: off until set up, and the four routine shapes;
   - secrets: only in `~/.config/assistant/secrets/`;
   - the base capabilities.

   Moss follows the skill after the user approves a proposal. An assistant made in Settings starts with a `[Workbench: Set up]` message asking the user what it is for.
3. **Base capabilities.** Every assistant can:
   - schedule messages (`routine add`, ADR 0012);
   - read the shared wiki;
   - hand what lasts to Moss with `assistant send main "Remember: …"`.

   Only Moss (`memory = "wiki"`) edits the wiki. An assistant owns its folder and does not change another's.
4. **Language and welcome.**
   - Setup asks for a language: English, 简体中文 or 繁體中文, default 简体中文. It is kept in the `assistants` setting and named by a rule line in the root `AGENTS.md`. Settings changes both.
   - When setup makes Moss, the workbench sends it a `[Workbench: Welcome]` turn to write in that language. The turn covers Moss's skills, example phrases, the quick starts, the three ways to make a routine, new assistants with a few ideas, the assistants already in the repository, and Settings.
   - Settings can send the welcome again. Transcripts label these messages "Workbench · Welcome" and "Workbench · Set up".
5. **Creating a routine quickly.** A clock button in the sidebar header and Moss's "Schedule a routine" quick start open the routine dialog aimed at Moss.
6. **No restart.** `assistant reload` (`POST /api/local/assistants/reload`) applies an edited `assistant.toml` at once; otherwise it applies within a minute. Scripts run as fresh processes each time.
7. **Backup to GitHub.**
   - With a remote named `origin`, the scheduler pushes commits that the remote lacks, at most every two minutes. These are the workbench's own commits and those assistants make, pushed with `-u` the first time.
   - Settings → Assistants shows the remote, unpushed commits, the last push or its error, and a Push now button.
   - Without a remote it offers to make a private GitHub repository with `gh repo create --private --source … --push`.

8. **Memory less often.** The wiki is updated from a conversation when its topic ends (about once a day, or when the context fills, or on New topic) and only if it has new turns, or when the user asks: "remember …" (Moss changes the page at once), "update your memory" (Moss runs `assistant memory`), or ⋯ → Update memory now. A conversation going to sleep no longer starts an ingest, and Moss no longer changes the wiki on its own during a conversation.

9. **Credentials stay out of the repository.** Assistants keep credentials only in the user's home (`~/.config/assistant/secrets/`, mode 600) or use a tool's existing sign-in (`gh`, git, a CLI's login). That includes private URLs that work like passwords. A git pre-commit check, installed by setup and `ensureShared` but never over a hook of the user's own, refuses commits that carry them:
   - credential file names;
   - private keys and common token shapes;
   - OAuth fields;
   - URLs with a password.

   It names files and lines but never the values. It guards every commit: an assistant's, the workbench's, the user's. The root `.gitignore` leaves out the usual credential files. Only on the user's explicit request does a commit set `ASSISTANTS_ALLOW_SECRETS=1`.

10. **Order and on/off.**
    - Moss is always first.
    - The order the user drags the others into, by pointer (touch needs a long press) or Move up/down, is kept in the `assistants` setting.
    - `enabled = false` in an `assistant.toml` turns that assistant off: it is listed under Show more, its routines wait (a once routine records why it skipped), and messages to it are refused.
    - Moss cannot be turned off.
11. **The ⋯ menu is grouped:** this conversation's actions, then the workbench's.

## Consequences

- Agent Workbench keeps no assistant logic. The user's assistants are plain folders in their repository, where they can change them; they sync to other computers through the repository.
- Repositories set up by v0.4.0 and v0.4.1 keep their mail template if they made one. The skills and rules arrive through `ensureShared` only where they are missing.
- Pushing uses the user's git credentials. Under the systemd service, a credential helper that needs an unlocked keyring can fail. The failure is shown in Settings, and the user can push by hand.
