# Security

Selene is early-stage software. Installation, rollback, and complete removal modify Steam integration inside the current user's account. Artifact downloads write only to Selene's cache until the user confirms installation.

## Reporting a vulnerability

Do not publicly disclose exploitable details before a fix is available. Use GitHub private vulnerability reporting when it is available. Otherwise, open an issue requesting a private contact channel without including exploit details.

Include:

- affected version or commit;
- Linux distribution and architecture;
- native or Flatpak Steam;
- minimal reproduction steps;
- observed impact;
- logs with tokens, credentials, usernames, and personal data removed.

## Installation rules

- Downloads must use HTTPS and cryptographic integrity checks.
- Archives must never escape their staging destination.
- Changes must be disclosed before confirmation.
- Mutations must remain in user scope.
- Backups must exist before replacement.
- Failures must trigger rollback whenever possible.
- Successful rollback must restart Steam.
- Selene must never read or store Steam credentials.

The current adapter runs `setup.sh` from the pinned slsteam-moon ZIP. It never executes the mutable LuaTools installer from the `main` branch. Before execution, Selene:

- verifies artifact size and SHA-256;
- rejects unsafe ZIP files and extracts into private staging;
- creates a persistent snapshot of every known destination;
- rejects root and requires initialized native Steam;
- removes inherited `LD_*` injection variables;
- sets `SLSM_IMMUTABLE=1` and `SLSM_SUDO_DENIED=1` to block system changes and administrative prompts.

Snapshots may contain copies of user settings. They are stored in private directories under `XDG_STATE_HOME/selene/transactions` and do not expire automatically yet.

Complete removal reuses the same pinned artifact, runs only its `uninstall` action without sudo, and creates a snapshot of the installed state first. Selene refuses to commit removal while a managed desktop tag, wrapper, runtime, or service remains. Games and Steam library files are outside the removal scope.

## Artifact downloader

The downloader:

- accepts only HTTPS URLs from the embedded validated catalog;
- limits redirects and rejects HTTP downgrade;
- verifies declared size and SHA-256;
- writes into a restricted temporary file first;
- rejects path traversal, links, encrypted entries, duplicates, and excessive expansion;
- confirms required files before activating cache content.

## Selene bootstrap

The root `install.sh` installs only the Selene executable for the current user. It accepts Linux `amd64`, rejects root, enforces HTTPS redirects, requires the `.sha256` asset, runs `selene --version` as a self-test, and atomically replaces the destination.

A checksum protects against corruption or isolated binary replacement, but it is not a signature and cannot protect against a compromised release account. Signed releases remain on the roadmap.
