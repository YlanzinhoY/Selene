# Catalog and manifests

Selene manifests are stored in the repository and embedded into the executable with `go:embed`. They are never fetched from an arbitrary runtime URL.

The stable catalog is:

```text
internal/catalog/manifests/stable.json
```

Each component records its upstream origin in `source`:

- `swwayps/slsteam-moon`;
- `swwayps/lumen`;
- `swwayps/luatools-moon`;
- `swwayps/cloudredirect-moon` for the optional component.

The LuaTools Moon `install.sh` and the upstream mirror manifest are useful references, but Selene does not execute the script and treats neither as an integrity source: the SHA-256 committed with the manifest is the trust anchor.

## Download sources

Upstream replaces GitHub release assets in place under the same tag, which breaks the pinned size and SHA-256 (the slsteam-moon v2.8 asset was replaced several times). The artifact `url` must therefore be pinned to a commit, and `mirrors` lists fallbacks tried in order:

1. `url`: the upstream release mirror on jsDelivr, pinned to a mirror commit and addressed by the asset SHA-256, `https://cdn.jsdelivr.net/gh/swwayps/jsdelivr@<commit>/releases/<mirror-key>/<tag>/<sha256>/<asset>` (mirror keys: `slsteam-moon`, `lumen`, `plugin`);
2. the same commit and path through `https://raw.githubusercontent.com/swwayps/jsdelivr/<commit>/...`;
3. the GitHub release asset, which works only while upstream has not replaced it.

Every source is verified against the same size and SHA-256 before use, and a source that serves other bytes is skipped. `TestStableCatalogPinsPrimarySourcesToCommits` rejects a primary URL that is not pinned to a commit.

## Updating a version

1. Read the upstream release notes and source changes.
2. Confirm repository, immutable tag, and exact artifact name.
3. Read the upstream mirror manifest at `https://cdn.jsdelivr.net/gh/swwayps/jsdelivr@main/manifest.json`; it lists the commit-pinned `url`, `size`, and `sha256` of each component's current stable asset.
4. Cross-check size and SHA-256 against the asset `digest` in the GitHub release API, then download the artifact independently and calculate SHA-256 again.
5. Inspect the full archive and reject absolute paths, `..`, unsafe links, and unexpected content.
6. Review the installation strategy and validation markers.
7. Update version, URL, mirrors, size, and SHA-256 together.
8. Run `go test ./...` on Windows and Linux.
9. Commit the catalog update separately, for example:

```text
chore(catalog): pin luatools-moon vX.Y
```

Never use `latest` or a branch name in the stable catalog. The primary URL must contain a commit, and the digest must be committed with the manifest.

## Strategies

- `extract`: prepare content and activate it in a user destination;
- `replace-preserve`: transactional replacement while preserving declared data;
- `copy`: verified copy of one file;
- `verified-script`: execute an entrypoint contained in the pinned artifact after verification and snapshot creation.

slsteam-moon uses `verified-script` because it owns the wrapper, desktop, PATH, and user-service logic. Its manifest declares `setup.sh` as both entrypoint and validation marker. Selene runs it with administrative access blocked.

slsteam-moon and Lumen use `${HOME}/.local/share` because that is the runtime path resolved by the upstream wrapper.
