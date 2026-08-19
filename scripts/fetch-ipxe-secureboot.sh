#!/usr/bin/env bash
set -euo pipefail

RELEASE="v2.0.0"
COMMIT="12798ec29aa8a64d8675c4378b99f5fe28447afb"
ASSET="ipxeboot.tar.gz"
API_BASE="https://api.github.com/repos/ipxe/ipxe"
DOWNLOAD="https://github.com/ipxe/ipxe/releases/download/${RELEASE}/${ASSET}"
OUTPUT="${1:-dist/secureboot}"

for command in curl python3 sha256sum sbverify; do
  command -v "$command" >/dev/null 2>&1 || {
    echo "missing required command: $command" >&2
    exit 1
  }
done

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
mkdir -p "$OUTPUT"

curl_common=(--fail --silent --show-error --location --proto '=https' --tlsv1.2 --retry 3 --retry-all-errors)
curl "${curl_common[@]}" -H 'Accept: application/vnd.github+json' \
  "${API_BASE}/releases/tags/${RELEASE}" > "$work/release.json"
curl "${curl_common[@]}" -H 'Accept: application/vnd.github+json' \
  "${API_BASE}/commits/${RELEASE}" > "$work/commit.json"

python3 - "$work/release.json" "$work/commit.json" "$work/release.env" <<'PY'
import json
import re
import sys

release_path, commit_path, output_path = sys.argv[1:]
with open(release_path, encoding="utf-8") as handle:
    release = json.load(handle)
with open(commit_path, encoding="utf-8") as handle:
    commit = json.load(handle)

expected_release = "v2.0.0"
expected_commit = "12798ec29aa8a64d8675c4378b99f5fe28447afb"
expected_name = "ipxeboot.tar.gz"
expected_url = f"https://github.com/ipxe/ipxe/releases/download/{expected_release}/{expected_name}"

if release.get("tag_name") != expected_release or release.get("draft") or release.get("prerelease"):
    raise SystemExit("iPXE release metadata does not match pinned stable release")
if commit.get("sha") != expected_commit:
    raise SystemExit("iPXE release tag no longer resolves to the pinned commit")
assets = [item for item in release.get("assets", []) if item.get("name") == expected_name]
if len(assets) != 1:
    raise SystemExit("iPXE release does not contain exactly one pinned network boot asset")
asset = assets[0]
if asset.get("state") != "uploaded" or asset.get("browser_download_url") != expected_url:
    raise SystemExit("iPXE release asset metadata is invalid")
size = asset.get("size")
if not isinstance(size, int) or size <= 0 or size > 256 * 1024 * 1024:
    raise SystemExit("iPXE release asset size is outside accepted bounds")
digest = asset.get("digest", "")
if not re.fullmatch(r"sha256:[0-9a-f]{64}", digest):
    raise SystemExit("GitHub did not publish a usable SHA-256 digest for the iPXE release asset")
with open(output_path, "w", encoding="utf-8") as handle:
    handle.write(f"ASSET_DIGEST={digest}\n")
    handle.write(f"ASSET_SIZE={size}\n")
PY

# shellcheck disable=SC1090
source "$work/release.env"
curl "${curl_common[@]}" "$DOWNLOAD" -o "$work/$ASSET"
actual_size="$(stat -c %s "$work/$ASSET")"
actual_digest="sha256:$(sha256sum "$work/$ASSET" | awk '{print $1}')"
if [[ "$actual_size" != "$ASSET_SIZE" || "$actual_digest" != "$ASSET_DIGEST" ]]; then
  echo "iPXE release asset failed GitHub size/digest validation" >&2
  exit 1
fi

python3 - "$work/$ASSET" "$work/extracted" <<'PY'
import os
import pathlib
import shutil
import sys
import tarfile

archive_path, output_path = sys.argv[1:]
required = {
    "ipxe-shim.efi": "/x86_64-sb/ipxe-shim.efi",
    "ipxe.efi": "/x86_64-sb/ipxe.efi",
}
os.makedirs(output_path, mode=0o700, exist_ok=True)
with tarfile.open(archive_path, "r:gz") as archive:
    members = archive.getmembers()
    for target_name, suffix in required.items():
        matches = [member for member in members if ("/" + member.name.lstrip("/")).endswith(suffix)]
        if len(matches) != 1:
            raise SystemExit(f"archive does not contain exactly one {suffix}")
        member = matches[0]
        if not member.isfile() or member.issym() or member.islnk() or member.size <= 0 or member.size > 32 * 1024 * 1024:
            raise SystemExit(f"unsafe iPXE archive member: {member.name}")
        source = archive.extractfile(member)
        if source is None:
            raise SystemExit(f"could not read iPXE archive member: {member.name}")
        target = pathlib.Path(output_path) / target_name
        with source, target.open("wb") as destination:
            shutil.copyfileobj(source, destination)
        target.chmod(0o644)
PY

for file in ipxe-shim.efi ipxe.efi; do
  if ! sbverify --list "$work/extracted/$file" 2>&1 | grep -qi 'signature'; then
    echo "iPXE Secure Boot asset $file has no Authenticode signature table" >&2
    exit 1
  fi
done

rm -rf "$OUTPUT"
mkdir -p "$OUTPUT"
install -m 0644 "$work/extracted/ipxe-shim.efi" "$OUTPUT/ipxe-shim.efi"
install -m 0644 "$work/extracted/ipxe.efi" "$OUTPUT/ipxe.efi"

IPXE_ASSET_DIGEST="$ASSET_DIGEST" python3 - "$OUTPUT" <<'PY'
import hashlib
import json
import os
import pathlib
import sys

output = pathlib.Path(sys.argv[1])
files = {}
for name in ("ipxe-shim.efi", "ipxe.efi"):
    content = (output / name).read_bytes()
    files[name] = {
        "sha256": "sha256:" + hashlib.sha256(content).hexdigest(),
        "size": len(content),
    }
manifest = {
    "upstream_release": "v2.0.0",
    "upstream_commit": "12798ec29aa8a64d8675c4378b99f5fe28447afb",
    "release_asset_sha256": os.environ["IPXE_ASSET_DIGEST"],
    "files": files,
}
(output / "manifest.json").write_text(json.dumps(manifest, sort_keys=True, separators=(",", ":")) + "\n", encoding="utf-8")
PY

echo "validated official iPXE ${RELEASE} Secure Boot bundle into $OUTPUT"
