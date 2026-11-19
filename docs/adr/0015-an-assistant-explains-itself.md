# ADR 0015: An assistant explains itself

- Date: 2026-11-19
- Status: accepted

## Context

Opening a new assistant showed a name and an empty box. Nothing said what the assistant does, what it can do, how to ask it, or whether it was set up, and its settings were scattered or only in files.

## Decision

1. **Metadata in `assistant.toml`.**
   - `description` is one line.
   - `capabilities` and `examples` are lists of at most 12 one-line items of at most 200 characters each.
   - `ready` says whether setup is done; a file without it counts as set up.

   Moss writes them when it creates an assistant (the create-assistant skill), in the user's language. The set-up conversation fills them in and sets `ready = true`. The personal assistant gets its own in the setup language. The rules ask assistants to keep them true.
2. **A home card** (`assistants/AssistantHome.tsx`) shows:
   - name, agent, topic mode and on/off;
   - the description, and what it can do;
   - example requests that fill the composer;
   - its routines and their state;
   - "Not set up yet" with **Set up** (`POST /api/assistants/{dir}/setup`, which sends `[Workbench: Set up]`);
   - Settings, and its procedure.

   It is shown above a new request, and from an ⓘ button in the assistant's conversations.
3. **Per-assistant settings** (`assistants/AssistantSettings.tsx`) cover:
   - name, description, capabilities, examples, agent, topics and on/off;
   - its routines, with switches and Run now;
   - its files, which open in the preview panel.

   Saving `PATCH`es only the changed top-level lines (`PatchAssistant`: strings and string arrays as TOML, missing keys added, the result checked before it replaces the file, then committed). A change of agent or topics starts a new topic for a continuous assistant.
4. **The sidebar menu** offers:
   - Settings…;
   - Set up… while not set up;
   - the agent (Claude or Codex, an uninstalled one disabled; `installed` in `GET /api/assistants`).

   A row says "set up" until its assistant is ready. The highlighted row is the assistant open now, whether its new request or one of its sessions.

5. **What it needs, and sign-ins outside the conversation.**
   - `requires` lists what an assistant needs from the user, each item with the ways it can be given (a signed-in CLI, an MCP server, an API token, an account). The card shows it.
   - Setup is a wizard: one question at a time, with the choices offered (AskUserQuestion for Claude, a numbered list for Codex).
   - Every sign-in or secret is done by the user outside the conversation:
     - `uv run ../shared/tools/secret.py set <name>` asks with hidden input and saves with mode 600;
     - the tool's own login (`gh auth login`, `tools/<x>.py auth`) opens the page and records the result;
     - `secret.py open <url>` opens a sign-in page.

     The assistant checks with `secret.py check`, which never prints a value. It never asks for, repeats or stores a secret, code or callback URL in the conversation. `shared/tools/secret.py` is a shared file, so older repositories get it.

## Consequences

- An assistant tells the user what to do with it, and its card is a plain file the user or the assistant can change.
- Assistants made before this have no metadata. Their card says so and offers Settings; the user's repository was given metadata by hand.
