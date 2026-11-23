---
name: start-session
description: Start a separate Agent Workbench session for a piece of work, which the user then opens and continues from the sidebar. Use only when the user asks for one ("start a new session for this", "open a separate session to fix X", "开个新会话处理…"); never on your own, and not for your own sub-tasks (use your own subagents for those, as before).
---

# Start a session

The session is an ordinary one in Agent Workbench: it is listed under its project in the sidebar, and the user opens it and carries on there. It is not yours: you do not watch it or get its replies.

1. **Where.** The folder it works in, usually a project the user named. If it is not clear which, ask in one short question. Not this repository of assistants: work for an assistant stays with that assistant.
2. **What.** Write its first message so that it stands alone: the task, what you already know (paths, errors, links, decisions made), and when it is done. The new session sees nothing of this conversation.
3. **Start it:**

   ```sh
   "$AGENT_WORKBENCH_BIN" session new --folder <dir> [--agent claude|codex] [--title "<short title>"] "<first message>"
   ```

   The agent defaults to yours, and the session gets the user's settings for new sessions. Quote the message; it starts with a line saying it came from you.
4. **Tell the user** its title and the project it is under in the sidebar, in one line.

If the command fails, say why in one line and offer to try again, or the user can start one with **+** in the sidebar.
