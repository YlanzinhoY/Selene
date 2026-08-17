# Selene 🌙

> There are truths that only the light of the moon can reveal.

Selene is a friendly terminal application for installing, checking, rolling back, and completely removing the LuaTools stack on Linux. It is written in Go for people who use native Steam, including games running through Proton.

Created by [YlanzinhoY](https://github.com/YlanzinhoY) as an independent community project.

## Why Selene

- A TUI built with Bubble Tea, Lip Gloss, and Bubbles.
- Pinned LuaTools, Lumen, and slsteam-moon artifacts verified with SHA-256.
- Clear installation details before anything changes.
- User-only installation without `sudo`.
- Persistent snapshots and automatic recovery on failure.
- Rollback that restores the previous state and restarts Steam.
- Complete LuaTools removal without deleting Steam or games.
- Community plugins, starting with a safe alias for an existing Steam library on a mounted NTFS disk.

## Goals

- Keep the normal experience entirely inside the TUI.
- Avoid administrative privileges whenever possible.
- Explain every change before applying it.
- Verify every download with a pinned size and SHA-256 digest.
- Snapshot affected files before mutation.
- Restore the previous state and restart Steam after rollback.
- Remove the full LuaTools, Lumen, and slsteam-moon integration without touching games.

## Compatibility

Selene v0.0.2 currently requires:

- Linux `x86_64/amd64`.
- Native Steam opened at least once.
- A regular desktop user session.
- Internet access for artifacts not already cached.

Wine and Proton do not block Selene. Each game continues using the compatibility tool selected in Steam. Flatpak Steam can be detected, but it is not an installation target yet.

Never run Selene with `sudo`.

## Install v0.0.3

Download and inspect the bootstrap before running it:

```bash
curl --proto '=https' --tlsv1.2 -fL \
  https://raw.githubusercontent.com/YlanzinhoY/Selene/main/install.sh \
  -o /tmp/selene-install.sh

less /tmp/selene-install.sh
sh /tmp/selene-install.sh --version v0.0.3
```

Press `q` to close `less`. The bootstrap verifies the release checksum and atomically installs only `~/.local/bin/selene`.

If `selene` is not found after installation:

```bash
export PATH="$HOME/.local/bin:$PATH"
selene
```

See the [user guide](docs/USER_GUIDE.md) for a dry run, detailed usage, paths, and troubleshooting.

## Use

Start the TUI without arguments:

```bash
selene
```

Use `↑`/`↓` to navigate, `Enter` to select, `Esc` to go back, and `q` to quit.

A good first run is:

1. Open **Check compatibility**.
2. Review **Installation details**.
3. Select **Install LuaTools** and read the confirmation.
4. Open Steam after installation succeeds.

Use **Undo last installation** to restore the exact previous snapshot. Use **Remove LuaTools completely** when you want to remove the full LuaTools, Lumen, and slsteam-moon user integration. Neither action deletes games.

**Selene Plugins** includes **Shared Steam library (NTFS)**. It finds an existing Steam library on an already-mounted Windows game disk and, after confirmation, creates a user-only symbolic link. It never mounts disks, changes games, or edits Steam's settings; add the created link in Steam's **Settings → Storage** when needed.

## Safety

Selene does not execute the mutable LuaTools installer from `main`. It downloads exact release artifacts over HTTPS, verifies their expected size and SHA-256 digest, inspects archives, and stages files privately.

Every mutation starts with a snapshot. Failed installation or removal triggers recovery, and Steam is restarted after rollback so the restored configuration takes effect.

Read [Security](SECURITY.md) and [Transactions and rollback](docs/TRANSACTIONS.md) for the full trust and recovery model.

## Documentation

| Guide | Purpose |
|---|---|
| [User guide](docs/USER_GUIDE.md) | Installation, TUI actions, rollback, removal, and troubleshooting |
| [CachyOS testing](docs/CACHYOS_TESTING.md) | Hardware validation checklist and report format |
| [Transactions](docs/TRANSACTIONS.md) | Snapshots, journals, rollback, and recovery boundaries |
| [Architecture](docs/ARCHITECTURE.md) | Components and trust boundaries |
| [Catalog](docs/CATALOG.md) | Pinned upstream artifacts and manifest maintenance |
| [Contributing](CONTRIBUTING.md) | Development, interface text, tests, and builds |
| [Security](SECURITY.md) | Security rules and vulnerability reporting |

## Development

```bash
git clone https://github.com/YlanzinhoY/Selene.git
cd Selene
go test ./...
go run ./cmd/selene
```

See [CONTRIBUTING.md](CONTRIBUTING.md) before changing code, copy, catalogs, or release artifacts.

## Roadmap

- [x] Charm TUI.
- [x] Linux, Steam, and Proton compatibility checks.
- [x] Pinned real artifacts and SHA-256 verification.
- [x] Transactional user-only installation.
- [x] Persistent manual and automatic rollback.
- [x] Automatic Steam restart after rollback.
- [x] Transactional complete removal.
- [x] TUI-only operational interface.
- [x] 100% compatibility with Selene's supported native Steam workflows on CachyOS and Bazzite.
- [ ] Signed catalog and releases.
- [ ] Native Flatpak Steam support.
- [ ] Snapshot retention policy.
- [ ] Automated GitHub releases and AUR packaging.

## Independence

Selene is not affiliated with Valve, Steam, LuaTools, or the authors of the integrated components. Each upstream project retains its own authorship and license.
