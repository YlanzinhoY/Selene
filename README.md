# Selene 🌙

> There are truths that only the light of the moon can reveal.

Selene is an independent community project written in Go. It provides a friendly terminal interface for installing, checking, rolling back, and completely removing the LuaTools stack on Linux, including systems where games run through Proton.

The project is still pre-release. The current build has a Charm-based TUI, Steam and Proton checks, a SHA-256-pinned artifact catalog, transactional installation, persistent snapshots, automatic recovery, and complete user-only removal. Real hardware validation on CachyOS is still required before the first public release.

## Goals

- Keep the normal experience entirely inside the TUI.
- Avoid administrative privileges whenever possible.
- Explain every change before applying it.
- Verify every download with a pinned size and SHA-256 digest.
- Snapshot affected files before mutation.
- Restore the previous state and restart Steam after rollback.
- Remove the full LuaTools/Lumen/slsteam-moon integration without touching games.

## Requirements

- Linux `x86_64/amd64`.
- Native Steam, not Flatpak Steam yet.
- Steam must have been opened once so its first update can finish.
- A regular desktop user session; never run Selene with `sudo`.
- Internet access for artifacts that are not already cached.

Wine and Proton do not block Selene. Selene integrates with the native Linux Steam client, while each game continues using the Proton version selected in Steam.

## 1. Install Selene

After a GitHub Release publishes the binary and checksum, download and inspect the bootstrap before running it:

```bash
curl --proto '=https' --tlsv1.2 -fL \
  https://raw.githubusercontent.com/YlanzinhoY/Selene/main/install.sh \
  -o /tmp/selene-install.sh
less /tmp/selene-install.sh
sh /tmp/selene-install.sh
```

The bootstrap installs only `~/.local/bin/selene`, without `sudo`. It verifies the published checksum, runs an internal version self-test, and atomically activates the binary.

For a specific release or a dry run:

```bash
sh install.sh --version v0.1.0
sh install.sh --dry-run --version v0.1.0
```

The release must contain:

```text
selene-linux-amd64
selene-linux-amd64.sha256
```

There is no public tag yet. To test the current source on CachyOS:

```bash
sudo pacman -S --needed go git
git clone https://github.com/YlanzinhoY/Selene.git
cd Selene
go test ./...
go build -trimpath -o selene ./cmd/selene
install -Dm755 selene "$HOME/.local/bin/selene"
```

Only the `pacman` command needs `sudo`. Build and run Selene as your regular user.

If the shell reports `selene: command not found`, run:

```bash
export PATH="$HOME/.local/bin:$PATH"
selene
```

Add that export to `~/.bashrc` or `~/.zshrc` if you want it to persist.

## 2. Use Selene

Selene is TUI-only. Start it without arguments:

```bash
selene
```

Use `↑`/`↓` to navigate, `Enter` to select, `Esc` to go back, and `q` to quit. During installation, rollback, or removal, normal exit is blocked until the transaction reaches a safe state.

The home screen contains:

- **Check compatibility:** checks Linux, native/Flatpak Steam, Steam libraries, Proton, and the desktop session without changing files.
- **Installation details:** shows every download, checksum, destination, snapshot, and activation step before installation.
- **Download and verify:** prepares the verified artifacts in Selene's cache without installing anything.
- **Install LuaTools:** displays a final confirmation, creates a snapshot, and installs the pinned stack.
- **Undo last installation:** stops Steam, restores the exact previous snapshot, restores services, and starts Steam again.
- **Remove LuaTools completely:** removes LuaTools, Lumen, slsteam-moon, settings, services, and user-level Steam integration.
- **About Selene:** shows the project's mission and current state.
- **Exit:** closes the interface.

Recommended first run:

1. Open **Check compatibility**.
2. Resolve anything marked as an error.
3. Review **Installation details**.
4. Optionally use **Download and verify**.
5. Select **Install LuaTools** and read the confirmation.
6. Open Steam after installation succeeds.

Operational CLI commands are intentionally unavailable. `--version` exists only for release diagnostics and the bootstrap self-test.

## 3. Rollback versus complete removal

These actions have different meanings:

- **Undo last installation** restores exactly what existed before a Selene installation. If Lumen or slsteam-moon already existed at that point, they are preserved. Every successful rollback restarts Steam so the restored launch configuration takes effect immediately.
- **Remove LuaTools completely** aims for a clean native Steam launch. It removes the full LuaTools, Lumen, and slsteam-moon user stack, including plugin data, SLSsteam settings, shell entries, desktop integration, and user services.

Complete removal does not delete games, Steam libraries, Steam itself, the Selene binary, Selene's download cache, or safety snapshots.

Removal uses `setup.sh uninstall` from the same pinned slsteam-moon ZIP verified by SHA-256. Selene creates a new safety snapshot before starting. If removal fails, it restores the installed stack and restarts Steam automatically.

After successful complete removal, Selene does not allow an older rollback to cross that boundary and reactivate the removed stack. An interrupted removal remains recoverable from **Undo last installation**, where it appears as **Recover interrupted removal**.

## 4. First CachyOS test

CachyOS recommends **CachyOS Hello → Apps/Tweaks → Install Gaming packages**. The equivalent packages are:

```bash
sudo pacman -S cachyos-gaming-meta cachyos-gaming-applications
```

The applications package includes Steam. Run Selene later as your regular user, without `sudo`. See the [CachyOS gaming guide](https://wiki.cachyos.org/configuration/gaming/) and [ArchWiki Steam documentation](https://wiki.archlinux.org/title/Steam).

Test checklist:

1. Update CachyOS and install its gaming packages.
2. Open native Steam, sign in, and let its first update finish.
3. Close Steam.
4. Install or build Selene.
5. Open **Check compatibility** and confirm native Steam is detected.
6. Open **Installation details** and confirm the runtime destination is `~/.local/share`.
7. Install LuaTools from the TUI.
8. Open Steam and confirm Lumen/LuaTools loads.
9. Use **Undo last installation** and confirm Steam closes and starts again automatically.
10. Install again, then use **Remove LuaTools completely**.
11. Confirm `~/.local/share/SLSsteam`, `~/.local/share/Lumen`, and `slsteam-desktop-guardian.*` units are gone.
12. Confirm native Steam opens normally and games remain untouched.

When reporting a test, include the CachyOS edition, desktop environment, GPU/driver, compatibility screen results, transaction ID, and visible log. Remove usernames, tokens, and personal data first.

## 5. Files and directories

| Content | Default path |
|---|---|
| Selene executable | `~/.local/bin/selene` |
| Download cache | `~/.cache/selene/downloads` |
| Journals and snapshots | `~/.local/state/selene/transactions` |
| slsteam-moon | `~/.local/share/SLSsteam` |
| Lumen and LuaTools | `~/.local/share/Lumen` |
| SLSsteam settings | `~/.config/SLSsteam` |

XDG variables are respected for cache, configuration, state, desktop entries, and services. SLSsteam and Lumen remain in `~/.local/share` because the upstream wrapper resolves those paths directly.

## 6. Common problems

### Native Steam is not initialized

Open Steam from the desktop, wait for its initial update to finish, close it, and run **Check compatibility** again.

### Only Flatpak Steam is detected

The first Selene adapter does not install into Flatpak Steam. Use the native Steam package on CachyOS for now.

### A transaction was interrupted

Open **Undo last installation**. Selene finds the newest recoverable install or interrupted removal snapshot.

### Steam cannot be restarted after rollback

Selene looks for `/usr/bin/steam`, `/usr/games/steam`, `/usr/local/bin/steam`, and native Steam bootstrap launchers under your home directory. Reinstall or open native Steam once if none exists, then retry the rollback.

### Remove only the Selene executable

Use **Remove LuaTools completely** first if you also want to clean the Steam integration. Then remove `~/.local/bin/selene`. Removing only the executable does not remove LuaTools or snapshots.

## 7. Editing interface text

The main TUI copy is intentionally centralized:

- `internal/ui/text.go`: menu names, descriptions, activity messages, buttons, page headings, confirmations, and explanatory paragraphs.
- `internal/doctor/doctor.go`: compatibility-check results.
- `internal/planner/planner.go`: operations shown under **Installation details**.
- `internal/catalog/manifests/stable.json`: bundle and component names/descriptions.
- `README.md`: this user guide.

After editing text, run `gofmt -w internal` for Go files and `go test ./...`.

## Development

Requirements:

- Go 1.24 or newer.
- Git.
- Linux for real Steam/Proton integration tests.

```bash
git clone https://github.com/YlanzinhoY/Selene.git
cd Selene
go mod download
go test ./...
sh scripts/test-install.sh
go run ./cmd/selene
```

Build for Linux:

```bash
go build -trimpath -o bin/selene ./cmd/selene
```

Cross-compile from Windows PowerShell:

```powershell
$env:GOOS = "linux"
$env:GOARCH = "amd64"
go build -trimpath -o bin/selene-linux-amd64 ./cmd/selene
Remove-Item Env:GOOS
Remove-Item Env:GOARCH
```

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
docs/                technical decisions
```

## Security

Selene never executes the mutable LuaTools `install.sh` from `main`. It downloads exact release artifacts over HTTPS, verifies size and SHA-256, inspects archives, extracts into private staging, blocks inherited injection variables, and executes pinned slsteam-moon setup logic without sudo.

Every mutation begins after its affected paths have been snapshotted. See [docs/TRANSACTIONS.md](docs/TRANSACTIONS.md) and [SECURITY.md](SECURITY.md).

## Roadmap

- [x] Charm TUI.
- [x] Linux, Steam, and Proton compatibility checks.
- [x] Pinned real artifacts and SHA-256 verification.
- [x] Transactional user-only installation.
- [x] Persistent manual and automatic rollback.
- [x] Automatic Steam restart after rollback.
- [x] Transactional complete removal.
- [x] TUI-only operational interface.
- [ ] Real CachyOS hardware validation.
- [ ] Signed catalog and releases.
- [ ] Native Flatpak Steam support.
- [ ] Snapshot retention policy.
- [ ] GitHub Release and AUR packaging.

## Independence

Selene is not affiliated with Valve, Steam, LuaTools, or the authors of integrated components. Each upstream project retains its own authorship and license.

Before the first public release, the maintainer should finalize the module path, authorship, and Selene license.
