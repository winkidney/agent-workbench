# ADR 0009: Routines send messages and run commands on a schedule

- Date: 2026-11-19
- Status: accepted; a run can wait for the user's approval (0021)

## Context

Some work recurs: check the mail every half hour, write the week's report on Friday, ask a session how a long job is going each morning. The agents have timers of their own (Claude's `CronCreate`, `/loop`, `ScheduleWakeup`; see `schedule.go`), but they live inside an engine process: they end when it sleeps, is restarted or the workbench stops, and only Claude has them. A schedule that should outlast all of that has to belong to the workbench.

Much of this work needs no agent at all (a script that fetches something), and sending an agent a message just to learn that nothing happened costs tokens. "Automation" and "scheduled" already name other things in the interface (Codex `exec` sessions, Claude's own timers), so these are **routines**.

## Decision

1. **A routine sends a message, runs a command, or both.** With a message only, it sends it to a session. With a command only, it runs it and keeps what it printed. With both, the command runs first: if it fails, the run fails; if it prints nothing, nothing is sent (the run is skipped); otherwise the end of its output (4 KiB) is added to the message.
2. **Three schedules, in the computer's local time:** once at a time, daily at a time (on chosen weekdays), or every so many minutes (at least 5). No cron syntax: it covers what people ask for and needs no parser.
3. **A run the workbench misses is skipped, not made up.** When the workbench was not running or the computer slept, the next run is the next time due from now; a due time more than 5 minutes past is recorded as skipped. A once routine turns itself off after its time and is removed a week later.
4. **Messages go where a person's would.** To an existing session they go through the same queue as typed messages: sent at once when the session is free (waking its engine), queued behind a running turn otherwise. A routine can instead start a new session in a folder each time. The message starts with `[Routine: <name>]` and a blank line, so the agent, the transcript (which shows it as a label) and any later reader know nobody typed it just now.
5. **A run ends with its turn.** A message's run ends when the turn it started ends (done; failed if the turn failed, was interrupted, or the engine stopped first). Runs are kept a month, at most 50 per routine, with their status, exit code and the last 16 KiB of output; a run the workbench lost track of ends after a day, and runs open when the workbench stopped end as failed when it starts again (the queue lives in memory).
6. **Commands run like a terminal's, without one.** The command line runs with `sh -lc` (`cmd /c` on Windows) in its folder, in a process group (a Job Object on Windows) that is ended when it exits or its timeout (default 10 minutes, at most 60) passes, with the workbench's environment minus its service variables. A routine whose command is still running skips its next run.
7. **The same trust as terminals.** Any signed-in device may make routines, including command routines, as any signed-in device may open a terminal. Only the page makes command routines: a later way for agents to schedule their own reminders takes messages only, so text an agent read cannot leave a command running on a schedule.
8. **Quiet unless it matters.** A failed run notifies (desktop notification, while the page is in the background). A message's run notifies when the agent has answered; a command-only run notifies only when its routine asks to hear about output and it printed some.

## Consequences

- A schedule outlasts engines, sleep and restarts, and works the same for Claude and Codex. It still needs the workbench running: there is no catch-up.
- A command routine is a scheduled shell command with the workbench user's rights, reachable from every signed-in device, including through a tunnel. Settings → Routines lists every routine with its command and who made it.
- Output may hold secrets; it stays in the workbench's database for a month and is sent only to signed-in pages, never cached.
- A message queued behind a long turn waits for it, as a typed one would; a queued message is lost (and its run fails) if the workbench restarts first.
