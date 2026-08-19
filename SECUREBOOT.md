# Secure Boot deployment

AegisPXE 0.1.0-dev.22 requires UEFI Secure Boot for destructive provisioning by default.

## DHCP / first-stage filename

For x86-64 UEFI Secure Boot clients, configure the DHCP boot filename as:

```text
ipxe-shim.efi
```

Do not point Secure Boot clients directly at `ipxe.efi`. The first stage must be the official signed iPXE shim, which then verifies and loads the correspondingly named signed iPXE second stage.

The package materializes both files into the configured TFTP root and keeps BIOS `undionly.kpxe` available for non-Secure-Boot policy modes.

## Firmware

The client must use UEFI firmware with Secure Boot enabled and SetupMode disabled. The firmware trust database must support the Microsoft UEFI CA chain needed by the official iPXE shim and Debian shim.

AegisPXE discovery records the UEFI state reported by iPXE. Under the default `required` policy only an observed `enabled` state may receive installation material.

## Configuration

```ini
AEGISPXE_SECURE_BOOT_POLICY=required
AEGISPXE_SECURE_BOOT_ASSET_DIR=/usr/lib/aegispxe/secureboot
```

Available policies:

- `required`: fail closed unless Secure Boot is observed enabled.
- `audit`: allow provisioning while logging non-enabled states.
- `disabled`: do not gate provisioning on the observation.

`required` is the package and server default.

## Diagnostics

Startup validation:

```bash
journalctl -u aegispxe --no-pager | grep -E 'secureboot|Secure Boot|SEC02[3-5]'
```

Expected successful startup fields include:

```text
component=boot.secureboot
operation=validate_assets
secure_boot_policy=required
upstream_release=v2.0.0
result=success
```

Machine discovery and boot-decision logs include `secure_boot_state`. A required-policy rejection uses `SEC023_SECURE_BOOT_REQUIRED`.

The health endpoint exposes the active policy and whether the packaged signed first-stage assets passed runtime integrity validation.

## Security scope

The discovery values are not remote attestation. The executable trust boundary is the firmware signature check plus the signed iPXE shim/second stage and Debian shim/kernel verification chain. TPM/PCR attestation remains separate future work.
