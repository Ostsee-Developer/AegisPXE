#!/bin/sh
set -eu

CONFIG="${AEGISPXE_TFTPD_CONFIG:-/etc/default/tftpd-hpa}"
SECURE_BOOT_DIR="${AEGISPXE_SECURE_BOOT_ASSET_DIR:-/usr/lib/aegispxe/secureboot}"

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
    exit 1
    ;;
esac

if [ -z "$root" ] || [ "$root" = "/" ]; then
  log "refusing unsafe TFTP_DIRECTORY '$root'"
  exit 1
fi

case "$SECURE_BOOT_DIR" in
  /*) ;;
  *)
    log "refusing non-absolute Secure Boot asset directory '$SECURE_BOOT_DIR'"
    exit 1
    ;;
esac

install -d -m 0755 "$root"

materialize() {
  source_file="$1"
  target_name="$2"
  target_file="$root/$target_name"

  if [ -L "$source_file" ] || [ ! -r "$source_file" ] || [ ! -f "$source_file" ]; then
    log "required PXE asset missing or unsafe: $source_file"
    return 1
  fi

  if [ -L "$target_file" ]; then
    log "refusing package-managed PXE asset symlink: $target_file"
    return 1
  fi
  if [ -e "$target_file" ] && [ ! -f "$target_file" ]; then
    log "refusing non-regular package-managed PXE asset: $target_file"
    return 1
  fi
  if [ -f "$target_file" ] && cmp -s "$source_file" "$target_file"; then
    chmod 0644 "$target_file"
    log "$target_name already current in $root"
    return 0
  fi

  tmp="$target_file.aegispxe.$$"
  rm -f "$tmp"
  if ! install -m 0644 "$source_file" "$tmp"; then
    rm -f "$tmp"
    return 1
  fi
  if ! mv -f "$tmp" "$target_file"; then
    rm -f "$tmp"
    return 1
  fi
  log "refreshed package-managed $target_name in $root"
}

# UEFI Secure Boot must enter through the official Microsoft-trusted iPXE
# shim. That shim loads the correspondingly named signed iPXE second stage.
materialize "$SECURE_BOOT_DIR/ipxe-shim.efi" ipxe-shim.efi
materialize "$SECURE_BOOT_DIR/ipxe.efi" ipxe.efi

# BIOS remains supported for local/non-Secure-Boot policy modes. The Debian
# 13 Secure Boot provisioning policy will never authorize BIOS provisioning
# when AEGISPXE_SECURE_BOOT_POLICY=required.
materialize /usr/lib/ipxe/undionly.kpxe undionly.kpxe
