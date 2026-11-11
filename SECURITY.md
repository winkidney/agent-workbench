# Security

## Reporting a vulnerability

Please report vulnerabilities privately through GitHub: **Security → Report a vulnerability** on this repository. Do not open a public issue. Include the version (`agent-workbench version`), your platform, and steps to reproduce. You should get a reply within a week.

Only the latest release receives security fixes.

## What the workbench protects

Agent Workbench can start agents that read and change files and run commands with your account's rights, so access to it is close to access to your shell.

- It answers on all interfaces by default. Requests are accepted by origin and `Host`: browsers on this machine and on private networks may use IP addresses and local host names; with pairing on, they must log in with the pairing code. Any other domain name always needs the code, so a website cannot reach the workbench through DNS rebinding. Public addresses are refused unless they name `agent.localhost`, and then need the code too.
- Changing requests must carry an `Origin` equal to the requested host.
- Previews run on separate origins and never receive the workbench's cookies.
- Files are served only from inside a session's directory.
- The workbench never reads the CLIs' credential files and never writes their conversation records. Agent approvals are forwarded to you, never answered automatically.

See [docs/architecture.md](docs/architecture.md#security-model) for details.

## Out of scope

Exposing the workbench to the internet (for example through a tunnel) is not supported. Anyone who can use your browser session, your account on the machine, or the pairing code can control your agents.
