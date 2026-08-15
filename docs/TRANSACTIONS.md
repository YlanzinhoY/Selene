# Transactions and rollback

Every installation and complete removal opens a transaction before the first upstream mutation. Journals and backups live at:

```text
${XDG_STATE_HOME:-$HOME/.local/state}/selene/transactions/<id>/
├── journal.json
├── backup/
└── stage/
```

The snapshot covers:

- `~/.local/share/SLSsteam` and `~/.local/share/Lumen`;
- slsteam-moon settings and state;
- `.bashrc`, `.zshrc`, and `.profile`;
- Steam menu, autostart, and Desktop entries plus the user association cache;
- guardian units, drop-ins, and systemd activation links;
- temporary directories used for atomic Lumen activation.

Existing files are copied with their type and permissions. Missing paths are recorded so anything created later can be removed. Narrow glob snapshots preserve original matches and remove newly created entries.

## States

- `active`: snapshot complete and mutation in progress;
- `committed`: operation validated and recovery data retained;
- `rolling_back`: restoration in progress;
- `rolled_back`: previous state restored;
- `failed`: error recorded and restoration may be retried.

Installation failure calls the verified uninstaller from the same staging directory, stops known services, restores the snapshot, and restarts Steam. Manual rollback never executes a script retained inside a snapshot; it stops Steam and services directly, restores journal data, reloads systemd, and starts Steam with the restored integration.

Complete removal creates a separate `uninstall` transaction, downloads or reuses the pinned slsteam-moon artifact, calls `setup.sh uninstall`, and validates that no wrapper, desktop tag, unit, or runtime remains. Failure restores the installed state and restarts Steam.

Rollback normally selects installation journals. An interrupted removal is also recoverable so power loss cannot leave half-removed integration. After successful complete removal, rollback cannot cross that boundary and reactivate an older stack.

## Interruptions

Downloads happen before snapshots and can be canceled safely. Once mutation begins, the TUI blocks normal exit until commit or automatic rollback. After a power loss or forced exit, open **Undo last installation** to recover the newest active or failed transaction.

Snapshots are not automatically pruned yet. This favors recovery during the MVP but may consume space proportional to previous installations.
