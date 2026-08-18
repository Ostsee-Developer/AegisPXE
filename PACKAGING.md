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
- safe schema migration invocation once persistent schemas exist.

Starting with the 0.0.3 discovery slice, the package also owns the availability of its stage-1 PXE runtime dependencies. When `tftpd-hpa` already defines a safe absolute `TFTP_DIRECTORY`, package configuration may materialize AegisPXE's iPXE stage-1 files into that root. It must not replace an existing file whose content differs from the packaged iPXE asset. See [`docs/PXE_RUNTIME.md`](docs/PXE_RUNTIME.md).

The package must not silently choose DHCP ranges, router credentials, installation profiles, machine policy or privileged administrative exposure.

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
- `/etc/aegispxe/` for administrator-owned configuration,
- `/var/lib/aegispxe/` for persistent application state,
- `/var/log/aegispxe/` only for log material that is intentionally persisted as files; structured service logs may otherwise use journald,
- `/run/aegispxe/` for runtime sockets and transient state.

Secrets must never be installed as package payload defaults.

## Service behavior

Package installation may install and register systemd units. A service may start automatically only when its default configuration is safe for the privilege of each surface.

The documented network-reachable PXE default is permitted because only the low-privilege PXE route allowlist is present there. Administrative routes remain on the separate loopback/trusted-proxy Studio surface.

If required privileged configuration is missing or inconsistent, the service should fail closed with a precise status and structured diagnostic rather than guessing values.

Uninstalling the package must not silently destroy persistent state. Purge semantics, once implemented, must be documented separately and require an explicit administrator action.

## Upgrade contract

Upgrades must be idempotent and preserve administrator configuration and persistent state.

Database or state migrations must:

1. be versioned,
2. be testable independently,
3. emit structured logs,
4. fail closed without partially advancing the recorded schema version,
5. have an explicit rollback or recovery story where destructive transformation is possible.

## Build reproducibility

CI must build the `.deb` from repository source. The package version and embedded application version must both be deterministic derivations of `VERSION` and CI must verify the expected mapping.

Before an artifact may be published, CI must inspect at least:

- package name,
- version,
- architecture,
- installed file list,
- ownership/mode expectations for security-sensitive paths,
- required systemd units,
- package dependency metadata,
- embedded application version.

A package that builds but does not install cleanly in a fresh supported VM is not considered valid.

## Testing gate

Starting with the first executable milestone, CI/E2E must include a clean-package test:

1. create a fresh supported Debian VM/container environment,
2. install the generated `.deb`,
3. verify package contents and service state,
4. exercise the milestone's functionality,
5. upgrade from the previous development package once upgrades become relevant,
6. remove the package and verify that persistent state is not unexpectedly destroyed.

## Release principle

AegisPXE is developed and tested as the package administrators will actually install. A source-tree-only success does not satisfy the release gate.
