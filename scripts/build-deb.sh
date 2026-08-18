#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VERSION="$(tr -d '[:space:]' < "$ROOT_DIR/VERSION")"
DEB_ARCH="${DEB_ARCH:-$(dpkg --print-architecture)}"

case "$DEB_ARCH" in
  amd64) GOARCH=amd64 ;;
  arm64) GOARCH=arm64 ;;
  *) echo "unsupported Debian architecture: $DEB_ARCH" >&2; exit 1 ;;
esac

BUILD_DIR="$(mktemp -d)"
trap 'rm -rf "$BUILD_DIR"' EXIT
PKG_ROOT="$BUILD_DIR/root"
OUT_DIR="$ROOT_DIR/dist"
mkdir -p "$OUT_DIR"
install -d -m 0755 \
  "$PKG_ROOT/DEBIAN" \
  "$PKG_ROOT/usr/lib/aegispxe" \
  "$PKG_ROOT/etc/aegispxe" \
  "$PKG_ROOT/lib/systemd/system"

CGO_ENABLED=0 GOOS=linux GOARCH="$GOARCH" go build \
  -trimpath \
  -ldflags "-s -w -X main.version=$VERSION" \
  -o "$PKG_ROOT/usr/lib/aegispxe/aegispxe-server" \
  "$ROOT_DIR/cmd/aegispxe-server"

install -m 0644 "$ROOT_DIR/packaging/aegispxe.env" "$PKG_ROOT/etc/aegispxe/aegispxe.env"
install -m 0644 "$ROOT_DIR/packaging/aegispxe.service" "$PKG_ROOT/lib/systemd/system/aegispxe.service"
printf '/etc/aegispxe/aegispxe.env\n' > "$PKG_ROOT/DEBIAN/conffiles"

cat > "$PKG_ROOT/DEBIAN/control" <<EOF
Package: aegispxe
Version: $VERSION
Section: admin
Priority: optional
Architecture: $DEB_ARCH
Maintainer: Ostsee-Developer
Description: Security-first headless PXE provisioning control plane
EOF

cat > "$PKG_ROOT/DEBIAN/postinst" <<'EOF'
#!/bin/sh
set -e
if command -v systemctl >/dev/null 2>&1 && [ -d /run/systemd/system ]; then
  systemctl daemon-reload
  systemctl enable --now aegispxe.service
fi
EOF

cat > "$PKG_ROOT/DEBIAN/prerm" <<'EOF'
#!/bin/sh
set -e
if command -v systemctl >/dev/null 2>&1 && [ -d /run/systemd/system ]; then
  if systemctl is-active --quiet aegispxe.service; then
    systemctl stop aegispxe.service
  fi
  if systemctl is-enabled --quiet aegispxe.service; then
    systemctl disable aegispxe.service
  fi
fi
EOF

chmod 0755 "$PKG_ROOT/DEBIAN/postinst" "$PKG_ROOT/DEBIAN/prerm"
OUT="$OUT_DIR/aegispxe_${VERSION}_${DEB_ARCH}.deb"
dpkg-deb --build --root-owner-group "$PKG_ROOT" "$OUT"
echo "$OUT"
