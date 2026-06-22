#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST_DIR="$ROOT_DIR/dist"
APP_VERSION="${SUNA_BUILD_VERSION:-dev+$(date -u '+%Y-%m-%dT%H:%M:%SZ')}"

mkdir -p "$DIST_DIR"

build_one() {
  local arch="$1"
  local name="suna-windows-$arch"
  local exe="suna.exe"

  CGO_ENABLED=0 GOOS=windows GOARCH="$arch" go build \
    -trimpath \
    -ldflags "-s -w -X 'github.com/alanchenchen/suna/internal/tui.appVersion=$APP_VERSION'" \
    -o "$DIST_DIR/$exe" \
    "$ROOT_DIR"

  (
    cd "$DIST_DIR"
    rm -f "$name.zip"
    zip -9 "$name.zip" "$exe"
    rm -f "$exe"
  )

  ls -lh "$DIST_DIR/$name.zip"
}

build_one arm64
build_one amd64
