# ADR 0009: Validated Secure Boot provisioning chain

- Status: Accepted for dev.22 implementation, pending real E2E gate
- Date: 2026-08-19

## Context

AegisPXE must support UEFI Secure Boot without modifying the Debian Installer initrd/Preseed transport that is already proven on the real Proxmox fixture. Earlier reporter experiments demonstrated that changing the installer transport introduces unacceptable boot fragility.

A Secure Boot claim also cannot be based solely on an iPXE query parameter. The chain must combine firmware enforcement, signed first-stage components, Debian's signed shim path, immutable artifact verification and fail-closed server policy.

## Decision

AegisPXE will use the official signed iPXE v2.0.0 network-boot release chain for x86-64 UEFI Secure Boot:

1. UEFI firmware executes `ipxe-shim.efi`.
2. The iPXE shim verifies and loads its matching signed `ipxe.efi` second stage.
3. iPXE reports the UEFI `SecureBoot` and `SetupMode` variables to AegisPXE discovery.
4. With policy `required`, AegisPXE serves destructive installation material only when the persisted Machine observation is `enabled`.
5. Debian kernel, native initrd and `bootnetx64.efi` are pinned from one Debian installer version through Debian signed release metadata and SHA-256 manifests.
6. The iPXE `shim` command is configured with Debian `bootnetx64.efi` before booting the Debian kernel.
7. The known-good native Debian initrd and Preseed remain unchanged.
8. On reboot, local UEFI boot prefers Debian `shimx64.efi` and firmware continues enforcing Secure Boot.

The packaged default is `AEGISPXE_SECURE_BOOT_POLICY=required`. `audit` and `disabled` exist for diagnostics and compatibility but must be explicit configuration choices.

## Validation boundaries

AegisPXE validates the official iPXE bundle at package-build time by pinning the release tag to its exact commit, verifying GitHub's release asset SHA-256/size, rejecting unsafe archive members and requiring PE signature tables. A package manifest records per-file hashes. Runtime startup hashes package files against that manifest and fails closed under `required` policy if validation fails.

The actual UEFI firmware and shim signature verification remain authoritative for executable trust. AegisPXE does not claim that the reported UEFI variable values constitute cryptographic remote attestation.

## Logging

All Secure Boot decisions are structured and correlated. Logs include policy, observed state, machine/installation/assignment IDs where relevant, signed-component hashes and stable error codes. Secret material is not part of this chain or its logs.

## Consequences

- DHCP must hand UEFI Secure Boot clients `ipxe-shim.efi`, not `ipxe.efi`.
- Secure Boot provisioning is x86-64/UEFI only in this driver version.
- New Debian InstallationSpecs use driver contract version 2 and include the signed Debian shim artifact.
- Existing driver-v1 InstallationSpecs are not silently reinterpreted as Secure Boot capable.
- Reporter/TPM runtime delivery remains a separate design and may not alter the installer initrd to satisfy this ADR.
- Merge/release is blocked until real positive and negative Secure Boot E2E tests succeed.
