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
An SMBIOS UUID is likewise an identifier and must not be used as an authentication factor.

## Provisioning trust layers

Provisioning trust is layered and must not collapse identification, authorization and authentication into one flag.

1. **Discovery identity** resolves observations such as MAC/SMBIOS UUID to a Machine record. It provides no secret-bearing authority.
2. **Operator approval** is represented by explicit provisioning intent for that Machine. It authorizes scheduling but does not authenticate a later network client.
3. **Armed assignment** binds one Machine to one immutable InstallationSpec. At most one assignment may be armed for a Machine at a time.
4. **Cryptographic boot trust** proves possession of a machine-bound or explicitly enrolled provisioning credential with freshness/replay protection.

Operator approval plus an armed assignment may authorize delivery of non-secret public boot material. Lifecycle credentials, authenticated installer APIs and other secrets additionally require cryptographic boot trust.

TPM-backed attestation is the preferred first hardware-backed trust mechanism for capable systems. A non-TPM fallback must be an explicit separately reviewed security mode and must never be selected silently.

See `docs/adr/0003-provisioning-trust-model.md`.

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

- fetch secret-bearing installation material where a driver truly requires it,
- report lifecycle event,
- upload installer log chunk,
- report validation result.

It must not authorize administrative APIs or access another installation.

Credential values must not appear in query strings, public boot scripts, kernel arguments or logs.

## Seed security

Seeds are installation-scoped. Fetching or rendering a seed is not equivalent to claiming or starting the installation.

The Debian 13 Standard driver uses initrd preseeding. Its rendered Preseed contains desired-state configuration and SSH public keys but no lifecycle credential, reusable password, private key or recovery secret. The assignment-gated iPXE script loads the verified Debian initrd and then injects the served `preseed.cfg` as `/preseed.cfg` into iPXE's magic initrd. Debian Installer consumes that file as native initrd preseed material.

The Preseed endpoint is therefore non-secret public boot material, not a secret-release channel. It is available only while the exact InstallationSpec remains armed for an operator-approved Machine. AegisPXE does not use Debian `preseed/url` for this path and does not maintain a custom CPIO/initrd repacker.

If a future driver requires secret-bearing seed delivery, access remains valid only for the period required by the native installer and is revoked according to explicit lifecycle policy, normally after successful completion or administrative cancellation/expiry. That path additionally requires cryptographic boot trust.

## Boot assignment

An assignment is consumed only after an authenticated `INSTALLER_STARTED` event for the assigned installation. Firmware fetches, bootloader retries, BootSpec rendering, artifact reads and seed reads do not consume it.

At most one assignment may be armed for one Machine. Cancelling an assignment is an auditable administrative action. Consuming an assignment is an auditable runtime state mutation tied to the accepted authenticated installer-start event.

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

### Bootstrap operator boundary

The first Debian vertical path uses a deliberately narrow bootstrap operator mechanism before richer user/RBAC support exists.

- AegisPXE generates a random 256-bit bootstrap operator key under `/var/lib/aegispxe/operator.key` by default.
- The key file is a regular file with no group/other access; symlinks and unsafe existing permissions are rejected.
- The key value is not logged and is exchanged for a short-lived server-side session rather than stored in browser storage.
- Session cookies are HttpOnly and SameSite=Strict. TLS sessions additionally use the Secure attribute.
- Every browser mutation requires a CSRF value bound to the server-side session.
- Login attempts are rate limited.
- Operator login and mutations are refused on cleartext non-loopback network HTTP.
- Proxy headers such as `X-Forwarded-Proto` are not trusted as transport proof in this initial contract.

The bootstrap operator may perform only the explicitly exposed provisioning mutations required by the current milestone. It does not satisfy cryptographic Machine/installer trust and does not authorize release of lifecycle credentials or recovery material merely by existing.

A future authenticated Studio with users, passkeys and RBAC may replace this bootstrap login while preserving the same audited domain mutation boundaries.

## Input validation

External identifiers, URLs, filenames, paths, hostnames, MAC addresses, driver IDs and profile values are validated at domain boundaries.

Path traversal and user-controlled filesystem destinations are forbidden. Internal paths should be constructed from typed IDs and fixed roots.

## Logging and redaction

Security decisions must be logged without logging secrets. See `OBSERVABILITY.md`.

Authentication failures, authorization failures, invalid lifecycle events, replay attempts, assignment conflicts and artifact integrity failures require structured security logs with stable error codes.

Bootstrap operator logs may contain request ID, remote address, actor after authentication, decision result and a non-secret cause class. They must never contain the bootstrap key, session cookie or CSRF value.

## Replay and sequencing

Installation events include a sequence or idempotency mechanism. The server must reject invalid state regressions and handle duplicate reports deterministically.

A replayed request must not create duplicate state transitions.

Cryptographic boot trust must include server freshness/challenge material or an equivalent replay-resistant mechanism. A previously valid proof may not be replayed as a new provisioning authorization.

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
