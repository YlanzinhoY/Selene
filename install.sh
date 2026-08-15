#!/usr/bin/env sh

set -eu

umask 077

PROGRAM="selene"
REPOSITORY="${SELENE_REPO:-YlanzinhoY/Selene}"
VERSION="${SELENE_VERSION:-latest}"
INSTALL_DIR="${SELENE_INSTALL_DIR:-${XDG_BIN_HOME:-${HOME:-}/.local/bin}}"
BASE_URL="${SELENE_BASE_URL:-}"
DRY_RUN=0
TEMP_DIR=""
INSTALL_CANDIDATE=""

say() {
	printf '%s\n' "$*"
}

fail() {
	printf 'selene-installer: %s\n' "$*" >&2
	exit 1
}

usage() {
	cat <<'EOF'
Installs the Selene binary for the current user.

Usage:
  sh install.sh [options]

Options:
  --version TAG       Install a specific release, for example v0.0.2
  --repo OWNER/REPO   GitHub repository (default: YlanzinhoY/Selene)
  --install-dir DIR   Destination (default: ~/.local/bin)
  --dry-run           Show URLs and destination without changing files
  -h, --help          Show this help

Equivalent environment variables:
  SELENE_VERSION, SELENE_REPO, SELENE_INSTALL_DIR, SELENE_BASE_URL

Each release must publish these two assets:
  selene-linux-amd64
  selene-linux-amd64.sha256
EOF
}

require_value() {
	[ "$#" -ge 2 ] || fail "option $1 requires a value"
}

while [ "$#" -gt 0 ]; do
	case "$1" in
		--version)
			require_value "$@"
			VERSION="$2"
			shift 2
			;;
		--repo)
			require_value "$@"
			REPOSITORY="$2"
			shift 2
			;;
		--install-dir)
			require_value "$@"
			INSTALL_DIR="$2"
			shift 2
			;;
		--dry-run)
			DRY_RUN=1
			shift
			;;
		-h|--help)
			usage
			exit 0
			;;
		--)
			shift
			break
			;;
		*)
			fail "unknown option: $1"
			;;
	esac
done

[ "$#" -eq 0 ] || fail "unexpected argument: $1"
[ "$(uname -s 2>/dev/null || true)" = "Linux" ] || fail "this installer supports Linux only"
[ "$(id -u 2>/dev/null || printf '0')" -ne 0 ] || fail "do not run this installer as root"
[ -n "${HOME:-}" ] || fail "HOME is not set"

case "$(uname -m 2>/dev/null || true)" in
	x86_64|amd64) ARCH="amd64" ;;
	*) fail "unsupported architecture; the current release requires x86_64/amd64" ;;
esac

case "$REPOSITORY" in
	*[!A-Za-z0-9._/-]*|/*|*/|*//*|*/*/*) fail "invalid GitHub repository: $REPOSITORY" ;;
	*/*) : ;;
	*) fail "use --repo in OWNER/REPO format" ;;
esac

case "$VERSION" in
	latest) : ;;
	""|*[!A-Za-z0-9._-]*) fail "invalid release tag: $VERSION" ;;
esac

case "$INSTALL_DIR" in
	/*) : ;;
	*) fail "the installation directory must be absolute: $INSTALL_DIR" ;;
esac

ASSET="${PROGRAM}-linux-${ARCH}"
CHECKSUM_ASSET="${ASSET}.sha256"

if [ -n "$BASE_URL" ]; then
	case "$BASE_URL" in
		https://*) : ;;
		*) fail "SELENE_BASE_URL must use HTTPS" ;;
	esac
	DOWNLOAD_BASE="${BASE_URL%/}"
elif [ "$VERSION" = "latest" ]; then
	DOWNLOAD_BASE="https://github.com/${REPOSITORY}/releases/latest/download"
else
	DOWNLOAD_BASE="https://github.com/${REPOSITORY}/releases/download/${VERSION}"
fi

BINARY_URL="${DOWNLOAD_BASE}/${ASSET}"
CHECKSUM_URL="${DOWNLOAD_BASE}/${CHECKSUM_ASSET}"
DESTINATION="${INSTALL_DIR}/${PROGRAM}"

if [ "$DRY_RUN" -eq 1 ]; then
	say "Selene installer (dry run)"
	say "  release:  $VERSION"
	say "  binary:   $BINARY_URL"
	say "  checksum: $CHECKSUM_URL"
	say "  target:   $DESTINATION"
	exit 0
fi

for command in curl sha256sum mktemp awk tr cp chmod mkdir mv rm; do
	command -v "$command" >/dev/null 2>&1 || fail "required command not found: $command"
done

cleanup() {
	if [ -n "$INSTALL_CANDIDATE" ]; then
		rm -f -- "$INSTALL_CANDIDATE" 2>/dev/null || true
	fi
	if [ -n "$TEMP_DIR" ]; then
		rm -rf -- "$TEMP_DIR" 2>/dev/null || true
	fi
}
trap cleanup EXIT
trap 'exit 130' HUP INT TERM

TEMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/selene-install.XXXXXX")" || fail "could not create the temporary directory"
BINARY_PATH="${TEMP_DIR}/${ASSET}"
CHECKSUM_PATH="${TEMP_DIR}/${CHECKSUM_ASSET}"

download() {
	url="$1"
	destination="$2"
	curl \
		--fail \
		--location \
		--silent \
		--show-error \
		--proto '=https' \
		--proto-redir '=https' \
		--tlsv1.2 \
		--connect-timeout 15 \
		--retry 3 \
		--output "$destination" \
		"$url"
}

say "Downloading Selene ${VERSION} for Linux/${ARCH}..."
download "$BINARY_URL" "$BINARY_PATH" || fail "failed to download $BINARY_URL"
download "$CHECKSUM_URL" "$CHECKSUM_PATH" || fail "the release did not publish the required checksum"

EXPECTED="$(awk 'NF { print $1; exit }' "$CHECKSUM_PATH" | tr 'A-F' 'a-f')"
case "$EXPECTED" in
	*[!0-9a-f]*|"") fail "invalid checksum file" ;;
esac
[ "${#EXPECTED}" -eq 64 ] || fail "the published SHA-256 does not contain 64 characters"

ACTUAL="$(sha256sum "$BINARY_PATH" | awk '{ print $1 }' | tr 'A-F' 'a-f')"
[ "$ACTUAL" = "$EXPECTED" ] || fail "SHA-256 mismatch; the binary will not be installed"

mkdir -p -- "$INSTALL_DIR" || fail "could not create $INSTALL_DIR"
[ ! -d "$DESTINATION" ] || fail "the destination is a directory: $DESTINATION"

INSTALL_CANDIDATE="$(mktemp "${INSTALL_DIR}/.selene.XXXXXX")" || fail "could not prepare the atomic installation"
cp -- "$BINARY_PATH" "$INSTALL_CANDIDATE" || fail "could not copy the verified binary"
chmod 0755 "$INSTALL_CANDIDATE" || fail "could not make the binary executable"
"$INSTALL_CANDIDATE" --version >/dev/null 2>&1 || fail "the downloaded binary failed its self-test"
mv -f -- "$INSTALL_CANDIDATE" "$DESTINATION" || fail "could not activate $DESTINATION"
INSTALL_CANDIDATE=""

say "Selene installed at $DESTINATION"
case ":${PATH:-}:" in
	*":${INSTALL_DIR}:"*) say "Execute: selene" ;;
	*)
		say "Add the directory to PATH and open another terminal:"
		say "  export PATH=\"${INSTALL_DIR}:\$PATH\""
		;;
esac
