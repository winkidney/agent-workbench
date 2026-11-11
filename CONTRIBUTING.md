# Contributing

Thanks for helping. Bug reports, fixes and focused improvements are welcome; for larger changes, open an issue first so we can agree on the approach.

## Setup

You need Go 1.24+, Node.js 22+, and the `claude` and/or `codex` CLIs logged in if you want to try real sessions.

```sh
make build                      # web app (npm ci && npm run build) + bin/agent-workbench
./bin/agent-workbench doctor
```

Layout:

- `cmd/agent-workbench`: the command line (`start`, `stop`, `install`, …) and per-OS service code.
- `internal/workbench`: the server: engines, native catalog, store, HTTP, previews.
- `web/`: the React + Vite interface, embedded into the binary from `web/dist`.
- `packaging/`, `.goreleaser.yaml`, `.github/workflows/`: releases and CI.

## Running a development instance

Keep your everyday workbench untouched by running a second one in the foreground with its own port and data directory:

```sh
./bin/agent-workbench serve --listen 127.0.0.1:17890 --data-dir /tmp/aw-dev
```

It reads the same `~/.claude` and `~/.codex` records, so test in a scratch project directory. The interface is embedded at build time: after changing `web/`, run `make build` (or `npm run build` in `web/` and `go build -o bin/agent-workbench ./cmd/agent-workbench`) and restart `serve`; open pages offer to reload when the build changes.

## Tests

```sh
go test -race ./...
GOOS=windows go vet ./... && GOOS=darwin go vet ./...
cd web && npm test && npm run typecheck && npm run format:check
```

- Go tests sit next to the code (`*_test.go`). Engines are faked in process; `internal/workbench/testdata/fakeagent` is a small program for process-level tests that run on every OS.
- Web tests use Vitest (`*.test.ts`) and cover pure logic (the transcript reducer, sidebar model, command routing, …).
- CI runs on Linux for every push; macOS and Windows run for releases and on manual dispatch.

Optional acceptance scripts in `scripts/` use real agents or the real service:

- `scripts/native-roundtrip.py` creates dedicated test sessions with your logged-in CLIs (small real usage).
- `scripts/smoke.py --inference` runs it.
- `scripts/lifecycle.py` **stops and replaces your own `agent-workbench.service`**; end your sessions first.

[docs/verification.md](docs/verification.md) records how each feature was verified.

## Style

- Go: `gofmt`; short, domain-named packages.
- TypeScript/React: Prettier (`npm run format`); components in PascalCase.
- Comments explain why, in plain English.
- Commits: short imperative subjects in Conventional Commits style (`feat: …`, `fix: …`, `docs: …`), with a body when the reason isn't obvious.
- Pull requests: describe the change, list the commands you ran to verify it, and include screenshots for visible changes (light and dark, and a phone width if the layout changes).

## Screenshots

The images in `docs/screenshots/` come from a demo project (`/tmp/aw-demo/acme-shop`) and sessions created only for them, on a development instance, captured with Playwright at 1440×900 (390×800 for the phone). Before saving, check that the page text shows nothing from your own machine: user names, home paths, host names, other projects.

## Releases

- `make dist` builds every archive, the `.deb` and `.rpm` packages and `checksums.txt` into `dist/` (a snapshot; needs Go and Node.js).
- Pushing a tag `vX.Y.Z` runs the tests on Linux, macOS and Windows, then GoReleaser publishes the GitHub release.
- `make release-check` validates the GoReleaser configuration.
