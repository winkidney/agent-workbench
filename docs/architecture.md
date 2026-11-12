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

- **Claude Code:** `claude -p --input-format stream-json --output-format stream-json --verbose --include-partial-messages --permission-prompt-tool stdio` (plus `--resume <id>` for an existing session). Messages are written as user records; settings change through control requests (`set_model`, `set_permission_mode`, `apply_flag_settings`); permission prompts arrive as `can_use_tool` control requests. A message written during a turn waits at the CLI and is taken in at the next step; `interrupt` with `send_now` asks for it at once. `/goal` runs in the CLI as a session Stop hook; stream-json carries no goal events, so the goal is read from the `goal_status` records in the session's JSONL (the CLI's own rule: the latest decides, met or failed ends it) after `/goal` output, each goal check and each result.
- **Codex:** `codex app-server`, the JSON-RPC protocol of the Codex IDE integrations. `thread/start` / `thread/resume` (with `excludeTurns`: the engine reads only the thread's settings; history comes from `thread/turns/list`), `turn/start` (with model, effort and collaboration mode), `turn/steer`, `turn/interrupt`; approvals arrive as server requests. Commands map to `thread/compact/start`, `review/start`, `thread/goal/*`, `thread/fork`, `mcpServerStatus/list`, `thread/archive`, `thread/delete`, and so on. Codex also starts turns itself to pursue a goal (after `/goal`, after each turn while the goal is active, on resume); a `turn/started` the page did not request runs like any other turn, and `thread/goal/updated` / `thread/goal/cleared` keep the session's goal current.

An engine stays running between turns and is put to sleep after the idle timeout when nothing is in progress (no turn, approval, queued message, extra process or managed preview); the next message resumes the native session. Metadata (models, commands, skills, quotas) comes from short-lived probe processes that never create a session or call a model.

### Processes

- When the workbench runs as the systemd service `agent-workbench.service` (Linux, `start` or `install`), each engine is a transient unit (`agent-workbench-session-<id>.service`) with `KillMode=control-group`, `SuccessExitStatus=143` (Claude Code answers SIGTERM by exiting 143; that stop is a success, as a death by SIGTERM already is) and `BindsTo=agent-workbench.service`. When the workbench's unit stops, systemd stops the engines first; an engine that ends because it was asked to (SIGTERM/SIGINT, or exit 143/130 after catching them, as Claude Code does) or while the workbench closes is put to sleep, not marked as failed, and its pending approvals and questions are released (they stay answerable later). "Busy" means the unit's cgroup has processes the engine did not have when it became ready.
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

The interface runs at `agent.localhost:PORT`. Each preview gets its own random origin, `p-<id>.agent.localhost:PORT`, which proxies a file in the session directory or a loopback HTTP server (including WebSockets). A preview is authorized by a one-time POST that sets a cookie for that origin only; the proxy never forwards the workbench's cookies or `Authorization` header upstream. The media endpoint serves raster images (up to 32 MiB) and videos with Range support, after sniffing the content; SVG and HTML are only served on preview origins. Pages that cannot reach the preview origins (other devices, tunnels) open file previews at `/__files/<grant>/<path>` on the workbench's own address instead, with `Content-Security-Policy: sandbox` (no `allow-same-origin`), `no-store`, `no-referrer` and `Access-Control-Allow-Origin: *` (the sandboxed page's module scripts and fetches are cross-origin); a real PDF (by its `%PDF-` header) is left unsandboxed for the browser's viewer. The grant is a sign-in credential issued to the signed-in page that opened the file: a grant issued to this computer while it skips sign-in only works on this computer; under pairing it lasts as long as that pairing session; under the password, until the sign-in rules change; never beyond the preview, 12 hours or a restart. The preview list never contains grants.

## Security model

- **Who can connect:** only this computer is open, meaning a request from a loopback address (`127.0.0.0/8`, `::1`) that also names a loopback host (`localhost`, `*.localhost`, a `127.x` address or `[::1]`). Both are required: any client can send `Host: localhost`, and a website that points its name at `127.0.0.1` (DNS rebinding) names a host that is not loopback. A request that carries a proxy header (`Forwarded`, `Via`, `X-Forwarded-*`, `X-Real-IP`, `CF-Connecting-IP`, `True-Client-IP`, `X-Client-IP`) was relayed and is not this computer either. The `local_signin` setting makes this computer sign in too, for plain tunnels that add no header; it requires `pairing` or `password`. Everything else follows the remote access setting (`remote_access`):
  - `off` (default): refused with a short HTML page (`403`, `X-Workbench-Access: off`);
  - `pairing`: the app is served; the API needs a session from `POST /api/login` with the pairing code;
  - `password`: HTTP Basic (`WWW-Authenticate: Basic realm="Agent Workbench", charset="UTF-8"`) on every request, pages included; pairing sessions are not accepted.
  From public addresses only `agent.localhost:PORT` is served, and it signs in. Installs from before the setting keep pairing if it was on, and are otherwise `off`.
- **Secrets:** the pairing code (`pairing-code`) and the username and password (`access-password.json`) are files in the data directory with mode 0600, compared in constant time. Failed sign-ins are limited to 10 per minute per client address.
- **Sessions** use a host-only, HttpOnly, SameSite=Strict cookie after pairing, kept in memory for 12 hours; changing the mode or the password ends them.
- **Changes** (`POST`, `PUT`, `PATCH`, `DELETE`) must carry an `Origin` equal to `http://` plus the requested host. Requests that sign in may also come from an origin listed in `allowed_origins` (Settings → Allowed origins: `scheme://host[:port]`, normalized, at most 20), such as an HTTPS tunnel's address; that also covers proxies that rewrite `Host`. This computer, while it skips sign-in, only accepts its own origin, so a listed site can never drive it from this computer's browser.
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
| `GET /api/auth/status`, `POST /api/login` | Sign-in state, pairing |
| `GET/PUT /api/settings`, `GET /api/settings/secrets` | Access from other devices, storage; the pairing code and password |

## Limits

Not included: multiple user accounts, a desktop shell, handing context between agents, automatic worktrees, Docker, or remote SSH execution.
