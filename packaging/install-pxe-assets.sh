#!/bin/sh
set -eu

CONFIG="${AEGISPXE_TFTPD_CONFIG:-/etc/default/tftpd-hpa}"

log() {
  printf 'aegispxe: %s\n' "$*" >&2
}

if [ ! -r "$CONFIG" ]; then
  log "TFTP configuration not readable at $CONFIG; PXE assets were not materialized"
  exit 0
fi

assignment="$(awk '
  /^[[:space:]]*TFTP_DIRECTORY[[:space:]]*=/ { line=$0 }
  END { print line }
' "$CONFIG")"

if [ -z "$assignment" ]; then
  log "TFTP_DIRECTORY is not configured in $CONFIG; PXE assets were not materialized"
  exit 0
fi

root="${assignment#*=}"
root="$(printf '%s\n' "$root" | sed 's/^[[:space:]]*//; s/[[:space:]]*$//')"
case "$root" in
  \"*\") root="${root#\"}"; root="${root%\"}" ;;
  \'*\') root="${root#\'}"; root="${root%\'}" ;;
esac

case "$root" in
  /*) ;;
  *)
    log "refusing non-absolute TFTP_DIRECTORY from $CONFIG"
    exit 0
    ;;
esac

if [ -z "$root" ] || [ "$root" = "/" ]; then
  log "refusing unsafe TFTP_DIRECTORY '$root'"
  exit 0
fi

install -d -m 0755 "$root"

materialize() {
  source_file="$1"
  target_name="$2"
  target_file="$root/$target_name"

  if [ ! -r "$source_file" ]; then
    log "required iPXE asset missing: $source_file"
    return 0
  fi

  if [ -e "$target_file" ] || [ -L "$target_file" ]; then
    if cmp -s "$source_file" "$target_file"; then
      log "$target_name already present in $root"
    else
      log "leaving existing $target_file unchanged because it differs from the packaged iPXE asset"
    fi
    return 0
  fi

  install -m 0644 "$source_file" "$target_file"
  log "installed $target_name into $root"
}

materialize /usr/lib/ipxe/ipxe.efi ipxe.efi
materialize /usr/lib/ipxe/undionly.kpxe undionly.kpxe
