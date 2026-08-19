# AegisPXE Debian Packaging Contract

AegisPXE ships a native Debian package from the first executable milestone onward. Packaging is part of the product contract, not a late release convenience.

## Goals

The administrator should be able to install or upgrade AegisPXE with a normal Debian package workflow and without manually copying binaries, systemd units, directories or permissions.

The package must make installation predictable while avoiding hidden infrastructure decisions.

## Initial package target

The first supported package target is Debian-family hosts using `dpkg`/APT. The package name is:

`aegispxe`

The project may add repository metadata later, but the `.deb` artifact exists before public repository distribution.

## Version source of truth

The repository-root `VERSION` file is the single source of truth for the AegisPXE application version.

Release tags, package metadata, artifact names and embedded binary versions must derive from that file. Version strings must not be duplicated in source code or workflow configuration. `scripts/version.sh` is the canonical derivation helper and `scripts/build.sh` is the canonical binary build entry point. A binary built directly with raw `go build` is intentionally identified as a non-release `dev` build.

Changing the project version therefore requires changing only `VERSION`; CI and release gates verify that generated artifacts agree with it.

The accepted release format is `MAJOR.MINOR.PATCH` with an optional SemVer-style prerelease suffix, for example:

- `0.0.3-dev.1`
- `0.5.0-beta.2`
- `0.9.0-rc.1`
- `1.0.0`

Versions carrying a prerelease suffix are published as GitHub prereleases. A plain `MAJOR.MINOR.PATCH` version is published as a stable GitHub release.

Debian package versions are derived from the same project version but use Debian's `~` prerelease ordering. For example, project version `1.0.0-rc.1` produces package version `1.0.0~rc.1`, while the binary version, Git tag and GitHub release remain `1.0.0-rc.1`. This guarantees that APT considers `1.0.0` newer than its release candidates without introducing a second version source.

## Unattended release contract

Every push to `main` runs the release workflow. The workflow reads `VERSION` and compares it using SemVer precedence against published AegisPXE releases.

- If `VERSION` is already published, the release job exits successfully without producing another release.
- If `VERSION` is lower than the highest published version, the release job fails closed to prevent accidental downgrades.
- If `VERSION` is higher, all release gates and package builds must succeed before the workflow creates or verifies the matching `v<VERSION>` tag and publishes the release.
- If a previous run created the tag but failed before publishing the release, a later run may resume only when that tag still points at the current `main` commit.

This makes changing `VERSION` and pushing to `main` the only required release operation. Manual tag creation is not part of the normal release path. The workflow may also be started manually for recovery, but it performs the exact same version checks and cannot force a duplicate or downgrade release.

The release workflow performs tagging and publication in the same workflow run. It does not depend on the generated tag starting a second workflow.

## Required package responsibilities

The package owns installation and upgrade of AegisPXE application files, including:

- versioned Go binaries,
- systemd unit files,
- static WebUI assets when those exist,
- default configuration templates,
- required runtime directories,
- ownership and filesystem permissions,
- package metadata and dependencies,
- safe schema migration invocation once persistent schemas exist,
- the validated signed iPXE UEFI Secure Boot first-stage assets required by the supported boot contract.

When `tftpd-hpa` defines a safe absolute `TFTP_DIRECTORY`, package configuration materializes package-managed PXE assets into that root. Package-managed files are refreshed atomically when their bytes differ from the package source so an upgrade cannot leave an older unsigned or stale bootloader indefinitely. Symlink and non-regular destinations are rejected instead of followed or overwritten.

The package must not silently choose DHCP ranges, router credentials, installation profiles, machine policy or privileged administrative exposure. DHCP remains administrator-owned. For the Secure Boot contract, x86-64 UEFI clients must be configured to request `ipxe-shim.efi`; AegisPXE packages and materializes that file but does not rewrite DHCP configuration.

## Secure Boot package assets

`0.1.0-dev.22` adds package-owned Secure Boot assets under:

```text
/usr/lib/aegispxe/secureboot/
  manifest.json
  ipxe-shim.efi
  ipxe.efi
```

The package build obtains these from the official iPXE `v2.0.0` network-boot release bundle pinned to upstream commit `12798ec29aa8a64d8675c4378b99f5fe28447afb`.

The build helper must fail closed unless all of these checks succeed:

1. the GitHub release metadata names exactly the pinned stable release,
2. the release tag resolves to the pinned commit,
3. exactly one expected `ipxeboot.tar.gz` release asset exists at the expected GitHub download URL,
4. GitHub provides a SHA-256 digest and bounded size for that asset,
5. downloaded bytes match that digest and size,
6. extraction resolves only the two expected x86-64 Secure Boot files and never follows filesystem symlinks or arbitrary archive paths,
7. an official in-archive hardlink may be resolved only to a regular member inside the same archive and is copied out as a new regular file,
8. both EFI files contain PE/Authenticode signature tables as checked by `sbverify`,
9. the generated package manifest records the exact release, commit, release-asset SHA-256, per-file SHA-256 and size.

At runtime AegisPXE independently validates the package-owned files against this manifest. With `AEGISPXE_SECURE_BOOT_POLICY=required`, missing, modified, symlinked, malformed or unpinned Secure Boot assets prevent the service from starting and emit `SEC025_SECURE_BOOT_ASSETS_INVALID`.

TFTP materialization uses:

```text
ipxe-shim.efi   <- package Secure Boot first stage
ipxe.efi        <- package signed second stage
undionly.kpxe   <- distribution iPXE BIOS asset for explicit non-Secure-Boot policy modes
```

A Secure Boot deployment must never hand UEFI clients the second-stage `ipxe.efi` directly as a substitute for the required shim entry point.

## Reporter package status

The reporter source and trust/telemetry primitives remain in the repository for isolated development, but the reporter executable is intentionally absent from the production `.deb` until a delivery path passes the real Debian UEFI/vTPM E2E gate.

Package smoke must assert that the suspended reporter executable is absent. Secure Boot work must not reintroduce reporter/initramfs injection into the known-good Debian installer transport.

## Listener defaults

PXE and browser administration have different functional/security requirements and therefore different package defaults.

The PXE HTTP surface must be reachable by firmware/iPXE clients. From `0.1.0-dev.9` its documented package default is:

```text
AEGISPXE_PXE_LISTEN=0.0.0.0:8090
```

This wildcard exception is allowed only because the listener is path-restricted to low-privilege health/discovery/boot transport. It must not expose Studio or operator mutation routes. Administrators should still scope the port to the provisioning network with host/network firewall policy where appropriate.

The Studio default remains loopback:

```text
AEGISPXE_STUDIO_LISTEN=127.0.0.1:8091
```

A non-loopback Studio bind requires explicit trusted reverse-proxy configuration and remains subject to the application-level trusted-proxy gate. The package must never silently convert the Studio/admin surface to wildcard exposure.

Legacy `AEGISPXE_LISTEN` and `AEGISPXE_OPERATOR_LISTEN` values remain compatibility fallbacks so an upgrade does not discard an administrator's existing listener choices.

## Filesystem contract

Initial canonical locations:

- `/usr/bin/aegispxe` for the operator CLI when introduced,
- `/usr/lib/aegispxe/` for internal executables and immutable application assets,
- `/usr/lib/aegispxe/secureboot/` for package-owned signed PXE assets and their integrity manifest,
- `/etc/aegispxe/` for administrator-owned configuration,
- `/var/lib/aegispxe/` for persistent application state,
- `/var/log/aegispxe/` only for log material that is intentionally persisted as files; structured service logs may otherwise use journald,
- `/run/aegispxe/` for runtime sockets and transient state.

Secrets must never be installed as package payload defaults.

## Service behavior

Package installation may install and register systemd units. A service may start automatically only when its default configuration is safe for the privilege of each surface.

The documented network-reachable PXE default is permitted because only the low-privilege PXE route allowlist is present there. Administrative routes remain on the separate loopback/trusted-proxy Studio surface.

The packaged Secure Boot policy is `required`. Therefore the service also requires a valid package-owned Secure Boot asset manifest and matching signed first-stage files before starting successfully. This is deliberate fail-closed behavior rather than a best-effort warning.

If required privileged or security configuration is missing or inconsistent, the service fails closed with a precise status and structured diagnostic rather than guessing values.

Uninstalling the package must not silently destroy persistent state. Purge semantics, once implemented, must be documented separately and require an explicit administrator action.

## Upgrade contract

Upgrades must be idempotent and preserve administrator configuration and persistent state.

Database or state migrations must:

1. be versioned,
2. be testable independently,
3. emit structured logs,
4. fail closed without partially advancing the recorded schema version,
5. have an explicit rollback or recovery story where destructive transformation is possible.

Signed package-managed boot assets are also upgrade state. An upgrade must replace stale package-managed TFTP copies with the package-owned validated bytes while refusing unsafe symlink/non-regular destinations.

## Build reproducibility and external release material

CI must build the `.deb` from repository source. The package version and embedded application version must both be deterministic derivations of `VERSION` and CI must verify the expected mapping.

The Secure Boot bundle is externally released signed material rather than source compiled inside AegisPXE. Its identity is therefore pinned by release tag, exact upstream commit, GitHub release-asset SHA-256/size and per-file package-manifest hashes. Build failure is preferable to consuming release material whose identity can no longer be proven.

Before an artifact may be published, CI must inspect at least:

- package name,
- version,
- architecture,
- installed file list,
- ownership/mode expectations for security-sensitive paths,
- required systemd units,
- package dependency metadata,
- embedded application version,
- Secure Boot manifest and signed EFI files,
- PE signature-table presence,
- package/TFTP byte equality after installation,
- runtime Secure Boot asset-validation status.

A package that builds but does not install cleanly in a fresh supported VM is not considered valid.

## Testing gate

Starting with the first executable milestone, CI/E2E must include a clean-package test:

1. create a fresh supported Debian VM/container environment,
2. install the generated `.deb`,
3. verify package contents and service state,
4. exercise the milestone's functionality,
5. upgrade from the previous development package once upgrades become relevant,
6. remove the package and verify that persistent state is not unexpectedly destroyed.

For Secure Boot the package-smoke lane additionally verifies the signed bundle, health-report policy, TFTP materialization and stale-asset repair. It is necessary but not sufficient for the security claim. The real OVMF/UEFI positive and negative Secure Boot E2E matrix remains mandatory before merge/release.

## Release principle

AegisPXE is developed and tested as the package administrators will actually install. A source-tree-only success does not satisfy the release gate.
