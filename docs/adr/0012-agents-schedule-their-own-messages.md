# ADR 0012: Agents schedule their own messages and hand tasks to assistants through a local command

- Date: 2026-11-19
- Status: accepted

## Context

An assistant often knows that something needs a look later: a reply that may come tomorrow, a deadline next week. It also knows when a task belongs to another assistant. Without a way to act on this, the user has to set the routine or forward the message themselves. The CLIs' own timers (Claude's `CronCreate`) live inside an engine and end with it, so they do not suit anything further away than the session. The workbench is reachable from the internet through a tunnel in some setups, and requests forwarded by a tunnel on this computer arrive from loopback.

## Decision

1. **Commands, not tools.** The workbench binary takes `routine add | list | rm` and `assistant send`. Every engine the workbench starts has `AGENT_WORKBENCH_BIN`, `AGENT_WORKBENCH_DATA_DIR` and `AGENT_WORKBENCH_SESSION` in its environment, and the shared rules of the repository of assistants tell agents how to use them. Any agent that can run a shell command can use them, Claude or Codex, with no MCP server to keep.
2. **A separate local endpoint.** The command calls `/api/local/*` on the running workbench. A request must come from this computer, carry the token from `<data dir>/agent-token` (mode 0600, made anew at every start, separate from the stop token), and have none of `Cf-Connecting-Ip`, `X-Forwarded-For`, `Forwarded` or `Origin`. A tunnel or proxy on this computer adds those headers, and a page sends `Origin`. The path is handled before the host and sign-in rules, as the stop command's is.
3. **Messages only, within limits.** A routine an agent adds sends a message: there is no way to give it a command. It runs at most every 15 minutes, and a session has at most 20 switched on. It is marked `agent:<session>` (`agent:cli` from a terminal), and `routine rm` removes only such routines. The user's routines are changed in the page or in their file.
4. **"This" follows the topic.** `--to this` (the default) is the session the command runs in. In the folder of a continuous assistant it is the assistant, so a reminder for tomorrow reaches tomorrow's topic.
5. **Hand-offs are labelled.** `assistant send` gives the assistant the message as if it were typed, starting with `[From another session: <assistant>: <title>]`, so the receiving agent knows who sent it.

## Consequences

- Assistants keep their own follow-ups and pass work along. Settings → Routines shows which session made each routine, and the user can switch it off or delete it there.
- Anything running as the user on this computer can read the token and use the command, as it could read the workbench's data anyway. The token keeps out requests that arrive over the network.
- An agent tricked by outside content could schedule messages or message other assistants, but it cannot schedule commands. Background runs (ADR 0011) cannot run the command at all: Claude has no shell there, and Codex's sandbox has no network.
