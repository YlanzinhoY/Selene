# Architecture

Selene separates the terminal experience from system logic so the same core can be exercised by the TUI and tests.

```text
TUI
 │
 ├── doctor      read-only compatibility checks
 ├── catalog     embedded validated manifests
 ├── planner     read-only installation details
 ├── achievementserver
 │    ├── private long-lived mode selected only for a Selene child process
 │    ├── loopback HTTP API and independent health check
 │    └── ordered service startup and graceful shutdown
 ├── achievementsupervisor
 │    ├── backend readiness before Steam startup
 │    ├── SLSsteam wrapper launch and current-user process monitoring
 │    └── backend restart, SIGTERM, timeout, and forced-stop fallback
 ├── plugins     optional, user-scoped community integrations
 │    └── mounted NTFS Steam-library discovery and managed symbolic links
 ├── artifact    download, integrity, and ZIP inspection
 ├── installer   user-only install and complete removal
 │    ├── pinned slsteam-moon setup.sh
 │    ├── atomic Lumen/LuaTools activation
 │    ├── pinned setup.sh uninstall + residue validation
 │    └── Steam stop/restart around rollback
 └── transaction
      ├── pre-mutation snapshot
      ├── persistent journal
      └── manual or automatic restore
```

The executable intentionally exposes no operational CLI commands. Running `selene` opens the TUI. The internal `--version` flag exists only for release diagnostics and bootstrap validation. The achievement backend reuses the same executable through a private, environment-selected child-process mode; it is not a supported command-line interface.

## Supervised achievement session

The home-screen action **Start Steam with achievements** owns one complete session:

```text
Selene TUI
  │
  ├─ spawn private achievement backend
  ├─ poll GET http://127.0.0.1:48212/v1/health
  ├─ start ~/.local/share/SLSsteam/path/steam -silent
  └─ while current-user Steam processes exist
       ├─ monitor the backend
       └─ restart it with backoff after failure

Steam closes
  └─ SIGTERM backend ── timeout ── SIGKILL fallback
```

Backend failure is deliberately non-fatal to Steam. The session is marked degraded and recovery continues in the background. The backend child receives a sanitized environment and a parent-death signal. Steam does not receive a parent-death signal, so an unexpected Selene exit does not intentionally kill the client.

The backend HTTP listener accepts only a literal loopback address. Shared services and the health endpoint start before the filesystem watcher; this keeps readiness independent from an empty, invalid, or temporarily unavailable scan path.

## Current boundaries

Real installation is limited to Linux `amd64`, initialized native Steam, and the current user's account. Flatpak Steam, Game Mode-specific integration, and `/usr` changes are outside the first adapter.

## Trust boundary

Selene controls acquisition, hashing, archive inspection, staging, process environment, snapshots, rollback, removal validation, Steam restart, and the achievement-backend lifecycle. slsteam-moon remains responsible for its `LD_AUDIT` wrapper, desktop entries, and guardian logic. Selene executes that logic only from the exact catalog artifact.

The executor never downloads or runs a live installer from `main`. It blocks sudo, forces upstream immutable/user-only mode, removes inherited `LD_AUDIT`, `LD_PRELOAD`, and `LD_LIBRARY_PATH`, and validates essential files before committing.

Journals live outside the installed tree. Interrupted installation or removal remains recoverable from the TUI.
