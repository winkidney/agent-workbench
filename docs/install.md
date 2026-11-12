# Installing Agent Workbench

Agent Workbench is a single executable with the web interface built in. Releases are published for macOS (Apple silicon and Intel in one universal binary), Windows (x64 and ARM64) and Linux (x64 and ARM64).

## Before you start

- Install and log in to the [Claude Code](https://docs.anthropic.com/en/docs/claude-code) CLI (`claude`) and/or the [Codex](https://github.com/openai/codex) CLI (`codex`). The workbench uses their logins and settings as they are.
- On Windows, Claude Code needs Git for Windows.

## Download

Get the file for your platform from the [Releases](https://github.com/winkidney/agent-workbench/releases) page:

| Platform | File |
| --- | --- |
| macOS | `agent-workbench_<version>_macos_universal.tar.gz` |
| Windows | `agent-workbench_<version>_windows_amd64.zip` (`_arm64` on ARM) |
| Linux | `agent-workbench_<version>_linux_amd64.tar.gz` (or `_arm64`), or the `.deb` / `.rpm` package |

`checksums.txt` on the same page lists every file's SHA-256:

```sh
shasum -a 256 -c checksums.txt --ignore-missing     # macOS
sha256sum -c checksums.txt --ignore-missing          # Linux
```

```powershell
Get-FileHash .\agent-workbench_<version>_windows_amd64.zip   # Windows: compare with checksums.txt
```

## macOS

```sh
tar -xzf agent-workbench_*_macos_universal.tar.gz
xattr -dr com.apple.quarantine agent-workbench   # the builds are not signed yet
./agent-workbench install
```

`install` copies the program to `~/.local/bin/agent-workbench`, adds a launch agent (`~/Library/LaunchAgents/local.agent-workbench.plist`) that starts it at login, and starts it now. Data is in `~/Library/Application Support/agent-workbench`; the log is `workbench.log` there.

## Windows

1. Unpack the zip. If Windows marks the files as downloaded, right-click → Properties → **Unblock**, or choose **More info → Run anyway** when SmartScreen asks (the builds are not signed yet).
2. In PowerShell, in the unpacked folder:

   ```powershell
   .\agent-workbench.exe install
   ```

`install` copies `agent-workbench.exe` and `agent-workbenchw.exe` to `%LOCALAPPDATA%\Programs\agent-workbench`, adds a login entry under `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`, and starts it in the background. `agent-workbenchw.exe` is the same program built without a console window; the login start and background runs use it. Data and the log are in `%LOCALAPPDATA%\agent-workbench`.

Windows Firewall may ask whether to allow connections the first time: deny it to use the workbench on this computer only, or allow private networks to reach it from other devices.

## Linux

From the archive:

```sh
tar -xzf agent-workbench_*_linux_amd64.tar.gz
./agent-workbench install
```

From a package (run `install` afterwards as your own user, not as root):

```sh
sudo apt install ./agent-workbench_*_amd64.deb      # Debian, Ubuntu
sudo dnf install ./agent-workbench-*.x86_64.rpm     # Fedora, RHEL
agent-workbench install
```

`install` copies the program to `~/.local/bin/agent-workbench` (a packaged `/usr/bin/agent-workbench` is used where it is) and enables a systemd user service, `~/.config/systemd/user/agent-workbench.service`. It starts when you log in; to start it at boot before anyone logs in, run `loginctl enable-linger`.

Without a systemd user manager (WSL without systemd, containers), there is no login start: run `agent-workbench start` when you need it.

## Using it

Open **http://agent.localhost:17880**.

```sh
agent-workbench status      # whether it runs, its addresses and the login start
agent-workbench stop        # end managed sessions and previews, then stop
agent-workbench start       # start again
agent-workbench doctor      # check claude, codex and the platform
```

- **Upgrade:** download the new release and run its `install`. It stops the old version, replaces the program and starts again; your data stays.
- **Uninstall:** `agent-workbench uninstall` removes the login start and stops the workbench, keeping your data. Add `--purge` to also delete the data directory and the installed program (kept while another workbench still starts with it).
- **Address and data:** `install` accepts `--listen`, `--data-dir` and `--idle` and keeps them for the login start. The default `0.0.0.0:17880` is reachable from your local network (other devices need a pairing code or password, see [Access from other devices](usage.md#access-from-other-devices)); `--listen 127.0.0.1:17880` keeps it to this machine.

### Environment

The login start does not run through your shell, so `install` records the current `PATH`, the proxy variables (`HTTP_PROXY`, `HTTPS_PROXY`, `ALL_PROXY`, `NO_PROXY` and their lowercase forms), `CODEX_HOME`, `CODEX_SQLITE_HOME` and `CLAUDE_CONFIG_DIR` in `service-env.json` in the data directory (readable by you only). API keys and other variables are not recorded; add one with `--env NAME`, for example `agent-workbench install --env ANTHROPIC_BASE_URL`. Run `install` again after changing your `PATH` or proxy. A program or home folder chosen in **Settings → Agents** takes precedence over these (see [Agents](usage.md#agents)).

## More than one workbench

Several workbenches can run for one user, for example one per account, each with its own data directory and address:

```sh
agent-workbench install --data-dir ~/.local/state/agent-workbench-work --listen 127.0.0.1:17881
agent-workbench status --data-dir ~/.local/state/agent-workbench-work
agent-workbench stop --data-dir ~/.local/state/agent-workbench-work
```

- The one in the default data directory is the default workbench, unchanged. Another needs its own `--listen` the first time, and is named after its port: the service `agent-workbench-17881` (systemd unit, launchd agent `local.agent-workbench-17881`, or Run entry on Windows), and its agents' units `agent-workbench-17881-session-*`. The name and address are kept in `instance.json` in its data directory, so `start`, `stop`, `status` and `uninstall` find it from `--data-dir` alone.
- Each has its own settings. Choose each one's programs and home folders in **Settings → Agents**, or set `CLAUDE_CONFIG_DIR` and `CODEX_HOME` when installing it.
- Each signs in other devices on its own. Browsers share cookies between the ports of one address, so each workbench uses a cookie of its own: signing in to one does not sign you out of another.
- **Through a tunnel,** give each workbench its own public host name, for example a Cloudflare Tunnel with `aw.example.com` → `localhost:17880` and `aw-work.example.com` → `localhost:17881`. A workbench can't sit under a path of another address. Then, on this computer, open the second one at `http://agent.localhost:17881`, choose how other devices sign in, and add its public address (`https://aw-work.example.com`) to its **Allowed origins**.
- `install` moves an older login start of the same data directory, made under the default name, to the new one.
- They share the one installed program: `install` from either upgrades it for both.

## Platform notes

- **Agent processes.** When the workbench runs as its systemd service on Linux, each agent runs as its own systemd unit, bound to that service. Everywhere else an agent is a child process kept with everything it starts in one process group (macOS, Linux without systemd) or Job Object (Windows). Ending a session ends the whole tree, and agents end when the workbench does.
- **Windows.** `C:\…` paths in replies are not yet turned into clickable path chips, and the login entry does not restart the workbench after a crash (macOS and Linux do). "Show in Files" opens File Explorer.
- **Unsigned builds.** macOS Gatekeeper and Windows SmartScreen warn the first time; allow it once as described above.
