# PXE Runtime Bootstrap

AegisPXE 0.0.3-dev.4 makes the stage-1 PXE runtime part of the Debian package installation path and treats the distribution-provided iPXE binary as the compatibility baseline.

## Runtime dependencies

The `aegispxe` Debian package depends on:

- `ipxe`, which provides the upstream iPXE boot binaries,
- `tftpd-hpa`, which provides the TFTP service used for firmware-stage chainloading,
- `adduser`, which is used for the unprivileged AegisPXE service account.

The Debian `ipxe` package is the source of the bootloader binaries. AegisPXE does not vendor or silently replace those binaries.

AegisPXE bootstrap scripts must work with the commands available in the distribution-provided iPXE package. Optional compile-time commands are not part of the runtime contract. In particular, discovery must not require the optional `PARAM_CMD` feature (`params`/`param`). The stock discovery path passes bounded observations in a dynamic HTTP query string instead.

## TFTP root discovery

During package configuration, `/usr/lib/aegispxe/install-pxe-assets` reads `/etc/default/tftpd-hpa` and looks for `TFTP_DIRECTORY`.

Only an absolute, non-root directory is accepted. A missing, unreadable or unsafe configuration causes a diagnostic warning and no filesystem mutation.

The helper may be tested or invoked against another configuration path by setting `AEGISPXE_TFTPD_CONFIG`. This override exists for packaging/tests and explicit administrator use; normal package installation uses `/etc/default/tftpd-hpa`.

## Materialized assets

When the configured TFTP root is safe, AegisPXE materializes:

- `/usr/lib/ipxe/ipxe.efi` as `<TFTP_DIRECTORY>/ipxe.efi` for UEFI chainloading,
- `/usr/lib/ipxe/undionly.kpxe` as `<TFTP_DIRECTORY>/undionly.kpxe` for legacy BIOS chainloading.

The destination directory is created when necessary with mode `0755`. New bootloader files are installed with mode `0644`.

## Non-overwrite rule

AegisPXE never blindly overwrites an existing TFTP bootloader file.

For each managed filename:

1. if the destination does not exist, the packaged iPXE asset is copied into place;
2. if the destination already has identical content, the operation is an idempotent no-op;
3. if the destination exists with different content, AegisPXE leaves it untouched and emits a diagnostic warning.

This allows the `.deb` to make a fresh AegisPXE host boot-ready without taking ownership of an administrator's existing PXE environment.

## Configuration boundary

The package does not modify:

- DHCP ranges or leases,
- DHCP boot-selection/tag rules,
- interface bindings,
- router/firewall configuration,
- an existing `TFTP_DIRECTORY` value,
- administrator-owned boot files with differing content.

Those remain explicit infrastructure configuration. AegisPXE only supplies the runtime dependencies and safely materializes its stage-1 boot assets into an already configured `tftpd-hpa` root.
