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

- **Claude Code:** `claude -p --input-format stream-json --output-format stream-json --verbose --include-partial-messages --permission-prompt-tool stdio --include-hook-events --prompt-suggestions` (plus `--resume <id>` for an existing session; the last two flags only when the CLI's `--help` lists them, as an older CLI exits on a flag it does not know). Messages are written as user records; settings change through control requests (`set_model`, `set_permission_mode`, `apply_flag_settings`); permission prompts arrive as `can_use_tool` control requests. A message written during a turn waits at the CLI and is taken in at the next step; `interrupt` with `send_now` asks for it at once. `/goal` runs in the CLI as a session Stop hook; stream-json carries no goal events, so the goal is read from the `goal_status` records in the session's JSONL (the CLI's own rule: the latest decides, met or failed ends it) after `/goal` output, each goal check and each result.
- **Codex:** `codex app-server`, the JSON-RPC protocol of the Codex IDE integrations. `thread/start` / `thread/resume` (with `excludeTurns`: the engine reads only the thread's settings; history comes from `thread/turns/list`), `turn/start` (with model, effort and collaboration mode), `turn/steer`, `turn/interrupt`; approvals arrive as server requests. Commands map to `thread/compact/start`, `review/start`, `thread/goal/*`, `thread/fork`, `mcpServerStatus/list`, `thread/archive`, `thread/delete`, and so on. Codex also starts turns itself to pursue a goal (after `/goal`, after each turn while the goal is active, on resume); a `turn/started` the page did not request runs like any other turn, and `thread/goal/updated` / `thread/goal/cleared` keep the session's goal current. The Auto-review permission choice is the workspace profile with `approvalsReviewer: "auto_review"` (sent with `thread/start`/`thread/resume` and every `turn/start`; other choices send `"user"`, since Codex keeps a turn's reviewer); `item/autoApprovalReview/completed` becomes a line in the conversation.

Requests besides approvals and questions: Codex's `item/permissions/requestApproval` is answered with the requested profile and a `turn` or `session` scope (or nothing); MCP elicitation (Codex `mcpServer/elicitation/request`, Claude's `elicitation` control request) with `accept` and the form's content, `decline` or `cancel`; Codex's `currentTime/read` is answered by the workbench itself. `thread/settings/updated` and the CLIs' name changes (`thread/name/updated`, `session_title_changed`) keep the pills and titles current. Telemetry carries `detail`: what a turn waits on (`api_retry`, `permission_check_status`, hook start, auto-review start).

Prompt suggestions: after a turn's `result`, Claude writes `{"type":"prompt_suggestion","suggestion":…}` (one more request on the cached conversation, skipped when the CLI judges the next step unclear or a request waits); the workbench keeps the latest for the composer as telemetry `suggestion`, drops it once a turn or a request begins, and does not store it as an event. The `suggestions` preference (`/api/preferences`) pauses them with the `set_prompt_suggestions_paused` control request. Codex 0.167 has no suggestions (`thread/prediction/request` is not implemented); one would fill the same field.

Subagents (subagents.go): Claude writes a subagent's records to `<session>/subagents/agent-<id>.jsonl`, next to `agent-<id>.meta.json` that names the Agent call (`toolUseId`). Live, stream-json forwards the subagent's tool calls and results with `parent_tool_use_id`, registers it as a `local_agent` task (`task_id` = agent id), and its permission requests carry `agent_id`. A history page adds, after each Agent call, a `workbench_subagent` record (agent id, calls left out) and the subagent's latest 200 tool calls and results, reduced to what a line shows (strings of 1 KB, results of 300 bytes, 1 MiB per page). The scan keeps only those lines' offsets, so its memory does not grow with the file. Codex runs a subagent as its own thread, started by a `subAgentActivity` item in the parent, and sends that thread's records and approval requests on the parent's connection with the child's `threadId`. Those records never change the parent's turn, status or usage; their tool items are re-sent tagged `workbenchChild`, reduced the same way, and history adds them from `thread/turns/list` of the child thread. Requests from a subagent carry `subagent` (description, toolUseId, the subagent's session id). The page keeps at most 200 lines per subagent row, with the same reductions.

Background work: Claude reports its tasks on stream-json (`background_tasks_changed` is the live set, replaced on each change and reset when the process starts; `task_started`, `task_progress`, `task_updated` and `task_notification` carry details), stops one with the `stop_task` control request and moves a foreground command or subagent to the background with `background_tasks` (sent once Claude has registered the task, as it ignores earlier requests). Codex's background terminals are only listed on request, so `thread/backgroundTerminals/list` is polled after each turn and every 5 s while any runs; `terminate` and `clean` stop them. Under systemd, processes in the engine's unit that no longer descend from its main process (they outlived the command that started them) and have run for 10 s are listed as other processes. Claude also starts turns by itself (a background task ended): a turn record arriving between turns runs as a turn, unless it is a subagent's (`parent_tool_use_id`). Sessions carry the count as `background`; the telemetry lists the tasks.

Each session in the list and in its status events carries `attention` (`approval`, `question` or `plan`) while it waits for the user: computed from pending requests, a Codex async question not yet answered and a Codex plan-mode turn that ended with a plan; never stored.

An engine stays running between turns and is put to sleep after the idle timeout when nothing is in progress (no turn, approval, queued message, extra process or managed preview); the next message resumes the native session. Metadata (models, commands, skills, quotas) comes from short-lived probe processes that never create a session or call a model.

### Processes

- When the workbench runs as its systemd service (Linux, `start` or `install`), each engine is a transient unit (`<service>-session-<id>.service`) with `KillMode=control-group`, `SuccessExitStatus=143` (Claude Code answers SIGTERM by exiting 143; that stop is a success, as a death by SIGTERM already is) and `BindsTo=<service>.service`. The service is `agent-workbench` for the default data directory and `agent-workbench-<port>` for another (instance.go); `serve --name` carries it, and previews follow the same naming. When the workbench's unit stops, systemd stops the engines first; an engine that ends because it was asked to (SIGTERM/SIGINT, or exit 143/130 after catching them, as Claude Code does) or while the workbench closes is put to sleep, not marked as failed, and its pending approvals and questions are released (they stay answerable later). "Busy" means the unit's cgroup has processes the engine did not have when it became ready.
- Everywhere else (macOS, Windows, Linux without a systemd user manager) an engine is a child process. Unix keeps it and everything it starts in a process group (descendants that leave the group are tracked through the process table); Windows uses a Job Object with kill-on-close. Stopping a session signals the whole tree and kills what is left after ten seconds.
- Managed preview servers follow the same rules.

### Programs and homes

Which program runs each agent and where it keeps its data (agents_setup.go): the program and home chosen in Settings (the `agents` setting), else the workbench's environment (`PATH`, `CLAUDE_CONFIG_DIR`, `CODEX_HOME`, as `service-env.json` restores them), else the CLI's default. Every run of a CLI goes through this: engines, metadata and init probes, the short-lived read-only app-servers (history, follow, archive, delete, rename), `--help` and `--version` checks (5 s), sign-in status and `doctor`. A home is passed (`CLAUDE_CONFIG_DIR` / `CODEX_HOME`, by name to `systemd-run`) only when Settings chose it: with the variable set, even to `~/.claude`, Claude Code keys its macOS Keychain login to that string and keeps `.claude.json` in it. Chosen paths are stored absolute and cleaned, so the CLI always gets one spelling. The catalog reads native records from the home in use; Codex's index from `CODEX_SQLITE_HOME` when set. A program is accepted only if it is an executable file whose `--version` names the CLI; a home only if it is an existing folder. A home changes only while none of that agent's sessions is at work; its idle engines are put to sleep first. Caches that depend on program and home (metadata, `claude-init`) are keyed by them.

Sign-in from the page (agents_login.go) runs the CLI's own login with the program and home in use: `codex login --device-auth` (its link and one-time code are passed to the page) or `claude auth login --claudeai|--console|--sso` (its sign-in link is passed to the page, and the code that page shows is written to the CLI's stdin). The CLI runs with `BROWSER` set to a no-op and, on Linux, without `DISPLAY`, so nothing opens on this computer. One login per agent, ended by cancel, a change of program or home, or 10 minutes. Credentials are written by the CLI; the workbench does not read, store or log them, nor the output beyond the link, the code and the last line of a failure. Status comes from `claude auth status --json` and `codex login status` (10 s timeout, cached 20 s).

## Native records

The CLIs' records are the source of truth; the workbench never writes them.

- **Codex:** thread metadata is read from `$CODEX_HOME/state_5.sqlite` (read-only); history is read through a short-lived app-server (`thread/turns/list`). Writer locks (`thread-writer-locks/<id>.lock`) tell whether another client holds a thread.
- **Claude Code:** history is read from `$CLAUDE_CONFIG_DIR/projects/<project>/<session>.jsonl`, following the parent-child chain of the newest branch. `sessions/<pid>.json` (never the credential files) tells whether another Claude process holds a session and whether it is busy.
- A session's project is the directory it was **started** in: Codex's thread directory, Claude's first recorded `cwd`.
- Sessions another client is writing are followed by reading only the bytes appended since the last read.
- **Codex records as they are now** (rollout.go). Codex compresses a rollout idle for 7 days to `<path>.zst` (one zstd frame, the same JSON lines; the index keeps the `.jsonl` path) and restores the plain file when the thread resumes. A recorded path that is gone is looked for compressed, then under this Codex home (from its `sessions/` or `archived_sessions/` part), for a home copied or moved from elsewhere. Compressed records are decoded as a stream, in one goroutine with a bounded window, line by line (lines over 8 MiB skipped): usage is read once in the background, one record at a time, and cached until the file changes (telemetry `reading` until then); the related-project index counts them once, within its per-pass budget. A held thread with only a compressed record (being restored) is followed by asking its app-server every 2 s. Codex keeps paginated history (`thread_history_1.sqlite`) as a projection of the rollout, which stays the full record; history is read through app-server as before.
- **Records that are gone.** A Codex thread listed in the index whose rollout exists nowhere (an older sessions folder deleted, say) opens as `recordMissing`: its first message, token count and start time from the index, read-only. A thread whose index names a record under another home cannot be opened by Codex itself; the error says where the record is.

## Store and events

A SQLite database (WAL) in the data directory keeps the session index, settings (pairing, sidebar pins, per-session options) and a short event cache. Events are kept 7 days, at most 2000 per session, and pruned at start and hourly; progress-only signals are never stored.

Nothing grows with uptime or with the number of sessions shown (retention.go):
- **Caches with a time of last use** (`cache`: usage read from native records, Claude's skills per directory; `file_previews`; `preview_tabs`) are dropped when unused for 7 days.
- **Rows of sessions gone.** An hourly pass records which sessions exist (`session_seen`: listed by the CLIs, or loaded here) and drops the rows of sessions not seen for 7 days: options, telemetry, goal, usage cache, preview tabs and file previews, activity counts, display names. It changes nothing when the list cannot be read or is empty. Creation request records go after 7 days. A session's first id (a workbench token) leaves no rows behind once it has its native id.
- **In memory:**
  - A session's runtime outlives its engine by 15 minutes when nothing waits on it (no request, queue, question or running server). It is rebuilt from the store when the session is opened again.
  - A sleeping engine's leftovers (the last Codex turn request, task output names, speed samples) are dropped when it stops.
  - Capability caches go after 2 minutes.
  - Usage is followed for 8 sessions at most, with four numbers per Claude reply.
  - Following another client keeps only the records of its current tail page. The page receives events over server-sent events (`GET /api/events`) and reconciles each finished turn with the native record.

On start the server writes `server.json` (pid, address, version, mode, and a one-time token, mode 0600) to the data directory; `agent-workbench stop` uses the token with `POST /api/local/shutdown`, which only accepts requests from this machine. The file is removed on exit.

## Previews

The interface runs at `agent.localhost:PORT`. Each preview gets its own random origin, `p-<id>.agent.localhost:PORT`, which proxies a file in the session directory or a loopback HTTP server (including WebSockets). A preview is authorized by a one-time POST that sets a cookie for that origin only; the proxy never forwards the workbench's cookies or `Authorization` header upstream. The media endpoint serves raster images (up to 32 MiB) and videos with Range support, after sniffing the content; SVG and HTML are only served on preview origins. Pages that cannot reach the preview origins (other devices, tunnels) open file previews at `/__files/<grant>/<path>` on the workbench's own address instead, with `Content-Security-Policy: sandbox` (no `allow-same-origin`), `no-store`, `no-referrer` and `Access-Control-Allow-Origin: *` (the sandboxed page's module scripts and fetches are cross-origin); a real PDF (by its `%PDF-` header) is left unsandboxed for the browser's viewer. The grant is a sign-in credential issued to the signed-in page that opened the file: a grant issued to this computer while it skips sign-in only works on this computer; under pairing it lasts as long as that pairing session; under the password, until the sign-in rules change; never beyond the preview, 12 hours or a restart. The preview list never contains grants.

A file preview is a row in the store (session, file, the secret of its origin), not an object in memory: the same file opens the same preview, ending the session drops its rows, and one unused for 7 days goes. Running servers are the only previews held in memory. A session's preview tabs (`GET/PUT /api/sessions/{id}/preview-tabs`) keep targets only: at most six, files by their path in the session's directory (with the file's size and time on read, for "Updated · Reload"), servers by their loopback URL or by the command and port the workbench runs. The page loads only the tab shown, and nothing while the panel is hidden.

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
- **Agents** keep their own permission systems: approvals are forwarded to the page, and the workbench never answers one by itself; automatic approvals are the CLIs' own (Claude Code's auto mode, Codex's auto-review), chosen per session. Unknown control requests are declined.
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
| `POST /api/sessions/{id}/plan-dismiss` | The user decided about a Codex plan in the page (it no longer needs them) |
| `POST /api/sessions/{id}/background-stop`, `GET …/background-output?task=`, `POST …/to-background` | Stop background work (`{task}`, empty for all), read a Claude task's output, move a foreground Claude command to the background (`{toolUseId}`); `POST …/sleep` with `{force: true}` stops background work too |
| `POST /api/sessions/{id}/reply` | Answer an approval: `{id, decision, message, answers}` |
| `GET /api/sessions/{id}/commands`, `POST …/command` | The command and skill catalog; run a command |
| `POST /api/sessions/{id}/rename`, `/archive`, `/unarchive`, `/delete` | Session management |
| `GET /api/sessions/{id}/telemetry`, `PATCH …/options` | Usage and settings |
| `GET /api/agents/{agent}/capabilities?cwd=`, `/usage`, `GET/PUT …/defaults` | Models, modes, quotas, default settings |
| `GET/PUT /api/agents/setup`, `POST /api/agents/{agent}/check` | Each agent's program and home: as in use, with version, sign-in and candidates on PATH; saved after checking (409 while that agent's sessions are at work); checked without saving |
| `POST/GET/DELETE /api/agents/{agent}/login`, `POST …/login/code` | Sign a CLI in from the page: start (link, one-time code or a code to paste), follow, cancel; give the code the sign-in page showed |
| `GET /api/media?session=&path=&base=` | Images and video |
| `POST /api/previews`, `GET /api/previews` | Open a file or server preview (a file opens the same preview again); the running servers |
| `GET/PUT /api/sessions/{id}/preview-tabs` | The session's preview tabs: at most six targets and the one shown; files read back with their stamp, servers with whether they run |
| `POST /api/sessions/{id}/reveal` | Show a file in the desktop file manager |
| `GET/POST /api/sidebar`, `GET /api/workspaces`, `GET /api/dirs?path=` | Sidebar preferences, projects, folder browser |
| `GET /api/auth/status`, `POST /api/login` | Sign-in state, pairing |
| `GET/PUT /api/settings`, `GET /api/settings/secrets` | Access from other devices, storage, signed-in devices (`signedIn`); the pairing code and password |
| `POST /api/settings/signout-others` | End every pairing sign-in except the caller's (pairing mode only) |
| `GET /api/about`, `GET /api/updates` | Versions, paths, run mode, data, and each CLI's `--version` (asked once per binary until it changes); the latest GitHub release, asked on request and kept 10 minutes |
| `GET/PATCH /api/preferences` | The workbench's own settings for every session: `{suggestions}` |

## Limits

Not included: multiple user accounts, a desktop shell, handing context between agents, automatic worktrees, Docker, or remote SSH execution.
