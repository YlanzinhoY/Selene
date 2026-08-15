# Contributing to Selene

Contributions that improve Linux compatibility, safety, documentation, and the TUI are welcome.

## Requirements

- Go 1.24 or newer.
- Git.
- Linux for real Steam, Proton, and systemd user-service integration tests.

## Development setup

```bash
git clone https://github.com/YlanzinhoY/Selene.git
cd Selene
go mod download
go test ./...
sh scripts/test-install.sh
go run ./cmd/selene
```

Format the Go packages and run the complete suite before committing:

```bash
go fmt ./...
go test ./...
```

Use semantic commit messages such as `feat:`, `fix:`, `docs:`, `test:`, `refactor:`, and `chore:`.

## Editing interface text

The main TUI copy is centralized so wording can change without touching state logic:

- `internal/ui/text.go`: menu names, descriptions, activity messages, buttons, headings, confirmations, and the creator credit.
- `internal/doctor/doctor.go`: compatibility-check results.
- `internal/planner/planner.go`: operations shown under **Installation details**.
- `internal/catalog/manifests/stable.json`: bundle and component names and descriptions.
- `README.md` and `docs/`: user-facing documentation.

Add or update a test when wording is part of a safety boundary, action key, or important navigation label.

## Build outputs

Build a local development binary on Linux:

```bash
go build -trimpath -o bin/selene ./cmd/selene
```

Cross-compile from Windows PowerShell:

```powershell
$env:GOOS = "linux"
$env:GOARCH = "amd64"
$env:CGO_ENABLED = "0"
go build -trimpath -o bin/selene-linux-amd64 ./cmd/selene
Remove-Item Env:GOOS
Remove-Item Env:GOARCH
Remove-Item Env:CGO_ENABLED
```

`bin/` is for local development binaries. `build/` is the local staging directory for final release assets. Both directories are intentionally ignored by Git.

Each GitHub Release must publish exactly:

```text
selene-linux-amd64
selene-linux-amd64.sha256
```

The release binary must embed its version, commit, and UTC build date through the variables in `internal/version/version.go`. Validate the resulting ELF binary on Linux, run `selene --version`, and confirm the checksum with `sha256sum --check` before uploading it.

## Project structure

```text
cmd/selene/          executable
internal/cli/        TUI-only entrypoint and internal version flag
internal/artifact/   download, SHA-256, and safe archive inspection
internal/catalog/    embedded catalog and manifest validation
internal/doctor/     read-only compatibility checks
internal/planner/    auditable installation details
internal/installer/  user-only install, rollback, removal, and Steam restart
internal/transaction snapshot, journal, and restore engine
internal/ui/         Charm interface and editable copy
internal/version/    build metadata
docs/                user and technical documentation
```

Read [Architecture](docs/ARCHITECTURE.md), [Catalog](docs/CATALOG.md), [Transactions](docs/TRANSACTIONS.md), and [Security](SECURITY.md) before changing trust boundaries or installation behavior.

## Real-system testing

Never test user-level Steam mutation as root. Use a disposable user account or a system with recoverable backups when changing installation, rollback, removal, services, or wrapper behavior.

Follow [docs/CACHYOS_TESTING.md](docs/CACHYOS_TESTING.md) and remove personal information before sharing logs.
