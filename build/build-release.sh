#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

resolve_version() {
  if [[ -n "${SUNA_BUILD_VERSION:-}" ]]; then
    printf '%s\n' "$SUNA_BUILD_VERSION"
    return
  fi
  if git -C "$ROOT_DIR" describe --tags --exact-match >/dev/null 2>&1; then
    git -C "$ROOT_DIR" describe --tags --exact-match
    return
  fi
  local rev
  rev="$(git -C "$ROOT_DIR" rev-parse --short HEAD 2>/dev/null || true)"
  if [[ -n "$rev" ]]; then
    if ! git -C "$ROOT_DIR" diff --quiet --ignore-submodules -- 2>/dev/null || ! git -C "$ROOT_DIR" diff --cached --quiet --ignore-submodules -- 2>/dev/null; then
      rev="$rev-dirty"
    fi
    printf 'dev+%s\n' "$rev"
    return
  fi
  date -u '+dev+%Y%m%d%H%M%S'
}

# 同一次 release 使用同一个版本字符串；正式版本来自 Git tag，开发构建回退到短 SHA。
export SUNA_BUILD_VERSION="$(resolve_version)"
echo "Building Suna $SUNA_BUILD_VERSION"
rm -rf "$ROOT_DIR/dist"
mkdir -p "$ROOT_DIR/dist"

"$ROOT_DIR/build/build-darwin.sh"
"$ROOT_DIR/build/build-linux.sh"
"$ROOT_DIR/build/build-windows.sh"

ls -lh "$ROOT_DIR/dist"
