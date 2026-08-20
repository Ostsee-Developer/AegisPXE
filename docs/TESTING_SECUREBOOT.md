# Secure Boot E2E matrix

Use a disposable Proxmox VM. Secure Boot readiness is not established from source-only confidence.

## Positive fixture

- Machine: q35 / OVMF UEFI
- Secure Boot: enabled
- Setup Mode: disabled
- Microsoft third-party UEFI CA: available
- NIC: PXE-capable VirtIO/E1000 supported by firmware/iPXE
- DHCP UEFI filename: `ipxe-shim.efi`
- AegisPXE policy: `required`

Expected sequence:

```text
firmware validates ipxe-shim.efi
-> iPXE shim validates ipxe.efi
-> discovery secure_boot_state=enabled
-> new Debian driver-v2 InstallationSpec
-> arm
-> PXE_BOOTED
-> verified Debian kernel
-> native Debian initrd.gz
-> iPXE shim command loads verified Debian bootnetx64.efi
-> final one-shot preseed.cfg handoff
-> Debian Installer completes
-> reboot
-> local Debian shim/grub/kernel boots with Secure Boot still enabled
-> SSH key login succeeds
```

The Debian shim must be fetched before `preseed.cfg`, because successful Preseed delivery consumes the one-shot Assignment.

Required server evidence:

```text
operation=validate_assets result=success
secure_boot_policy=required
secure_boot_state=enabled
shim_digest=sha256:...
```

### Observed dev.23 positive evidence

The real Proxmox dev.23 fixture has completed one full positive run with these observed outcomes:

- AegisPXE recorded `secure_boot_state=enabled` and selected `action=provision`.
- The verified kernel, native `initrd.gz` and `bootnetx64.efi` were served before the final Preseed handoff.
- Preseed delivery consumed the one-shot Assignment only after the Debian shim artifact had been served.
- Debian 13.6 completed unattended installation.
- The next PXE decision resolved to `action=local` with no armed Assignment.
- The installed system booted in UEFI mode with `SecureBoot enabled` after reboot.
- The ESP contains Debian `shimx64.efi`, `grubx64.efi` and the standard fallback `EFI/BOOT/BOOTX64.EFI` chain.
- SSH public-key login succeeded; root login and password/keyboard-interactive authentication are disabled.
- `sshd -t` succeeds, no systemd units are failed, and the AegisPXE late-command log reports success for authorized keys, sudo, sudoers validation, SSH hardening and automatic updates.

This is positive evidence, not the complete RC gate. Repeatability, negative fixtures and upgrade/state preservation remain required.

## Negative fixture: Secure Boot disabled

Keep the Installation armed, disable Secure Boot in OVMF and PXE boot again.

Expected:

- Machine state becomes `disabled`.
- AegisPXE does not chain to the installation endpoint.
- No kernel/initrd/preseed/shim installation material is served.
- Decision is local with reason `secure_boot_required`.
- Structured log contains `SEC023_SECURE_BOOT_REQUIRED`.

## Negative fixture: Setup Mode

Enter SetupMode / clear active platform-key enforcement as supported by the test firmware.

Expected Machine state: `setup_mode` and the same fail-closed result as above.

## Negative fixture: stale or tampered package asset

Modify a TFTP copy of `ipxe.efi`, rerun `/usr/lib/aegispxe/install-pxe-assets`, and confirm it is atomically restored from the package-owned validated asset.

Separately, modify a package-owned Secure Boot asset only on a disposable server fixture and restart AegisPXE.

Expected under `required`:

- runtime manifest validation fails,
- service refuses startup,
- log contains `SEC025_SECURE_BOOT_ASSETS_INVALID`.

Restore/reinstall the package immediately after this destructive test.

## Regression fixture

Repeat the dev.21 known-good installation with Secure Boot enabled. The Debian initrd and Preseed transport must remain native and unmodified. A regression that reintroduces reporter/initramfs injection is a release blocker.
