#!/usr/bin/env sh

set -eu

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
REPO_ROOT="$(dirname -- "$SCRIPT_DIR")"
TEST_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/selene-bootstrap-test.XXXXXX")"
trap 'rm -rf -- "$TEST_ROOT"' EXIT
trap 'exit 130' HUP INT TERM

FIXTURES="$TEST_ROOT/fixtures"
MOCK_BIN="$TEST_ROOT/mock-bin"
INSTALL_DIR="$TEST_ROOT/install/bin"
mkdir -p "$FIXTURES" "$MOCK_BIN"

(
	cd "$REPO_ROOT"
	GOOS=linux GOARCH=amd64 go build -trimpath -o "$FIXTURES/selene-linux-amd64" ./cmd/selene
)
(
	cd "$FIXTURES"
	sha256sum selene-linux-amd64 > selene-linux-amd64.sha256
)

cat > "$MOCK_BIN/curl" <<'EOF'
#!/usr/bin/env sh
set -eu
output=""
url=""
while [ "$#" -gt 0 ]; do
	case "$1" in
		--output)
			output="$2"
			shift 2
			;;
		*)
			url="$1"
			shift
			;;
	esac
done
[ -n "$output" ] && [ -n "$url" ]
cp -- "$SELENE_TEST_FIXTURES/${url##*/}" "$output"
EOF
chmod 0755 "$MOCK_BIN/curl"

PATH="$MOCK_BIN:$PATH" \
	SELENE_TEST_FIXTURES="$FIXTURES" \
	SELENE_BASE_URL="https://downloads.invalid/selene" \
	SELENE_INSTALL_DIR="$INSTALL_DIR" \
	sh "$REPO_ROOT/install.sh"

[ -x "$INSTALL_DIR/selene" ]
"$INSTALL_DIR/selene" --version >/dev/null

printf 'corruption' >> "$FIXTURES/selene-linux-amd64"
if PATH="$MOCK_BIN:$PATH" \
	SELENE_TEST_FIXTURES="$FIXTURES" \
	SELENE_BASE_URL="https://downloads.invalid/selene" \
	SELENE_INSTALL_DIR="$TEST_ROOT/rejected/bin" \
	sh "$REPO_ROOT/install.sh" >"$TEST_ROOT/rejected.log" 2>&1; then
	printf '%s\n' "test-install: a mismatched checksum was accepted" >&2
	exit 1
fi
[ ! -e "$TEST_ROOT/rejected/bin/selene" ]

printf '%s\n' "test-install: ok"
