#!/usr/bin/env bash
set -euo pipefail

OWNER="renderorange"
REPO="knowledge-mcp"
BINARY="knowledge-mcp"

VERSION=""
DRY_RUN=0
PASSTHRU=()

usage() {
  cat <<'EOF'
Usage: install.sh [--version <tag>] [--dry-run]
                  [--root <dir>] [--project <dir>] [--global <dir>]
                  [--store <dir>] [--index <dir>]

Fetches the knowledge-mcp binary for this platform from GitHub releases,
installs it, and runs `knowledge-mcp install` with the given flags.

  --version <tag>  install a specific release instead of the latest
  --dry-run        print actions without performing them

All other flags are passed to `knowledge-mcp install` unchanged.
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    -h|--help) usage; exit 0 ;;
    --dry-run) DRY_RUN=1; shift ;;
    --version)
      [[ $# -ge 2 ]] || { echo "error: --version requires a value" >&2; exit 1; }
      VERSION="$2"; shift 2 ;;
    --version=*) VERSION="${1#*=}"; shift ;;
    --) shift; PASSTHRU+=("$@"); break ;;
    *) PASSTHRU+=("$1"); shift ;;
  esac
done

case "$(uname -s)" in
  Linux) OS="linux" ;;
  Darwin) OS="darwin" ;;
  *) echo "error: unsupported OS '$(uname -s)'; supported: linux, darwin" >&2; exit 1 ;;
esac
case "$(uname -m)" in
  x86_64|amd64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) echo "error: unsupported architecture '$(uname -m)'; supported: amd64, arm64" >&2; exit 1 ;;
esac

if [[ -n "$VERSION" ]]; then
  URL="https://github.com/${OWNER}/${REPO}/releases/download/${VERSION}/${BINARY}-${OS}-${ARCH}"
else
  URL="https://github.com/${OWNER}/${REPO}/releases/latest/download/${BINARY}-${OS}-${ARCH}"
fi

install_dir() {
  local dir
  local saved_ifs="$IFS"
  IFS=: read -r -a path_entries <<< "${PATH:-}"
  IFS="$saved_ifs"
  for dir in "${path_entries[@]}"; do
    [[ -n "$dir" && -w "$dir" ]] || continue
    case "$dir" in
      "$HOME"/*|/usr/local/bin)
        printf '%s' "$dir"
        return 0
        ;;
    esac
  done
  local candidate="${HOME}/.local/bin"
  mkdir -p "$candidate"
  printf '%s' "$candidate"
}

DEST="$(install_dir)/${BINARY}"

if [[ "$DRY_RUN" == "1" ]]; then
  cat <<EOF
would download:  $URL
would install:   $DEST
would run:       $DEST install ${PASSTHRU[*]}
EOF
  exit 0
fi

TMPDIR_BIN="$(mktemp -d)"
trap 'rm -rf "$TMPDIR_BIN"' EXIT
echo "downloading $URL"
if ! curl -fsSL "$URL" -o "${TMPDIR_BIN}/${BINARY}"; then
  echo "error: download failed; check the repository has published releases and the version tag is right" >&2
  exit 1
fi
chmod +x "${TMPDIR_BIN}/${BINARY}"
mv "${TMPDIR_BIN}/${BINARY}" "$DEST"
echo "installed $DEST"

exec "$DEST" install "${PASSTHRU[@]}"