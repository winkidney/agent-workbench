# ADR 0017: Assistants choose their sessions' model, effort, mode and permissions

- Date: 2026-11-20
- Status: accepted

## Context

A session started in the page has pills for the model, effort, mode and permissions. A session an assistant starts didn't have them. The request screen hid them. Every assistant session (requests, routines, `assistant send`, the welcome, a new topic) started with Settings → Agents' defaults. So Moss, a code reviewer and a mail sorter all ran with the same model and effort, and a request couldn't ask for more.

## Decision

1. **Each assistant has its own settings, per agent, in its `assistant.toml`:**

   ```toml
   [options.claude]
   model = "opus"
   effort = "high"

   [options.codex]
   model = "gpt-6.2-sol"
   effort = "high"
   ```

   - `model`, `effort`, `mode` and `permissions`, all optional.
   - Each agent has its own table, so switching the agent keeps both tables.
   - The file syncs with the repository, and the assistant can change its own settings (then `assistant reload`).
2. **Layers.** A session an assistant starts takes:
   - the request's own choice;
   - over the assistant's table for its agent;
   - over Settings → Agents.

   This covers requests, routines, `assistant send`, the welcome, the set-up conversation and new topics. A message that joins the ongoing topic keeps that session's settings; the composer changes them there.
3. **The request screen shows the pills**, starting from the assistant's settings (`GET /api/assistants/{dir}/options`). The request sends them with the message. **Save as <name>'s default** writes the changed ones into the table.
4. **The assistant's Settings dialog** shows the pills for the agent picked there; "Default" leaves a setting to Settings → Agents. Saving sends `PATCH /api/assistants/{dir}` with `options: {<agent>: {…}}`, which replaces that table and keeps the rest of the file.
5. **Background runs take only the model and effort** (`--model`/`--effort`; Codex `-m`/`-c model_reasoning_effort`). Their limits never loosen with the assistant's mode or permissions: Claude keeps only the file tools; Codex keeps the workspace sandbox.

## Consequences

- The model names are each CLI's own and are not checked against its list. A model a CLI doesn't know fails when the session starts, as it would in the page.
- Speed stays a per-session choice.
