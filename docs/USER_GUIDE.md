# Selene user guide

This guide covers installation, everyday use, rollback, complete removal, local files, and common problems.

## Requirements

- Linux `x86_64/amd64`.
- Native Steam, not Flatpak Steam yet.
- Steam opened at least once so its initial update can finish.
- A regular desktop user session.
- Internet access for artifacts not already cached.

Wine and Proton do not block Selene. Selene changes the native Steam client integration while games continue using the compatibility tool selected in Steam.

Never run Selene with `sudo`.

## Install Selene

Download and inspect the bootstrap:

```bash
curl --proto '=https' --tlsv1.2 -fL \
  https://raw.githubusercontent.com/YlanzinhoY/Selene/main/install.sh \
  -o /tmp/selene-install.sh

less /tmp/selene-install.sh
sh /tmp/selene-install.sh --version v0.0.2
```

Press `q` to close `less`. The bootstrap installs `~/.local/bin/selene` without `sudo`, verifies the published SHA-256 checksum, runs a version self-test, and activates the binary atomically.

Preview the URLs and destination without changing files:

```bash
sh /tmp/selene-install.sh --dry-run --version v0.0.2
```

If the shell reports `selene: command not found`:

```bash
export PATH="$HOME/.local/bin:$PATH"
selene
```

Add that export to `~/.bashrc` or `~/.zshrc` if you want it to persist.

## Use the TUI

Start Selene without arguments:

```bash
selene
```

Use `↑`/`↓` to navigate, `Enter` to select, `Esc` to go back, and `q` to quit. During installation, rollback, or removal, normal exit is blocked until the transaction reaches a safe state.

The home screen contains:

- **Check compatibility:** checks Linux, native and Flatpak Steam, Steam libraries, Proton, and the desktop session without changing files.
- **Installation details:** shows downloads, checksums, destinations, snapshots, and activation steps before installation.
- **Download and verify:** prepares verified artifacts in Selene's cache without installing them.
- **Install LuaTools:** creates a snapshot and installs the pinned LuaTools stack after confirmation.
- **Undo last installation:** restores the exact previous snapshot and restarts Steam.
- **Remove LuaTools completely:** removes LuaTools, Lumen, slsteam-moon, settings, services, and user-level Steam integration.
- **About Selene:** shows the mission, current state, and creator credit.

Recommended first run:

1. Open **Check compatibility**.
2. Resolve anything marked as an error.
3. Review **Installation details**.
4. Optionally use **Download and verify**.
5. Select **Install LuaTools** and read the confirmation.
6. Open Steam after installation succeeds.

Selene intentionally exposes no operational CLI commands. The internal `--version` flag exists only for release diagnostics and bootstrap validation.

## Rollback versus complete removal

These actions have different purposes:

- **Undo last installation** restores exactly what existed before a Selene installation. Anything that already existed at that point is preserved. Selene stops Steam, restores the snapshot and related services, then starts Steam again.
- **Remove LuaTools completely** aims for a clean native Steam launch. It removes the LuaTools, Lumen, and slsteam-moon user stack, including plugin data, SLSsteam settings, shell entries, desktop integration, and user services.

Complete removal does not delete games, Steam libraries, Steam itself, the Selene executable, Selene's download cache, or safety snapshots.

Removal uses `setup.sh uninstall` from the pinned slsteam-moon ZIP verified by SHA-256. Selene creates another safety snapshot first. If removal fails, it restores the installed stack and restarts Steam automatically.

After successful complete removal, an older rollback cannot cross that boundary and reactivate the removed stack. An interrupted removal appears under **Undo last installation** as **Recover interrupted removal**.

## Files and directories

| Content | Default path |
|---|---|
| Selene executable | `~/.local/bin/selene` |
| Download cache | `~/.cache/selene/downloads` |
| Journals and snapshots | `~/.local/state/selene/transactions` |
| slsteam-moon | `~/.local/share/SLSsteam` |
| Lumen and LuaTools | `~/.local/share/Lumen` |
| SLSsteam settings | `~/.config/SLSsteam` |

XDG variables are respected for cache, configuration, state, desktop entries, and services. SLSsteam and Lumen remain in `~/.local/share` because the upstream wrapper resolves those paths directly.

## Common problems

### Native Steam is not initialized

Open Steam from the desktop, wait for its initial update to finish, close it, and run **Check compatibility** again.

### Only Flatpak Steam is detected

The current adapter does not install into Flatpak Steam. Use the native Steam package for now.

### A transaction was interrupted

Open **Undo last installation**. Selene finds the newest recoverable installation or interrupted removal snapshot.

### Steam cannot be restarted after rollback

Selene looks for `/usr/bin/steam`, `/usr/games/steam`, `/usr/local/bin/steam`, and native Steam bootstrap launchers under the home directory. Reinstall or open native Steam once if none exists, then retry the rollback.

### Remove only the Selene executable

Use **Remove LuaTools completely** first if you also want to clean the Steam integration. Then remove the executable:

```bash
rm "$HOME/.local/bin/selene"
```

Removing only the executable does not remove LuaTools or safety snapshots.

## More help

- [CachyOS testing guide](CACHYOS_TESTING.md)
- [Transactions and rollback](TRANSACTIONS.md)
- [Security policy](../SECURITY.md)
- [Project README](../README.md)
