# AegisPXE

AegisPXE is a security-first, headless provisioning platform for servers and virtual machines.

It is not an interactive PXE menu and not a general-purpose live-boot catalog. AegisPXE centrally discovers machines, assigns immutable installation specifications, boots the native unattended installer for the selected operating system, records authoritative lifecycle events and logs, and validates the resulting system.

## Core principles

1. **Headless clients**: machines never choose an operating system or profile locally.
2. **Server-authoritative provisioning**: every boot decision is made by AegisPXE.
3. **No inferred progress**: installation status changes only because an authenticated component reported a real event.
4. **Observability is mandatory**: operational I/O, state transitions, security decisions and driver operations use structured logging and correlation identifiers.
5. **Failure is data**: errors are preserved with stable error codes, context and installation-scoped logs.
6. **Immutable installation specs**: a running installation cannot change because a profile or driver was edited later.
7. **OS-native drivers**: Debian, Ubuntu and CentOS own their installer-specific behavior. Cross-driver runtime shortcuts are forbidden.
8. **Security by construction**: least privilege, scoped tokens, verified artifacts, secret redaction and a minimal privileged helper are architectural requirements.
9. **No silent state changes**: every meaningful state mutation produces an auditable event.
10. **E2E before expansion**: one complete vertical provisioning path must be reliable before another operating system or major feature is added.
11. **Small tests, sharp contracts**: one test proves one contract; complete workflows belong in E2E rather than oversized unit tests.
12. **Package what we test**: executable milestones ship and are validated as a native Debian package, not only from a source checkout.

## Initial target platform

AegisPXE is implemented in Go and initially targets:

- Debian 13
- Ubuntu Server 24.04 LTS
- Ubuntu Server 26.04 LTS
- CentOS via Kickstart after the Debian and Ubuntu paths are proven stable

## 0.0.3 discovery milestone

The headless discovery milestone is proven on a packaged installation with real UEFI PXE clients. Unknown machines register as `pending`, repeated boots resolve to the same machine ID and append discovery events rather than creating duplicates. Identity conflicts and invalid identities fail closed, and correlated audit/log records remain available across package reinstall.

A test machine can fetch `/boot/discovery.ipxe`, submit bounded identity observations, receive a non-provisioning server decision and appear immediately in the AegisPXE machine inventory.

The Debian package installs the `ipxe` and `tftpd-hpa` runtime dependencies. When `tftpd-hpa` already has a safe absolute `TFTP_DIRECTORY`, package setup materializes `ipxe.efi` and `undionly.kpxe` into that root without overwriting a different pre-existing bootloader. See [`docs/PXE_RUNTIME.md`](docs/PXE_RUNTIME.md).

The Studio is available at `/ui/` on the configured AegisPXE HTTP listener. Machine discovery remains read-only and exposes inventory, policy, architecture/firmware observations, identifiers and append-only machine events.

See [`docs/0.0.3-discovery-slice.md`](docs/0.0.3-discovery-slice.md) for the API, iPXE transport and E2E contract.

## 0.1.0 development slice

Development now targets **Debian 13 Standard**. `0.1.0-dev.1` introduced the immutable `InstallationSpec` foundation: server-assigned installation identity, pinned driver/profile revisions, storage/security snapshots, non-secret lifecycle credential identity and atomic `INSTALLATION_CREATED` audit output.

`0.1.0-dev.2` added Debian 13 artifact trust. AegisPXE verifies Debian's signed `trixie/InRelease`, resolves the moving installer alias to a versioned installer build, verifies the trusted `SHA256SUMS`, then verifies the actual amd64 netboot `linux` and `initrd.gz` bytes. InstallationSpecs pin the versioned source URL, installer version, SHA-256 digest, byte size and provenance for each artifact.

`0.1.0-dev.3` completed the two-spec boundary. `InstallationSpec` remains the immutable authoritative assignment, with unique artifact roles and an independent driver contract version. The Debian 13 driver validates that contract and deterministically renders a typed `BootSpec` containing only digest-pinned kernel/initrd references, bounded public kernel arguments and a non-secret seed reference. BootSpec is not persisted as a second source of truth.

`0.1.0-dev.4` made Debian Standard renderable without introducing a second source of truth. InstallationSpecs persist a versioned resolved ProfileSnapshot and explicit destructive target disk. Debian driver v1 fail-closes outside the unencrypted whole-disk/ext4/key-only Standard contract and renders a deterministic Preseed SeedBundle. Render operations and verified artifact loading emit correlated structured logs with stable failure codes while excluding seed, SSH-key and credential values.

`0.1.0-dev.5` established the provisioning trust and assignment foundation. Discovery identity, operator approval, an armed Machine-to-Installation assignment and cryptographic boot trust are distinct layers. An armed assignment may make non-secret boot material eligible, but lifecycle credentials and authenticated installer APIs remain blocked until cryptographic proof exists.

`0.1.0-dev.6` connected that trust model to the first real Debian boot transport. An armed `provision` Machine chains from discovery to an installation-scoped iPXE script. Kernel and initrd are served only through the verified artifact loader and only while the exact Assignment remains armed. The non-secret `preseed.cfg` is served through the same gate and iPXE injects it as `/preseed.cfg` into its magic initrd, which Debian Installer consumes as native initrd preseeding. AegisPXE therefore needs neither Debian `preseed/url` nor a custom CPIO/initrd repacker.

`0.1.0-dev.7` introduced the minimum authenticated operator boundary needed to prepare real provisioning without database-side shortcuts. AegisPXE creates a local 256-bit bootstrap operator key, exchanges it over an accepted secure transport for an 8-hour server-side session, requires per-session CSRF on mutations and rate-limits login attempts. Bootstrap keys, session tokens and CSRF values are excluded from logs. Cleartext non-loopback HTTP remains read-only.

`0.1.0-dev.8` turns that boundary into a usable Operator Console. A second fail-closed loopback listener defaults to `127.0.0.1:8091` for local or SSH-tunnel administration. The authenticated Console exposes Machine policy, Installation arm/cancel controls and the first Debian 13 Standard InstallationSpec wizard.

The wizard is deliberately narrower than the InstallationSpec model. The operator supplies desired profile values and an explicit whole-device target disk; AegisPXE owns the Debian driver contract, security baseline, artifact source, installer version, provenance and hashes. Input validation happens before network artifact resolution, then the normal signed Debian trust chain verifies kernel/initrd metadata before their descriptors are frozen into the immutable spec. Creation and arming remain separate actions so the resulting spec can be reviewed before the next PXE boot becomes destructive.

`0.1.0-dev.9` separates the network surfaces instead of treating PXE and Studio as one listener. The package PXE default is network reachable on port 8090 and only exposes health, discovery and `/boot/` transport. Studio has a separate listener and path allowlist. Non-loopback Studio binding requires an explicit Trusted Proxy boundary, allowing a reverse proxy/SSO layer to perform TLS and Passkey authentication while AegisPXE still issues its own short-lived session and CSRF token.

Trusted proxy identity is accepted only from configured direct proxy CIDRs and only with the configured HTTPS protocol and identity headers. Header names are configurable, ordinary LAN clients cannot forge proxy authority, and no public Studio hostname or DNS origin is compiled into AegisPXE. The local bootstrap key remains a loopback recovery path.

The same dev.9 slice hardens the first real Debian E2E finish path. The security-sensitive Preseed late hook remains fail-closed but now writes `/var/log/aegispxe-installer.log`, records validation step markers/final exit status, prepares `/run/sshd`, ensures host keys and then runs `sshd -t`. Debian's native `finish-install/reboot_in_progress` remains responsible for unattended completion rather than hiding hook failures with forced success.

All public boot reads remain retryable and non-consuming. Discovery, boot-script rendering, kernel/initrd reads and Preseed reads do not emit `INSTALLER_STARTED`, consume the Assignment or release lifecycle credentials. Those operations remain behind authenticated installer telemetry and cryptographic boot trust.

Studio exposes Machine and Installation views with immutable InstallationSpec details, resolved ProfileSnapshot, verified artifact provenance, target disk/security intent, assignment state and explicit trust gates. SSH public-key payloads and lifecycle credential metadata are not rendered.

See [`docs/0.1.0-installation-spec.md`](docs/0.1.0-installation-spec.md), [`docs/0.1.0-artifact-verification.md`](docs/0.1.0-artifact-verification.md), [`docs/0.1.0-boot-spec.md`](docs/0.1.0-boot-spec.md), [`docs/0.1.0-debian-preseed.md`](docs/0.1.0-debian-preseed.md), [`docs/0.1.0-trust-assignment.md`](docs/0.1.0-trust-assignment.md), [`docs/0.1.0-operator-auth.md`](docs/0.1.0-operator-auth.md), [`docs/0.1.0-operator-console.md`](docs/0.1.0-operator-console.md) and [`docs/adr/0004-studio-trusted-proxy.md`](docs/adr/0004-studio-trusted-proxy.md).

## Project constitution

These documents are normative for implementation and review:

- [Architecture](ARCHITECTURE.md)
- [Observability](OBSERVABILITY.md)
- [Security](SECURITY.md)
- [Installation lifecycle](LIFECYCLE.md)
- [OS driver contract](DRIVER_CONTRACT.md)
- [Testing strategy](TESTING.md)
- [Debian packaging contract](PACKAGING.md)
- [Contribution rules](CONTRIBUTING.md)
- [Coding-agent instructions](AGENTS.md)
- [Profile schema principles](docs/PROFILE_SCHEMA.md)
- [Error-code contract](docs/ERROR_CODES.md)
- [Machine discovery contract](docs/MACHINE_DISCOVERY.md)
- [Roadmap](docs/ROADMAP.md)
- [Architecture decisions](docs/adr/)

The Project Constitution workflow verifies that foundational contracts remain present and documented.

## Current milestone

**0.1.0: Debian 13 Standard.**

The current development step is the real unattended Debian Standard E2E through the separated PXE/Studio surfaces. Once the late-hook/finish path is proven repeatable, the next hard trust boundary is cryptographic boot trust plus authenticated installer lifecycle telemetry so a real installer can truthfully report `INSTALLER_STARTED`, later stages, logs and terminal success/failure without treating MAC/SMBIOS identity or reverse-proxy operator identity as installer authentication.
