#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

VERSION_FILE="${AEGISPXE_VERSION_FILE:-$ROOT_DIR/VERSION}"
if [ ! -f "$VERSION_FILE" ]; then
  echo "version file not found: $VERSION_FILE" >&2
  exit 1
fi

VERSION="$(tr -d '[:space:]' < "$VERSION_FILE")"
if [ -z "$VERSION" ]; then
  echo "VERSION must not be empty" >&2
  exit 1
fi

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
