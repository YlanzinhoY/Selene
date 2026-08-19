# Achievement system handoff

This is the implementation handoff for the local achievement work on branch
`feat-achievment-backend`. The central design is **Plano A — Sistema local de
conquistas**. This document separates what Selene already owns from the work
that still belongs to the achievement backend, LuaTools, and SLSsteam.

## Product boundary

Selene is the process supervisor. For one user-initiated Steam session it must:

1. start the achievement backend;
2. wait for an independent health check;
3. start Steam through the SLSsteam wrapper;
4. monitor Steam and the backend;
5. restart the backend after a failure without restarting Steam;
6. ask the backend to shut down cleanly after Steam closes;
7. force-stop only after a defined timeout.

The backend remains the source of truth for local achievements. LuaTools is the
HTTP/SSE user experience, and SLSsteam will consume snapshots and events over a
Unix socket. A backend failure must never prevent Steam from opening.

## Implemented now

- `internal/achievement-backend` contains the imported headless Go services.
- `internal/achievementserver` turns those services into a long-lived local
  server using the Selene executable itself as a private child process.
- `internal/achievementsupervisor` owns child startup, health polling, SLSsteam
  launch, `/proc`-based Steam monitoring, backend restart, and shutdown.
- `cmd/selene/main.go` selects the private server mode only through internal
  environment variables. There is no public operational subcommand.
- The Bubble Tea home screen exposes **Start Steam with achievements**.
- Backend storage is under the Selene namespace instead of an imported product
  namespace.
- Static Linux builds keep notifications but use a no-op sound implementation
  when `CGO_ENABLED=0`; Linux audio remains available in CGO builds.

The implementation is Linux `amd64` and regular-user only. Windows is an
editing host; use Debian/WSL2 for native validation.

## Runtime lifecycle

```text
Bubble Tea action
      │
      ▼
achievementsupervisor.Run
      │
      ├─ reject non-Linux/non-amd64/default wrapper absence
      ├─ reject an already-running current-user Steam client
      ├─ spawn current Selene binary in private backend mode
      ├─ wait for GET http://127.0.0.1:48212/v1/health
      └─ start ~/.local/share/SLSsteam/path/steam -silent
                 │
                 ▼
       poll current-user Steam processes
                 │
                 ├─ backend exits: restart with backoff and health check
                 ├─ backend unavailable: keep Steam running in degraded mode
                 └─ Steam exits: SIGTERM backend, then SIGKILL on timeout
```

The child backend has Linux `Pdeathsig=SIGTERM`; an unexpected supervisor death
does not leave its owned backend behind. The Steam process deliberately does not
receive that parent-death policy.

### Process environment

Both children receive a controlled environment containing only locale,
terminal, display/session bus, identity, XDG directories, home, and a trusted
system `PATH`. Inherited `LD_AUDIT`, `LD_PRELOAD`, and `LD_LIBRARY_PATH` do not
cross this boundary. The backend additionally receives:

```text
SELENE_INTERNAL_MODE=achievements-server
SELENE_ACHIEVEMENTS_HTTP=127.0.0.1:48212
```

These variables are an internal process contract, not a user interface.

## Server startup and shutdown

`achievementserver.Run` enforces Linux, a non-root user, and a literal loopback
HTTP address. Startup order is:

1. create the state/log directory;
2. configure structured logging;
3. initialize configuration;
4. initialize Steam metadata/cache and achievement cache services;
5. initialize the notifier worker;
6. bind the loopback HTTP listener and serve the router;
7. expose health;
8. start the filesystem watcher.

Watcher startup failure is logged but does not remove health. This allows the
UI to repair settings and preserves the important distinction between “server
is available” and “every configured directory is currently scannable”.

On cancellation or `SIGTERM`, the server stops the watcher, shuts down HTTP
with a five-second timeout, stops the notifier worker, and returns. The
supervisor allows six seconds before force-stopping the child.

## Package map

| Package | Responsibility |
| --- | --- |
| `achievement-backend/ach` | JSON/INI parsing, file cache, and diff calculation |
| `achievement-backend/api` | Chi routes, JSON responses, media, and existing notification SSE |
| `achievement-backend/bootstrap` | Dependency graph and ordered service startup |
| `achievement-backend/config` | JSON settings, emulator sources, and migrations |
| `achievement-backend/logger` | Structured rotating logs and path redaction |
| `achievement-backend/notifier` | Queued DBus/SSE notification delivery and embedded media |
| `achievement-backend/steam` | Steam metadata, schemas, images, and rarity |
| `achievement-backend/watcher` | Prefix discovery and `fsnotify` processing |
| `achievementserver` | Long-lived private server lifecycle |
| `achievementsupervisor` | Steam/backend process lifecycle and recovery |

## Current HTTP contract

The supervisor readiness route is:

```text
GET /v1/health
```

It returns HTTP 200 with `{"status":"success"}`. Existing imported routes are
still mounted below `/decky-backend`, including settings, watcher control, game
metadata, test notifications, and `/notifications` SSE. Media is mounted under
`/api/media/*`.

HTTP binds to `127.0.0.1:48212`. The router still needs a contract migration to
the final Plano A `/v1` endpoints. Do not treat the current imported route set
as the finished public LuaTools API.

## Storage layout

```text
${XDG_CONFIG_HOME:-~/.config}/selene/achievements/config.json
${XDG_DATA_HOME:-~/.local/share}/selene/achievements/
├── data/       per-AppID achievement JSON cache
├── games/      localized game metadata cache
├── icon/       achievement and game images
└── media/      embedded notification assets
${XDG_STATE_HOME:-~/.local/state}/selene/achievements/logs/achievements.log
```

Media extraction occurs during explicit notifier startup, not package import.
The new log/media startup path uses user-private modes. Some inherited cache
and configuration creation still uses broader modes and remains in the
hardening list below.

Achievement data is currently application state, not part of installation
transactions or stack rollback. Complete-removal semantics for this state are
still a product decision; deleting it automatically could destroy the user's
only local achievement history.

## Supervisor invariants and tests

The supervisor is expressed through narrow process and health interfaces. Unit
tests do not launch Steam and cover:

- backend starts before the SLSsteam wrapper;
- initial backend failure still allows Steam startup;
- backend crash while Steam runs triggers a healthy restart;
- already-running Steam is rejected to preserve ordering and ownership;
- shutdown sends the graceful signal first;
- timeout uses force-stop as fallback;
- the health URL must use a loopback IP.

The private server has tests for the internal environment selector and loopback
address validation. A WSL smoke test has also built the static binary, started
the private server with isolated temporary XDG directories, received HTTP 200
from `/v1/health`, sent `SIGTERM`, and observed a clean exit.

## Gaps against Plano A

Selene's supervisor responsibility is implemented, but Plano A as a whole is
not complete. The next owner must not mistake the imported file cache for the
final normalized source of truth.

### Backend core still needed

- transactional SQLite state and history;
- monotonic unlock/progress merge rules;
- protection against empty, partial, or temporarily invalid snapshots;
- startup restore before the first scan;
- durable event IDs and notification deduplication across restarts;
- watcher debounce/retry for partial writes and atomic rename workflows;
- final `/v1/games`, achievement, settings, rescan, and SSE contracts;
- clean persistence flush during shutdown.

### SLSsteam protocol still needed

- Unix socket below `XDG_RUNTIME_DIR` with mode `0600`;
- protobuf version negotiation;
- full snapshot on connect and incremental revisions afterward;
- reconnect/backoff and sequence-gap recovery;
- legacy and modern `GetUserStats` integration;
- strict local-only behavior with no upload to Valve.

### Hardening still needed

- restrict the current permissive CORS policy;
- replace hardcoded-key Steam API credential obfuscation;
- make config permissions private if it stores a credential;
- review media containment and all mutating loopback endpoints;
- audit imported source/assets for provenance and licensing;
- validate DBus notifications, watcher behavior, and optional CGO audio on a
  real non-root Linux desktop session.

## Development rules

- Keep Selene TUI-only; do not add a public `serve` subcommand.
- Keep all TUI copy in `internal/ui/text.go`.
- Never require root or expose the HTTP listener beyond loopback.
- Never make Steam startup depend on backend success.
- Use the SLSsteam wrapper at `~/.local/share/SLSsteam/path/steam`.
- Preserve user achievement state unless removal behavior is explicitly
  approved.
- Validate Linux behavior in WSL or native Linux, not by weakening tests for
  the Windows editing host.

## Validation

From the repository root inside Debian/WSL2:

```bash
CGO_ENABLED=0 go test ./...
CGO_ENABLED=0 go vet ./...
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build ./...
```

Useful contract check:

```bash
rg '127\.0\.0\.1:48212|/v1/health' internal
```

Do not exercise real Steam launch, DBus notifications, or a primary account's
configured watcher paths from an automated development test.
