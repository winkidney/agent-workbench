# Using Agent Workbench

Open **http://agent.localhost:17880** (or this machine's IP address or host name from another device on your network). The sidebar lists your sessions, the middle shows the conversation, and the right panel shows files and previews. Both side panels can be hidden, and dragged to resize.

## Sessions

The workbench works with the sessions the CLIs keep themselves. Every Claude Code and Codex session on the machine appears in the sidebar, whether it was started here or in a terminal, and the native session ID is its identity.

- **Continue anywhere.** Open a session and keep going, or continue it in a terminal: the "⋯" menu copies the resume command (`claude --resume <id>` / `codex resume <id>`). Whatever the CLI adds there shows up here.
- **Running elsewhere.** A session another client is using is shown read-only and followed live: new records appear as the CLI writes them, with the turn's elapsed time. Replies arrive in whole blocks rather than word by word, and that client's approvals cannot be answered here. Once the other client lets go, you can continue the session here.
- **Engines sleep.** Each session's agent stays running between turns. After 5 idle minutes (see `--idle`) it is put to sleep; the next message resumes the same session after a few seconds. A session with work in progress (a running tool, a pending approval, a dev server) is not put to sleep.
- **End a session** from the header menu to stop its agent and everything it started. Files and history stay.

### Starting a session

Press **+** in the sidebar (or the **+** on a project row). The conversation area turns into a new-session form:

1. Choose **Claude** or **Codex**.
2. Choose the project folder: recent projects are listed (pinned ones first), you can type to filter or type a path starting with `/` or `~` (Tab completes), or use **Browse folders…** to pick a folder in your home directory.
3. Optionally set model, effort, mode and permissions.
4. Type the first message and press Enter. The CLI creates its native session and you are switched to it.

The title comes from the first message until the CLI names the session; rename it from the "⋯" menu.

## The composer

| Key | While idle | While a turn runs |
| --- | --- | --- |
| Enter | send | **steer**: send into the running turn |
| Shift+Enter | new line | new line |
| Queue button | | keep the message for after this turn |
| Esc | | interrupt the turn |
| `/`, `$` | open the command or skill list | |

**Steering.** A message sent during a turn goes into that turn and is marked *Steered*:
- **Codex** adds it to the active turn (`turn/steer`); the model reads it at its next step.
- **Claude** takes it in at its next step, after the current tool call, without interrupting, exactly as pressing Enter in Claude Code's terminal UI does. If the turn finishes first, the message runs right after it. While it waits it shows **Send now**, Claude Code's own send-now (Ctrl+X Ctrl+S): a running tool moves to the background, or the reply in progress stops, and the message is handled at once.

Commands (`/…`) cannot steer; they run after the turn. If an approval is pending, a steered message is queued instead and the reason is shown.

**Queue.** Queued messages wait above the composer and are sent one after another as turns end. Each has **Steer** (send it into the running turn now; **Send now** when nothing is running) and a remove button. After an interrupt or an error the queue pauses until you continue it.

**Settings pills.** Model, effort, speed, mode and permissions sit under the input. Each shows the value in effect: your choice for this session, else what the running agent reports, else the CLI's configured default (shown muted). Changes apply from the next turn. The usage menu can save the current choices as defaults for new sessions without changing the CLI's own configuration.

### Commands and skills

Type `/` or `$` to open a list that filters as you type (prefix matches first, aliases included). Use ↑/↓ to choose, Tab or Enter to complete, Esc to close.

- **Both agents:** `/plan [task]` and `/default [task]` switch plan mode (and send the task if given); `/model`, `/effort`, `/permissions` and `/fast` change the matching setting.
- **Claude:** the list is Claude Code's own command list (terminal-only commands left out). Commands are sent as text and run by the CLI; output of local commands such as `/context` is shown formatted. A `$skill` goes at the start of a message and is sent as `/skill`.
- **Codex:** skills come from the CLI; `$skill` can appear anywhere, several times. The Codex TUI commands that make sense here are mapped to the app-server: `/compact`, `/review [instructions]`, `/goal [text|clear]`, `/rename`, `/fork` (opens the new session), `/mcp`, `/diff`, `/quit`; `/new`, `/status`, `/usage` and `/skills` are handled by the page. Unknown commands are refused rather than sent to the model; start with `//` to send a literal `/`.

### Approvals and questions

Permission requests appear as cards in the conversation: **1** allow, **2** allow for this session, **3** deny and tell the agent what to do instead; arrow keys and Enter work too, Esc denies. Questions (Claude's AskUserQuestion, Codex's input requests) are answered by choosing options. Claude's plan confirmation shows the plan. Requests the workbench does not know are shown as they are, and can be denied or interrupted.

## The conversation

- Replies are Markdown. Tool calls, thinking and subagents collapse to one line (`● Bash(npm test)`), with a short result below (`⎿ 2 failed, 31 passed`, `+3 −1`); click to expand the command, diff, full output or the raw record. **Ctrl+O** expands or collapses all.
- Colors follow each CLI's default terminal theme: Claude Code's for Claude sessions, Catppuccin (as the Codex TUI uses) for Codex sessions. Code blocks with a language are highlighted; ANSI colors in tool output are kept.
- The to-do list (Claude's TodoWrite, Codex's plan) stays pinned above the composer.
- The status line shows what the turn is doing (Thinking, Running a tool, Waiting for the model, …), its elapsed time, output tokens and speed. The ring next to it is context usage; the line below opens usage details and quota windows.
- History loads newest first; scroll to the top for older messages.

## Files and previews

![The storefront's index.html previewed in the right panel next to the conversation](screenshots/preview.png)

- **Paths in replies** are links. A relative path is resolved from the directory the agent was in when it wrote it, then from the session's launch directory and the subprojects it worked in. Only files inside the launch directory open.
- **Images and videos** open in a viewer: zoom with the wheel or a pinch, drag to pan, double-click to switch between fit and 1:1 (`+`, `-`, `0`, `1`), and use ←/→ for the other media in the same message or folder. Videos stream with seeking.
- **HTML, PDF and SVG** open in the right panel, on an isolated preview origin.
- **Dev servers:** the globe button in the right panel attaches an existing loopback URL, or starts a command you enter on a fixed port, for example `npm run dev -- --host 127.0.0.1 --port 5173 --strictPort`. A server the workbench started stops with the session. Loopback URLs in replies open in the panel too.
- **Show in Files** reveals a file in the desktop file manager of the computer running the workbench.

HTML and dev-server previews use `p-*.agent.localhost` addresses, so they work in a browser on the machine running the workbench; other devices can still read conversations, send messages and view images and videos.

## The sidebar

- **Agent icons:** the coral burst is Claude, the `>_` tile is Codex. At the right end of each row: a spinner while running, a hand while waiting for approval, a monitor when another client is using it, a triangle on error, a dot while the engine is loaded, otherwise the time of the last activity.
- **Active** lists sessions that are running, waiting, loaded or in use elsewhere. **Pinned** keeps the sessions you pin, in pin order.
- **Projects** groups sessions by the directory they were started in (moving elsewhere inside the session doesn't change that), nests subdirectories, and folds shared path prefixes into one folder. A session that worked across several subprojects shows them on a second line (`↗ api, web`) and is listed under each of them as related.
- **Other** holds sessions that are not project work: `codex exec` runs, temporary directories, and chats from the home directory or the Codex app.
- **Search** (press `/`) matches titles, projects and agents; separate words with spaces.
- **Hover a row** (or focus it with the keyboard) to show its pin button and "⋯" menu: rename, copy the resume command, copy the session ID, and
  - **Codex:** Archive / Unarchive and Delete. Both use Codex's own thread operations, so the Codex app and CLI see the same change. Delete asks first and is permanent. Archived sessions open read-only with an Unarchive button; **Archived N** at the bottom shows them.
  - **Claude:** Claude Code has no archive or delete for local sessions, so the menu offers **Hide from sidebar**, which never touches its records. **Hidden N** at the bottom shows hidden sessions.

Pins, hidden sessions and display names are stored by the workbench and sync live between browsers.

## Network access and pairing

The workbench listens on all interfaces by default (`0.0.0.0:17880`) and `start` prints its LAN addresses.

- From this machine and private networks (LAN, link-local, and the `100.64.0.0/10` range Tailscale uses), any IP address, `localhost`, single-label host names, `.local`, `.lan`, `.home`, `.home.arpa`, `.internal`, `.localdomain` and this machine's host name work. Whether a login is needed depends on the pairing switch.
- Any other domain name always needs the pairing code, because any website can point its domain at your machine (DNS rebinding). Without a pairing code set, the page suggests using an IP address or local name instead.
- From public addresses only `agent.localhost:PORT` is accepted, always with login.

**Pairing** is off by default. Turn it on in **⋯ → Settings**: the settings show the code, other browsers must enter it, and the current browser stays signed in. The setting and code survive restarts. Turning pairing off again lets local devices in directly.

`--listen 127.0.0.1:17880` keeps the workbench to this machine.

## Appearance

Choose light, dark or system in the header's **⋯** menu. The interface adapts to phones: the sidebar and the right panel slide over the conversation.
