# ADR 0006: A terminal tab lives until it is closed

- Date: 2026-11-18
- Status: accepted

## Context

Checking something an agent did often needs a shell in the session's folder: run the tests, look at `git status`, start a command by hand. The workbench had none, so this meant another program, and from another device an SSH session. The preview panel already holds what a session opens, as tabs.

Tabs there are light on purpose: the six most recently opened are kept, and a tab's page loads while it is shown and is gone when it is not. A shell cannot work that way. What runs in it (a build, a server, an editor) must keep running whether or not its tab is shown, the page is reloaded or the device changes.

## Decision

1. **A terminal is a tab in the preview panel.** **New terminal** after the tabs starts a login shell in the session's folder, on a pseudo-terminal: creack/pty on Linux and macOS, ConPTY (UserExistsError/conpty) on Windows. The page draws it with xterm.js and attaches over a WebSocket (coder/websocket).
2. **Only an explicit close ends it.** Closing its tab (asked first while its shell runs) ends the shell and everything in its session, nohup'd programs included; on Windows its Job Object. Hiding the panel, switching tabs or sessions, reloading, or opening more tabs never ends it: terminal tabs are kept besides the six files and services. A terminal also ends when its shell exits, its session is deleted, or the workbench stops; its tab then says so.
3. **Resources are taken as needed and all given back when it ends.** With no terminal nothing runs. A terminal holds its process, the pseudo-terminal, one reader and its output history, which grows with output up to 1 MiB. In the page, the xterm code loads when a terminal is first shown, and a view (xterm, socket, observers) exists only while its tab is shown; attaching again replays the history. Ending a terminal frees all of it; an exited one is remembered only by its exit status until its tab closes.
4. **Same access as the agents, with its own safeguards.** Any signed-in device may open terminals, the same rule as sending tasks. The WebSocket upgrade checks the page's origin, since browsers send cookies with cross-site WebSocket requests. Each socket ends as soon as the sign-in that opened it does. Output stays in memory only, and input is never logged. Opens and ends are recorded (time, session, terminal, device as "this computer" or a one-way mark of the sign-in) for 7 days and shown, collapsed until opened, in Settings → Access & security.

## Consequences

- A long build or server survives the page and the device; it ends only when you close it, or with the workbench, since terminals are its children. After a restart their tabs say they ended and offer a new terminal there.
- A terminal is a shell with the workbench user's rights, reachable from every signed-in device, including through a tunnel. Signing devices out or narrowing access ends their sockets at once.
- Up to 8 terminals per session and 32 in all, each at most 1 MiB of history in memory; a page that falls behind is dropped and reconnects.
- Windows needs ConPTY (Windows 10 1809 or later).
