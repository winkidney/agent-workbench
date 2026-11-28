# ADR 0028: Install and restart from Settings → About

- Date: 2026-11-28
- Status: accepted

## Context

**Check for updates** in Settings → About only linked to the newest release. Updating meant downloading the archive for this platform, unpacking it and running its `install` in a terminal, which a workbench used from a phone or another computer cannot do.

## Decision

1. **`agent-workbench update` installs the newest release.**
   - It reads the newest release from GitHub, as Check for updates does.
   - It downloads this platform's archive and `checksums.txt` from that release (at most 200 MB) and checks the archive's SHA-256. On a mismatch it stops, having changed nothing.
   - It takes only the program (and on Windows its windowless twin) from the archive's top level, and checks the program prints the version it should.
   - It keeps the program in use beside it (`agent-workbench.previous`) and runs the new program's `install` for the same instance (data directory, name, address, and the variables the service was given). `install` stops the workbench, replaces the program and starts it again, as an upgrade by hand does.
   - If that install fails, or the workbench that answers afterwards is not the new version, it runs the kept program's `install`: the previous version comes back.
   - Each step goes to `update.json` (read by the About tab) and `update.log` in the data directory.
2. **Install and restart in the About tab runs it apart from the workbench.**
   - The workbench checks it can update itself, writes `update.json`, and starts `update` from a copy of its own program in the data directory (the installed program is replaced while it runs, and Windows cannot replace a running program).
   - The update must outlive the workbench it stops. On Linux under systemd it runs as a unit of its own (`systemd-run`): stopping the service ends every process in it. On macOS it runs in a session of its own, outside the launchd job's process group. On Windows it runs outside the workbench's job, without a console.
   - The workbench names the program it runs. The update replaces only that one; if the installed program is another copy, it refuses.
   - One update at a time: another request while one runs is refused, unless the running one has said nothing for ten minutes.
   - Before it starts, the page names the sessions working or waiting for the user and asks. With engines as systemd units they keep running; with child processes their current turns stop.
   - The page reads About again until the new workbench says how it went.
3. **Not everywhere.** A program a package manager installed (under `/usr` or `/opt`) is updated with that package manager, and a workbench not installed to start at login has nothing to restart into; both say so instead of offering the button. A development build cannot tell which release is newer.
4. **Only when asked.** Nothing checks or installs on its own.

## Consequences

- A workbench updates from any browser that can open its settings, including from another device that has access.
- The download is checked against the release's own checksums: that protects against a damaged or cut-off download, not against a release published by someone else on the same repository.
- A failed update leaves the previous version running, and says why in the About tab.
- macOS and Windows are covered by unit tests and the release's CI tests; the full path was exercised on Linux.
