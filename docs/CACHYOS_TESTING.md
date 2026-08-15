# Testing Selene on CachyOS

Real CachyOS hardware validation is still in progress. This checklist is for controlled community testing of native Steam installation, rollback, and complete removal.

## Prepare CachyOS

CachyOS Hello can install both `cachyos-gaming-meta` and `cachyos-gaming-applications`. The equivalent terminal command is:

```bash
sudo pacman -S --needed cachyos-gaming-meta cachyos-gaming-applications
```

The applications package includes Steam. See the official [CachyOS gaming guide](https://wiki.cachyos.org/configuration/gaming/) and [ArchWiki Steam documentation](https://wiki.archlinux.org/title/Steam).

Open native Steam, sign in, and let its first update finish. Close Steam before testing Selene. Run Selene later as the regular desktop user, never with `sudo`.

## Install Selene

Follow the pinned installation instructions in the [README](../README.md#install-v002), then start:

```bash
selene
```

## Validation checklist

1. Update CachyOS and install its gaming packages.
2. Open native Steam and let its initial update finish.
3. Close Steam.
4. Install Selene v0.0.2.
5. Open **Check compatibility** and confirm native Steam is detected.
6. Open **Installation details** and confirm the runtime destination is under `~/.local/share`.
7. Install LuaTools from the TUI.
8. Open Steam and confirm Lumen and LuaTools load.
9. Use **Undo last installation** and confirm Steam closes and starts again automatically.
10. Install again, then use **Remove LuaTools completely**.
11. Confirm `~/.local/share/SLSsteam`, `~/.local/share/Lumen`, and `slsteam-desktop-guardian.*` user units are gone.
12. Confirm native Steam opens normally and games remain untouched.

Do not manually delete transaction data during this test. The snapshots are needed to diagnose and recover interrupted operations.

## Test report

Include:

- CachyOS edition and kernel.
- Desktop environment and session type.
- GPU and driver.
- Native Steam package version.
- Compatibility screen results.
- Action that succeeded or failed.
- Transaction ID and visible operation log.
- Whether Steam restarted successfully.

Remove usernames, account names, tokens, credentials, and personal paths before sharing logs.

## Recovery

If installation or removal is interrupted, reopen Selene and select **Undo last installation**. An interrupted removal appears as **Recover interrupted removal**.

For additional details, read the [user guide](USER_GUIDE.md) and [transaction model](TRANSACTIONS.md).
