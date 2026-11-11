# Architecture

Agent Workbench is one Go program (`cmd/agent-workbench`) with the React interface (`web/`) embedded through `go:embed`. `internal/workbench` holds the server: engine control, the native catalog, persistence, HTTP and previews.

```text
browser ──HTTP + SSE──▶ agent-workbench ──stdin/stdout JSON──▶ claude -p … / codex app-server
                              │                                      │
                              ├── SQLite (index, settings,           └── native records
                              │   event cache)                           (~/.claude, ~/.codex)
                              └── preview proxy ──▶ loopback dev servers
```

## Engines

Each session's agent is the official CLI, started in the session's launch directory and controlled over stdin and stdout.

- **Claude Code:** `claude -p --input-format stream-json --output-format stream-json --verbose --include-partial-messages --permission-prompt-tool stdio` (plus `--resume <id>` for an existing session). Messages are written as user records; settings change through control requests (`set_model`, `set_permission_mode`, `apply_flag_settings`); permission prompts arrive as `can_use_tool` control requests. A message written during a turn waits at the CLI and is taken in at the next step; `interrupt` with `send_now` asks for it at once.
- **Codex:** `codex app-server`, the JSON-RPC protocol of the Codex IDE integrations. `thread/start` / `thread/resume`, `turn/start` (with model, effort and collaboration mode), `turn/steer`, `turn/interrupt`; approvals arrive as server requests. Commands map to `thread/compact/start`, `review/start`, `thread/goal/*`, `thread/fork`, `mcpServerStatus/list`, `thread/archive`, `thread/delete`, and so on.

An engine stays running between turns and is put to sleep after the idle timeout when nothing is in progress (no turn, approval, queued message, extra process or managed preview); the next message resumes the native session. Metadata (models, commands, skills, quotas) comes from short-lived probe processes that never create a session or call a model.

### Processes

- When the workbench runs as the systemd service `agent-workbench.service` (Linux, `start` or `install`), each engine is a transient unit (`agent-workbench-session-<id>.service`) with `KillMode=control-group` and `BindsTo=agent-workbench.service`. "Busy" means the unit's cgroup has processes the engine did not have when it became ready.
- Everywhere else (macOS, Windows, Linux without a systemd user manager) an engine is a child process. Unix keeps it and everything it starts in a process group (descendants that leave the group are tracked through the process table); Windows uses a Job Object with kill-on-close. Stopping a session signals the whole tree and kills what is left after ten seconds.
- Managed preview servers follow the same rules.

## Native records

The CLIs' records are the source of truth; the workbench never writes them.

- **Codex:** thread metadata is read from `$CODEX_HOME/state_5.sqlite` (read-only); history is read through a short-lived app-server (`thread/turns/list`). Writer locks (`thread-writer-locks/<id>.lock`) tell whether another client holds a thread.
- **Claude Code:** history is read from `$CLAUDE_CONFIG_DIR/projects/<project>/<session>.jsonl`, following the parent-child chain of the newest branch. `sessions/<pid>.json` (never the credential files) tells whether another Claude process holds a session and whether it is busy.
- A session's project is the directory it was **started** in: Codex's thread directory, Claude's first recorded `cwd`.
- Sessions another client is writing are followed by reading only the bytes appended since the last read.

## Store and events

A SQLite database (WAL) in the data directory keeps the session index, settings (pairing, sidebar pins, per-session options) and a short event cache. Events are kept 7 days, at most 2000 per session, and pruned at start and hourly; progress-only signals are never stored. The page receives events over server-sent events (`GET /api/events`) and reconciles each finished turn with the native record.

On start the server writes `server.json` (pid, address, version, mode, and a one-time token, mode 0600) to the data directory; `agent-workbench stop` uses the token with `POST /api/local/shutdown`, which only accepts requests from this machine. The file is removed on exit.

## Previews

The interface runs at `agent.localhost:PORT`. Each preview gets its own random origin, `p-<id>.agent.localhost:PORT`, which proxies a file in the session directory or a loopback HTTP server (including WebSockets). A preview is authorized by a one-time POST that sets a cookie for that origin only; the proxy never forwards the workbench's cookies or `Authorization` header upstream. The media endpoint serves raster images (up to 32 MiB) and videos with Range support, after sniffing the content; SVG and HTML are only served on preview origins.

## Security model

- **Who can connect** depends on the address the request comes from and the `Host` it names:
  - this machine and private networks (LAN, link-local, `100.64.0.0/10`): IP addresses, `localhost` and `*.localhost`, single-label names, `.local`, `.lan`, `.home`, `.home.arpa`, `.internal`, `.localdomain` and the machine's host name are accepted; a pairing code is required only when pairing is on;
  - any other domain name always requires the pairing code (DNS rebinding);
  - public addresses: only `agent.localhost:PORT`, always with login.
- **Sessions** use a host-only, HttpOnly cookie after pairing; repeated wrong codes are rate-limited.
- **Changes** (`POST`, `PUT`, `PATCH`, `DELETE`) must carry an `Origin` equal to the requested host.
- **Files:** the interface opens files only inside a session's launch directory; `..` and symlinks that lead outside are refused. The folder browser for new sessions lists directories inside the home directory only.
- **Agents** keep their own permission systems: approvals are forwarded to the page, never answered automatically; unknown control requests are declined.
- The workbench is not designed to be exposed through a public tunnel. That would need HTTPS origins and a preview domain of its own.

## HTTP API

The interface uses a JSON API under `/api/`. Main endpoints:

| Endpoint | Purpose |
| --- | --- |
| `GET /api/sessions` | All sessions (native and managed) with status |
| `POST /api/sessions` | Create a session: `{agent, cwd, initialMessage, options?, requestID}` |
| `GET /api/history?session=&limit=&before=` | A page of native history, newest first |
| `GET /api/events` | Server-sent events |
| `POST /api/sessions/{id}/send` | `{text}`; `queue: true` queues during a turn, `steer: true` steers |
| `POST /api/sessions/{id}/queue/{qid}/steer`, `DELETE …/queue/{qid}`, `POST …/queue-resume` | The queue |
| `POST /api/sessions/{id}/send-now` | `{uuid}`: Claude's send-now for a waiting steer |
| `POST /api/sessions/{id}/interrupt`, `/sleep`, `/stop`, `/resume` | Turn and engine control |
| `POST /api/sessions/{id}/reply` | Answer an approval: `{id, decision, message, answers}` |
| `GET /api/sessions/{id}/commands`, `POST …/command` | The command and skill catalog; run a command |
| `POST /api/sessions/{id}/rename`, `/archive`, `/unarchive`, `/delete` | Session management |
| `GET /api/sessions/{id}/telemetry`, `PATCH …/options` | Usage and settings |
| `GET /api/agents/{agent}/capabilities?cwd=`, `/usage`, `GET/PUT …/defaults` | Models, modes, quotas, default settings |
| `GET /api/media?session=&path=&base=` | Images and video |
| `POST /api/previews`, `GET /api/previews` | File and server previews |
| `POST /api/sessions/{id}/reveal` | Show a file in the desktop file manager |
| `GET/POST /api/sidebar`, `GET /api/workspaces`, `GET /api/dirs?path=` | Sidebar preferences, projects, folder browser |
| `GET /api/auth/status`, `POST /api/login` | Pairing |

## Limits

Not included: multiple user accounts, a desktop shell, handing context between agents, automatic worktrees, Docker, or remote SSH execution.
