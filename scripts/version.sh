#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
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

command="${1:-project}"
case "$command" in
  project)
    printf '%s\n' "$VERSION"
    ;;
  tag)
    printf 'v%s\n' "$VERSION"
    ;;
  debian)
    if [[ "$VERSION" == *-* ]]; then
      core="${VERSION%%-*}"
      prerelease="${VERSION#*-}"
      prerelease="${prerelease//-/.}"
      printf '%s~%s\n' "$core" "$prerelease"
    else
      printf '%s\n' "$VERSION"
    fi
    ;;
  prerelease)
    if [[ "$VERSION" == *-* ]]; then
      printf 'true\n'
    else
      printf 'false\n'
    fi
    ;;
  *)
    echo "usage: $0 {project|tag|debian|prerelease}" >&2
    exit 2
    ;;
esac
