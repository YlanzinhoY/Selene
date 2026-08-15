# Catalog and manifests

Selene manifests are stored in the repository and embedded into the executable with `go:embed`. They are never fetched from an arbitrary runtime URL.

The stable catalog is:

```text
internal/catalog/manifests/stable.json
```

Each component points directly to a known upstream source:

- `swwayps/slsteam-moon`;
- `swwayps/lumen`;
- `swwayps/luatools-moon`;
- `swwayps/cloudredirect-moon` for the optional component.

The LuaTools Moon `install.sh` is useful as design reference, but Selene does not execute it and does not treat it as an integrity source.

## Updating a version

1. Read the upstream release notes and source changes.
2. Confirm repository, immutable tag, and exact artifact name.
3. Obtain the size and SHA-256 from the official release API when available.
4. Download the artifact independently and calculate SHA-256 again.
5. Inspect the full archive and reject absolute paths, `..`, unsafe links, and unexpected content.
6. Review the installation strategy and validation markers.
7. Update version, URL, size, and SHA-256 together.
8. Run `go test ./...` on Windows and Linux.
9. Commit the catalog update separately, for example:

```text
chore(catalog): pin luatools-moon vX.Y
```

Never use `latest` in the stable catalog. URLs must contain an immutable tag or commit, and the digest must be committed with the manifest.

## Strategies

- `extract`: prepare content and activate it in a user destination;
- `replace-preserve`: transactional replacement while preserving declared data;
- `copy`: verified copy of one file;
- `verified-script`: execute an entrypoint contained in the pinned artifact after verification and snapshot creation.

slsteam-moon uses `verified-script` because it owns the wrapper, desktop, PATH, and user-service logic. Its manifest declares `setup.sh` as both entrypoint and validation marker. Selene runs it with administrative access blocked.

slsteam-moon and Lumen use `${HOME}/.local/share` because that is the runtime path resolved by the upstream wrapper.
