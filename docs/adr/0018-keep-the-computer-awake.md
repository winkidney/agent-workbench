# ADR 0018: Two switches keep the computer awake while the workbench runs

- Date: 2026-11-21
- Status: accepted

## Context

Agents use the display of the computer the workbench runs on: browsers with a window, screenshots, game engines. When that computer locks its screen after some idle minutes, they have no display left, and the work falls back to a virtual one (Xvfb), which costs CPU and does not behave the same. Turning sleep off in the system settings does not stop the screen from locking.

## Decision

1. **Two switches in Settings → General, under "This computer":** Prevent system sleep, and Prevent screen lock. Both are **off by default**. They are kept with the other preferences (`PATCH /api/preferences` with `preventSleep`, `preventLock`), and apply to the computer the workbench runs on, whichever device changes them.
2. **No guess about work.** While the workbench runs and a switch is on, the hold stays, whether or not an agent is working. The user decides with the switch. Turning it off, or stopping the workbench, releases it.
3. **Holds that end with the process:**
   - Linux, sleep: a logind `block` inhibitor (a file descriptor), plus the session's `org.freedesktop.PowerManagement` inhibition where the desktop has one (KDE).
   - Linux, screen lock: `org.freedesktop.ScreenSaver.Inhibit`, which the screen locker serves (KWin on Plasma), else the GNOME session manager's idle inhibition. Both are tied to the bus connection that asked, so each hold keeps its own connection.
   - macOS: `caffeinate -i` (sleep) and `caffeinate -d` (display, which starts the screen saver and the lock), each with `-w <workbench pid>`.
   - Windows: `SetThreadExecutionState` with `ES_SYSTEM_REQUIRED`, plus `ES_DISPLAY_REQUIRED` for the screen, from a goroutine locked to its own thread.

   A crash releases them all, since the descriptor, the connection or the child goes with the process.
4. **What the page shows:** under each switch, how the computer holds it now (`On · via logind + desktop power management`), or why it could not (no session bus, a desktop with no inhibition). The sweep every 15 seconds takes a lost or failed hold again, for example after the user signs in again.
5. **D-Bus through `github.com/godbus/dbus/v5`**, the library most Go programs use for D-Bus (containerd, go-systemd). It adds no cgo.

## Consequences

- With Prevent screen lock on, a computer left alone stays unlocked while the workbench runs. The hint says so; the default stays off for that reason.
- A workbench without a desktop session (a server over SSH) has no session bus: Prevent screen lock shows the error, and Prevent system sleep still works through logind.
- Each workbench instance holds its own; two instances with a switch on show two entries in the desktop's list.
