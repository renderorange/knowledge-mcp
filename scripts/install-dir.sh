#!/usr/bin/env bash
# Prints the directory `make install` and `install.sh` should deploy the
# binary to. Resolution order (so a build can never install somewhere the
# MCP config does not read — GH-45):
#
#   1. The existing knowledge-mcp install on PATH (`command -v`), when its
#      directory is writable: upgrades land where the running server already
#      reads the binary.
#   2. Otherwise the first writable $HOME/* (or /usr/local/bin) directory on
#      PATH.
#   3. Otherwise ~/.local/bin.
#
# This mirrors install.sh's install_dir() (install.sh:58) so the two can
# never disagree.
set -euo pipefail

install_dir() {
  local dir
  local existing
  local saved_ifs="$IFS"

  # 1. Upgrade in place: the running server reads the binary that `command -v`
  #    finds, so reuse its directory when it is writable.
  existing="$(command -v knowledge-mcp 2>/dev/null || true)"
  if [[ -n "$existing" ]]; then
    dir="$(dirname "$existing")"
    if [[ -w "$dir" ]]; then
      printf '%s' "$dir"
      return 0
    fi
  fi

  # 2. First writable $HOME/* or /usr/local/bin directory on PATH.
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

  # 3. Fallback.
  local candidate="${HOME}/.local/bin"
  mkdir -p "$candidate"
  printf '%s' "$candidate"
}

install_dir