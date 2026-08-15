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
Instala o binário do Selene no escopo do usuário.

Uso:
  sh install.sh [opções]

Opções:
  --version TAG       Instala uma release específica, por exemplo v0.1.0
  --repo OWNER/REPO   Repositório GitHub (padrão: YlanzinhoY/Selene)
  --install-dir DIR   Destino (padrão: ~/.local/bin)
  --dry-run           Mostra URLs e destino sem baixar ou alterar arquivos
  -h, --help          Mostra esta ajuda

Variáveis equivalentes:
  SELENE_VERSION, SELENE_REPO, SELENE_INSTALL_DIR, SELENE_BASE_URL

Cada release precisa publicar estes dois assets:
  selene-linux-amd64
  selene-linux-amd64.sha256
EOF
}

require_value() {
	[ "$#" -ge 2 ] || fail "a opção $1 exige um valor"
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
			fail "opção desconhecida: $1"
			;;
	esac
done

[ "$#" -eq 0 ] || fail "argumento inesperado: $1"
[ "$(uname -s 2>/dev/null || true)" = "Linux" ] || fail "este instalador suporta somente Linux"
[ "$(id -u 2>/dev/null || printf '0')" -ne 0 ] || fail "não execute este instalador como root"
[ -n "${HOME:-}" ] || fail "a variável HOME não está definida"

case "$(uname -m 2>/dev/null || true)" in
	x86_64|amd64) ARCH="amd64" ;;
	*) fail "arquitetura não suportada; a release atual exige x86_64/amd64" ;;
esac

case "$REPOSITORY" in
	*[!A-Za-z0-9._/-]*|/*|*/|*//*|*/*/*) fail "repositório GitHub inválido: $REPOSITORY" ;;
	*/*) : ;;
	*) fail "use --repo no formato OWNER/REPO" ;;
esac

case "$VERSION" in
	latest) : ;;
	""|*[!A-Za-z0-9._-]*) fail "tag de release inválida: $VERSION" ;;
esac

case "$INSTALL_DIR" in
	/*) : ;;
	*) fail "o diretório de instalação deve ser absoluto: $INSTALL_DIR" ;;
esac

ASSET="${PROGRAM}-linux-${ARCH}"
CHECKSUM_ASSET="${ASSET}.sha256"

if [ -n "$BASE_URL" ]; then
	case "$BASE_URL" in
		https://*) : ;;
		*) fail "SELENE_BASE_URL precisa usar HTTPS" ;;
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
	say "Selene installer (simulação)"
	say "  release:  $VERSION"
	say "  binário: $BINARY_URL"
	say "  checksum: $CHECKSUM_URL"
	say "  destino:  $DESTINATION"
	exit 0
fi

for command in curl sha256sum mktemp awk tr cp chmod mkdir mv rm; do
	command -v "$command" >/dev/null 2>&1 || fail "comando obrigatório não encontrado: $command"
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

TEMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/selene-install.XXXXXX")" || fail "não foi possível criar o diretório temporário"
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

say "Baixando Selene ${VERSION} para Linux/${ARCH}..."
download "$BINARY_URL" "$BINARY_PATH" || fail "falha ao baixar $BINARY_URL"
download "$CHECKSUM_URL" "$CHECKSUM_PATH" || fail "a release não publicou o checksum obrigatório"

EXPECTED="$(awk 'NF { print $1; exit }' "$CHECKSUM_PATH" | tr 'A-F' 'a-f')"
case "$EXPECTED" in
	*[!0-9a-f]*|"") fail "arquivo de checksum inválido" ;;
esac
[ "${#EXPECTED}" -eq 64 ] || fail "o SHA-256 publicado não possui 64 caracteres"

ACTUAL="$(sha256sum "$BINARY_PATH" | awk '{ print $1 }' | tr 'A-F' 'a-f')"
[ "$ACTUAL" = "$EXPECTED" ] || fail "SHA-256 divergente; o binário não será instalado"

mkdir -p -- "$INSTALL_DIR" || fail "não foi possível criar $INSTALL_DIR"
[ ! -d "$DESTINATION" ] || fail "o destino é um diretório: $DESTINATION"

INSTALL_CANDIDATE="$(mktemp "${INSTALL_DIR}/.selene.XXXXXX")" || fail "não foi possível preparar a instalação atômica"
cp -- "$BINARY_PATH" "$INSTALL_CANDIDATE" || fail "não foi possível copiar o binário verificado"
chmod 0755 "$INSTALL_CANDIDATE" || fail "não foi possível tornar o binário executável"
"$INSTALL_CANDIDATE" version >/dev/null 2>&1 || fail "o binário baixado não passou no autoteste"
mv -f -- "$INSTALL_CANDIDATE" "$DESTINATION" || fail "não foi possível ativar $DESTINATION"
INSTALL_CANDIDATE=""

say "Selene instalado em $DESTINATION"
case ":${PATH:-}:" in
	*":${INSTALL_DIR}:"*) say "Execute: selene" ;;
	*)
		say "Adicione o diretório ao PATH e abra outro terminal:"
		say "  export PATH=\"${INSTALL_DIR}:\$PATH\""
		;;
esac
