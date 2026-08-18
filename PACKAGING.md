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

Release tags, package metadata, artifact names and embedded binary versions must derive from that file. Version strings must not be duplicated in source code or workflow configuration. `scripts/build.sh` is the canonical binary build entry point and injects the `VERSION` value into the executable. A binary built directly with raw `go build` is intentionally identified as a non-release `dev` build.

Changing the project version therefore requires changing only `VERSION`; CI and release gates verify that generated artifacts agree with it.

The accepted release format is `MAJOR.MINOR.PATCH` with an optional SemVer-style prerelease suffix, for example:

- `0.0.3-dev.1`
- `0.5.0-beta.2`
- `0.9.0-rc.1`
- `1.0.0`

Versions carrying a prerelease suffix are published as GitHub prereleases. A plain `MAJOR.MINOR.PATCH` version is published as a stable GitHub release.

## Unattended release contract

Every push to `main` runs the release workflow. The workflow reads `VERSION` and compares it using SemVer precedence against published AegisPXE releases.

- If `VERSION` is already published, the release job exits successfully without producing another release.
- If `VERSION` is lower than the highest published version, the release job fails closed to prevent accidental downgrades.
- If `VERSION` is higher, all release gates and package builds must succeed before the workflow creates or verifies the matching `v<VERSION>` tag and publishes the release.
- If a previous run created the tag but failed before publishing the release, a later run may resume only when that tag still points at the current `main` commit.

This makes changing `VERSION` and pushing to `main` the only required release operation. Manual tag creation is not part of the normal release path.

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

The package must not silently choose network topology, DHCP ranges, interface bindings, router credentials, installation profiles or machine policy.

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

Package installation may install and register systemd units. A service may start automatically only when its default configuration is safe and cannot unintentionally expose provisioning material or bind an administrator-selected production interface.

If required configuration is missing, the service should fail closed with a precise status and structured diagnostic rather than guessing values.

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

CI must build the `.deb` from repository source. The package version and embedded application version must agree.

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
