#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST_DIR="$ROOT_DIR/dist"
APP_VERSION="${SUNA_BUILD_VERSION:-dev+$(date -u '+%Y-%m-%dT%H:%M:%SZ')}"

mkdir -p "$DIST_DIR"

build_one() {
  local arch="$1"
  local name="suna-linux-$arch"
  local bin="suna"

  CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build \
    -trimpath \
    -ldflags "-s -w -X 'github.com/alanchenchen/suna/internal/version.BuildVersion=$APP_VERSION'" \
    -o "$DIST_DIR/$bin" \
    "$ROOT_DIR"

  (
    cd "$DIST_DIR"
    rm -f "$name.tar.gz"
    tar -czf "$name.tar.gz" "$bin"
    rm -f "$bin"
  )

  ls -lh "$DIST_DIR/$name.tar.gz"
}

build_one arm64
build_one amd64
