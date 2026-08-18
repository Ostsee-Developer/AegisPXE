# Security Model

AegisPXE provisions privileged systems over a network. Security properties therefore belong to the architecture, not to optional hardening profiles.

## Trust boundaries

Primary boundaries:

- firmware/PXE client to AegisPXE boot service,
- installer to AegisPXE API,
- browser/CLI administrator to AegisPXE server,
- unprivileged server/worker to privileged helper,
- AegisPXE to upstream artifact sources,
- AegisPXE to local secret storage.

A MAC address is an identifier, not an authentication factor.

## Least privilege

`aegispxe-server` and normal workers run unprivileged.

The privileged helper exposes only typed, allowlisted operations that truly require root. It must never provide:

- arbitrary command execution,
- arbitrary shell execution,
- arbitrary file write paths,
- arbitrary systemctl commands,
- arbitrary environment injection.

Every helper action validates its complete input domain and emits an audit/operational log record.

## Installation credentials

Each installation receives a cryptographically random credential scoped to that installation. The credential may authorize only explicitly defined installer operations, such as:

- fetch installation seed,
- report lifecycle event,
- upload installer log chunk,
- report validation result.

It must not authorize administrative APIs or access another installation.

Credential values must not appear in query strings or logs.

## Seed security

Seeds are installation-scoped. Fetching a seed is not equivalent to claiming or starting the installation.

Seed access remains valid for the period required by the native installer and is revoked according to explicit lifecycle policy, normally after successful completion or administrative cancellation/expiry.

## Boot assignment

An assignment is consumed only after an authenticated `INSTALLER_STARTED` event for the assigned installation. Firmware fetches, bootloader retries and seed reads do not consume it.

## Secret handling

Secrets include:

- recovery keys,
- passwords,
- private keys,
- bearer tokens,
- session credentials,
- seed/lifecycle credentials.

Secrets must not be placed in:

- normal logs,
- audit details,
- URLs/query strings,
- public GRUB configuration,
- machine metadata,
- error messages.

Secret storage is accessed through a narrow vault abstraction. Recovery material is revealed only through an explicit authorized action that itself creates an audit event.

## Artifact integrity

Provisioning artifacts are identified by cryptographic digest and provenance metadata. A driver may not return an artifact as usable until integrity verification succeeds.

A hash mismatch is a hard failure. AegisPXE must never silently use an existing file whose digest does not match the expected artifact identity.

Where upstream signatures or signed checksum metadata are available, drivers/artifact resolvers should verify them in addition to the final digest.

## Administrative authorization

The initial role model should remain small. Security-sensitive actions require explicit permission checks, including:

- approving/blocking/deleting machines,
- creating/arming/cancelling installations,
- changing profiles,
- managing trusted keys,
- revealing recovery secrets,
- changing system settings.

All such actions produce audit events.

## Input validation

External identifiers, URLs, filenames, paths, hostnames, MAC addresses, driver IDs and profile values are validated at domain boundaries.

Path traversal and user-controlled filesystem destinations are forbidden. Internal paths should be constructed from typed IDs and fixed roots.

## Logging and redaction

Security decisions must be logged without logging secrets. See `OBSERVABILITY.md`.

Authentication failures, authorization failures, invalid lifecycle events, replay attempts and artifact integrity failures require structured security logs with stable error codes.

## Replay and sequencing

Installation events include a sequence or idempotency mechanism. The server must reject invalid state regressions and handle duplicate reports deterministically.

A replayed request must not create duplicate state transitions.

## Secure defaults

Defaults favor:

- SSH keys over passwords,
- root login disabled,
- minimum necessary exposed services,
- verified artifacts,
- supported/LTS operating systems where available,
- local boot when there is no explicit provisioning assignment,
- refusal rather than guessing when state is inconsistent.

## Security changes

Changes to trust boundaries, credential scope, helper permissions, artifact trust, secret handling or authorization require documentation updates and an ADR when the architectural contract changes.

A security-relevant behavior change without tests is not mergeable.