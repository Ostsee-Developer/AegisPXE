#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

VERSION="$(bash scripts/version.sh project)"
OUTPUT="${OUTPUT:-$ROOT_DIR/aegispxe-server}"
TARGET="${TARGET:-./cmd/aegispxe-server}"
BUILD_GOOS="${GOOS:-$(go env GOOS)}"
BUILD_GOARCH="${GOARCH:-$(go env GOARCH)}"
BUILD_CGO="${CGO_ENABLED:-0}"

mkdir -p "$(dirname "$OUTPUT")"

CGO_ENABLED="$BUILD_CGO" GOOS="$BUILD_GOOS" GOARCH="$BUILD_GOARCH" go build \
  -trimpath \
  -ldflags "-s -w -X main.version=$VERSION" \
  -o "$OUTPUT" \
  "$TARGET"

echo "$OUTPUT"
