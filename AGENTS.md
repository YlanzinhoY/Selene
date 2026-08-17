# Agent Guidelines for Selene

Selene is a TUI-first manager and installer for Linux `amd64` Steam integration stacks (`slsteam-moon`, `lumen`, `luatools-moon`, `cloudredirect-moon`). It isolates system mutation behind a transactional snapshot and rollback engine.

---

## Essential Commands

### Build & Run
- **Launch TUI (Development)**: `go run ./cmd/selene`
- **Build Local Linux Binary**: `go build -trimpath -o bin/selene ./cmd/selene`
- **Cross-Compile Linux Release Binary**:
  ```bash
  GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-X github.com/selene-linux/selene/internal/version.Version=v1.0.0 -X github.com/selene-linux/selene/internal/version.Commit=$(git rev-parse --short HEAD) -X github.com/selene-linux/selene/internal/version.Date=$(date -u +%Y-%m-%dT%H:%M:%SZ)" -o bin/selene-linux-amd64 ./cmd/selene
  ```

### Testing & Code Quality
- **Run Unit & Integration Tests**: `go test ./...`
- **Run Installation Script Bootstrap Test**: `sh scripts/test-install.sh`
- **Format Code**: `go fmt ./...`

---

## Architecture & Data Flow

```text
TUI (Charm Bubble Tea)
 ├── cli         TUI-only entrypoint & version flag
 ├── doctor      Read-only compatibility diagnostics
 ├── catalog     Embedded JSON manifests (go:embed)
 ├── planner     Read-only execution detail generator
 ├── plugins     NTFS Steam library discovery & managed symbolic links
 ├── artifact    Download, SHA-256 verification, safe ZIP extraction
 ├── installer   User-only installation, atomic staging, script execution, removal
 └── transaction Pre-mutation snapshot, journal tracking, rollback engine
```

### Operational Boundaries & Trust Model
1. **TUI-Only Command Line**: Running `selene` directly starts the Charm TUI. The executable intentionally exposes **no operational CLI subcommands**. The only supported flag is `--version` (used for bootstrap self-tests).
2. **User-Only Execution (No Root)**: Selene refuses to run as root (`effectiveUID() == 0`). Operations target `${HOME}/.local/share` and user systemd services. Sudo is blocked.
3. **Controlled Execution Environment**: Subprocesses scrub inherited environment variables (`LD_AUDIT`, `LD_PRELOAD`, `LD_LIBRARY_PATH`) and run under a sanitized, trusted `PATH` (`/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin`).
4. **Embedded Immutable Catalog**: Manifests are embedded into the binary via `go:embed` at `internal/catalog/manifests/stable.json`. Upstream artifacts use immutable URLs (pinned tag/commit), hardcoded SHA-256 digests, and exact byte sizes. The `latest` tag is prohibited.
5. **Transactional Integrity**:
   - Downloads and hash verification occur *before* taking snapshots or making filesystem changes.
   - Operations acquire a file lock at `${XDG_STATE_HOME:-$HOME/.local/state}/selene/lock`.
   - Pre-mutation snapshots cover `~/.local/share/SLSsteam`, `~/.local/share/Lumen`, shell profiles (`.bashrc`, `.zshrc`, `.profile`), desktop entries, and user systemd units.
   - Journal files persist at `${XDG_STATE_HOME:-$HOME/.local/state}/selene/transactions/<id>/journal.json`.

---

## Code Base Organization & Key Locations

- `cmd/selene/main.go`: Binary entrypoint delegating to `cli.Run`.
- `internal/cli/cli.go`: Argument parser for `--version` or launching `ui.Run()`.
- `internal/version/version.go`: Variables (`Version`, `Commit`, `Date`) populated via `-ldflags`.
- `internal/catalog/`: Embedded catalog loading, JSON parsing, dependency graph ordering (`OrderedComponents`), and manifest validation.
- `internal/catalog/manifests/stable.json`: Pinned catalog manifest defining components, download URLs, SHA-256 hashes, and install strategies.
- `internal/artifact/`: Download manager (`Fetcher`), SHA-256 checksum verification, safe archive path checking, and zip extraction.
- `internal/doctor/`: Environment pre-check logic (OS/Arch compatibility, Steam installation presence, bash/tool availability).
- `internal/planner/`: Formats operational plan previews displayed in the TUI.
- `internal/installer/`: Installer engine (`install.go`), uninstaller (`uninstall.go`), rollback execution (`rollback.go`), Steam process/service management (`steam.go`), and transaction scope definitions (`scope.go`).
- `internal/transaction/`: Snapshot creation, atomic directory swapping, journal serialization, and restoration.
- `internal/plugins/`: Steam game scanner (`steam_games.go`) and mounted NTFS library linker (`steam_library.go`).
- `internal/ui/`: Bubble Tea TUI model (`model.go`), screen views, and centralized copy (`text.go`).

---

## Conventions & Gotchas

### Centralized Copy Strategy
Do not hardcode user-facing copy in UI views or installers. All UI text, button labels, headers, descriptions, and messages are centralized in specific files:
- **TUI UI Copy**: `internal/ui/text.go`
- **Compatibility Diagnostics**: `internal/doctor/doctor.go`
- **Installation Details**: `internal/planner/planner.go`
- **Catalog Names/Descriptions**: `internal/catalog/manifests/stable.json`

### Platform Constraints
- The Go code is structured to compile on all platforms (`identity_linux.go` vs `identity_other.go`), allowing unit tests (`go test ./...`) to pass on Windows and macOS.
- Real system installation and systemd/Steam manipulation require **Linux `amd64`**, native Steam, and `/bin/bash`. Flatpak Steam is explicitly unsupported.

### Updating Upstream Components
When updating component versions in `internal/catalog/manifests/stable.json`:
1. Obtain the exact immutable release URL, byte size, and SHA-256 digest from the official upstream source.
2. Update version, URL, size, and SHA-256 together.
3. Run `go test ./...`.
4. Commit separately with a semantic commit message, e.g., `chore(catalog): pin luatools-moon vX.Y`.

---

## Testing Patterns

- **Unit Testing**: Tests use Go's `testing` package with `t.TempDir()` for filesystem isolation.
- **Mock Interfaces**: Heavy operations use interfaces for test injection, e.g. `ScriptRunner` in `internal/installer/install.go`.
- **Bootstrap Shell Script Testing**: `scripts/test-install.sh` tests `install.sh` by mocking `curl` using local build outputs and validating checksum failure rejection.
- **Real-System Testing**: Never test system mutations with root or on a primary account without backups. Follow `docs/CACHYOS_TESTING.md` when running manual end-to-end checks on Linux.
