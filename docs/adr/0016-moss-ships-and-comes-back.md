# ADR 0016: Moss ships with Agent Workbench and comes back from the user's repository

- Date: 2026-11-20
- Status: accepted; decision 1 extended by 0022 (Moss's tools are part of its definition)

## Context

Moss, the personal assistant, should work as soon as Agent Workbench is installed, and still be one binary. The maintainer also improves Moss by using it, in their own repository of assistants, and those improvements should come back into Agent Workbench. Symlinking the repository's files to a checkout would not ship in the binary. It would break on other computers once the repository syncs through GitHub, and it would let personal content reach a public repository.

## Decision

1. **Definition and data are apart.**
   - Moss's **definition** lives in `internal/workbench/assistant` and is embedded in the binary:
     - its procedure (`moss/AGENTS.md`, a plain file);
     - the shared rules (`root/AGENTS.md`, with a language placeholder);
     - the shared skills (`create-assistant`, `help`);
     - `shared/tools/secret.py`.
   - Moss's `assistant.toml` (name, agent, language, examples, routines), `state/`, `log.md`, `raw/` and the wiki are the user's and never leave their repository.
2. **Down, on every start.**
   - `updateMoss` compares each definition file with this version.
   - A file the user left as shipped is replaced with the new version.
   - A file they changed is kept and listed (`mossUpdates`). Settings → Assistants then offers "Use the new version" or "Keep mine". A declined version is not offered again; a later one is.
   - What was shipped is remembered as digests in the `moss` setting.
3. **Back, on request.** `agent-workbench moss export --to <checkout>` (or asking Moss) copies only the definition files into an agent-workbench checkout:
   - the language rule goes back to its placeholder;
   - a file that looks like it holds a credential stops it.

   The maintainer reviews `git diff` and commits there.

## Consequences

- One binary still installs everything, and Moss is ready after setting up.
- Improvements made while using Moss reach Agent Workbench through a reviewed commit, never automatically.
- A user who edits Moss keeps their edits across upgrades and decides when to take a new version.
