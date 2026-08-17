# Architecture

Selene separates the terminal experience from system logic so the same core can be exercised by the TUI and tests.

```text
TUI
 │
 ├── doctor      read-only compatibility checks
 ├── catalog     embedded validated manifests
 ├── planner     read-only installation details
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

The executable intentionally exposes no operational CLI commands. Running `selene` opens the TUI. The internal `--version` flag exists only for release diagnostics and bootstrap validation.

## Current boundaries

Real installation is limited to Linux `amd64`, initialized native Steam, and the current user's account. Flatpak Steam, Game Mode-specific integration, and `/usr` changes are outside the first adapter.

## Trust boundary

Selene controls acquisition, hashing, archive inspection, staging, process environment, snapshots, rollback, removal validation, and Steam restart. slsteam-moon remains responsible for its `LD_AUDIT` wrapper, desktop entries, and guardian logic. Selene executes that logic only from the exact catalog artifact.

The executor never downloads or runs a live installer from `main`. It blocks sudo, forces upstream immutable/user-only mode, removes inherited `LD_AUDIT`, `LD_PRELOAD`, and `LD_LIBRARY_PATH`, and validates essential files before committing.

Journals live outside the installed tree. Interrupted installation or removal remains recoverable from the TUI.
