# Security Model

AegisPXE provisions privileged systems over a network. Security properties therefore belong to the architecture, not to optional hardening profiles.

## Current implementation status

The `0.1.0-dev.21` stabilization line intentionally separates **implemented security primitives** from **E2E-proven production paths**.

- Debian 13 provisioning uses the last real-VM-proven kernel + Debian initrd + Preseed boot contract.
- The TPM boot-trust, signed reporter telemetry, credential-release and reporter source code remain available for isolated tests and further design work.
- The failed reporter/initramfs injection experiments from dev.14 through dev.20 are not registered in the production boot handler and the reporter binary is not shipped in the production `.deb`.
- No runtime reporter path is considered production-ready until a redesigned delivery mechanism passes the real UEFI/vTPM E2E gate.
- Secure Boot is not yet an implemented or claimed property of the 0.1.0 Debian provisioning path.

AegisPXE must not advertise a trust, telemetry or Secure Boot property merely because its underlying primitives compile or pass unit tests.

## Trust boundaries

Primary boundaries:

- firmware/PXE client to AegisPXE boot service,
- installer/reporter to AegisPXE API,
- browser administrator to AegisPXE Studio,
- trusted reverse proxy/SSO to the Studio listener,
- AegisPXE to upstream artifact sources,
- AegisPXE to local persistent state and secret material.

A MAC address is an identifier, not an authentication factor. An SMBIOS UUID is likewise an identifier and must not be used as an authentication factor.

## Provisioning trust layers

Provisioning trust is layered and must not collapse identification, authorization and authentication into one flag.

1. **Discovery identity** resolves observations such as MAC/SMBIOS UUID to a Machine record. It provides no secret-bearing authority.
2. **Operator approval** is represented by explicit provisioning intent for that Machine. It authorizes scheduling but does not authenticate a later network client.
3. **Armed assignment** binds one Machine to one immutable InstallationSpec. At most one assignment may be armed for a Machine at a time.
4. **Cryptographic boot trust** is the separate proof required before secret release. Its TPM-bound primitives exist, but their Debian runtime delivery remains suspended until E2E-proven.

Operator approval plus an armed assignment may authorize delivery of non-secret public boot material. Secret-bearing operations must never be authorized solely from discovery identifiers.

See `docs/adr/0003-provisioning-trust-model.md` and `docs/adr/0007-tpm-bound-reporter-trust.md`.

## Human authentication and authorization

The trusted reverse proxy establishes only the outer source/identity boundary. A proxy identity by itself does not create an AegisPXE session and does not grant an AegisPXE role.

Normal Studio authentication requires:

1. a request from the explicitly configured trusted proxy boundary,
2. an AegisPXE user mapped to the asserted external subject,
3. a successful AegisPXE WebAuthn/Passkey assertion,
4. an active AegisPXE account and authorized role,
5. a valid server-side AegisPXE session for subsequent requests.

Emergency recovery requires the separately documented recovery factors. See ADR 0008.

The Studio listener exposes browser administration only under `/ui/` plus `/healthz`. The legacy core `/api/v1/machines` inventory is not exposed on the Studio listener because trusted source address alone is not an authenticated user session.

Every browser mutation requires a valid session and session-bound CSRF token. Destructive deletion additionally requires administrator role and exact-ID confirmation in Studio.

## Trusted reverse-proxy boundary

AegisPXE considers forwarded identity/protocol metadata only when the **direct TCP peer** belongs to configured `AEGISPXE_TRUSTED_PROXY_CIDRS`.

The configured protocol header must equal `https`. The configured identity header must be bounded and free of control characters before use.

The reverse proxy must overwrite or remove client-supplied copies of trusted headers before forwarding.

A trusted proxy source without an identity may reach the explicit recovery/health transport boundary, but source trust never creates an authenticated AegisPXE user session.

See `docs/adr/0004-studio-trusted-proxy.md` and `docs/adr/0008-layered-human-authentication.md`.

## Debian 13 boot and seed security

The production Debian 13 boot transport is intentionally simple and currently proven by the earlier real-VM installation path:

```text
verified Debian kernel
verified Debian initrd.gz
installation-scoped preseed.cfg injected as /preseed.cfg by iPXE
boot
```

The rendered Preseed contains desired-state configuration and SSH public keys but no lifecycle credential, reusable administrator password, private key or recovery key.

Reporter binaries, reporter configuration, custom newc overlays, multi-initrd EFI handoffs and server-side repacked initramfs images are not part of the stabilized production boot path in dev.21.

The Preseed is the final network object in the destructive public handoff. Immediately before returning the rendered Preseed, AegisPXE atomically consumes the armed assignment. This prevents automatic re-entry into the same destructive installation on the next PXE boot.

Consumption is scheduling state only. It does not mean `INSTALLER_STARTED`, successful installation, validation or cryptographic trust.

## Assignment safety

At most one assignment may be armed for one Machine. Arming, consuming and cancelling assignments are audited state transitions.

A consumed assignment does not grant future destructive boot authority. A new destructive attempt requires an explicit new assignment decision.

Deletion is deliberately guarded:

- an InstallationSpec with an `armed` assignment cannot be deleted until the assignment is cancelled,
- a Machine cannot be deleted while InstallationSpecs still reference it,
- deletions execute transactionally,
- correlated runtime state is deleted with its InstallationSpec,
- a system-level deletion audit event remains after the primary entity history is removed.

Machine nicknames are display metadata only. They never replace the immutable Machine ID or discovery identifiers in trust decisions.

## TPM boot-trust primitives

The dormant reporter design creates a deterministic RSA primary key in TPM 2.0 and sends only its public key to AegisPXE. Candidate keys are machine-bound and start as `pending`; an administrator must explicitly approve a candidate before it can authorize a challenge.

The design uses a short-lived challenge bound to installation ID, machine ID, key fingerprint and a random nonce. A verified proof may release a lifecycle credential only as RSA-OAEP-SHA256 ciphertext to the approved key.

This is TPM-bound explicit enrollment, not manufacturer-chain remote attestation. EK certificate-chain verification and measured-boot PCR quotes remain future hardening work.

Because the runtime delivery path is suspended, these primitives must not be described as active Debian runtime protection yet.

## Reporter telemetry primitives

The signed reporter API design does not send the raw lifecycle credential as a Bearer token over the cleartext PXE listener.

It derives a request MAC key from the lifecycle credential and authenticates the exact method, path, idempotency key, timestamp and body digest with HMAC-SHA256. The server enforces a bounded timestamp window before accepting a report.

The stored fixed-size verifier is itself authentication material and must receive the same database-access protection as other credential verifiers. Database compromise of verifier material is therefore security-significant.

The reporter telemetry API remains available for isolated integration testing, but without an E2E-proven reporter delivery path it does not yet make Debian installation lifecycle telemetry production-complete.

## Artifact integrity

Provisioning artifacts are identified by cryptographic digest and provenance metadata. A driver may not return an artifact as usable until integrity verification succeeds.

A hash mismatch is a hard failure. Existing bytes with the wrong digest must never be reused silently.

The Debian artifact resolver constrains expected origin/path/provenance and verifies downloaded artifact content against the pinned digest.

Upstream signature/checksum verification is used where the driver resolver defines it. Trust verification failures are fail-closed and use stable error codes.

## Secret handling

Secrets include:

- recovery keys,
- passwords,
- private keys,
- bearer tokens,
- HMAC authentication material,
- browser session credentials,
- lifecycle credentials,
- recovery tickets/material.

Secrets must not be placed in:

- normal logs,
- audit messages,
- URLs/query strings,
- public iPXE configuration,
- kernel arguments,
- machine nicknames/metadata,
- operator-visible error details.

The recovery key is stored in a narrowly permissioned regular file and verified constant-time. Browser sessions are server-side and use opaque random tokens.

## Administrative authorization

Security-sensitive operations require explicit permission checks, including:

- changing Machine provisioning policy,
- deleting Machines,
- creating/arming/cancelling/deleting installations,
- approving/revoking TPM boot-trust keys,
- approving/blocking operator users,
- recovery/bootstrap actions.

Machine nicknames may be changed by authenticated active operators because they are non-authoritative display metadata. Deletion remains admin-only.

The last non-blocked administrator cannot be blocked through the Store contract.

## Input validation

External identifiers, URLs, filenames, paths, hostnames, MAC addresses, driver IDs, profile values, nicknames, TPM public keys, signatures, timestamps, idempotency keys and telemetry bodies are validated at domain boundaries.

Machine nicknames are trimmed, bounded to 80 Unicode code points and reject control characters.

Path traversal and user-controlled filesystem destinations are forbidden. Internal paths should be constructed from typed IDs and fixed roots.

Request bodies are bounded before parsing on security-sensitive endpoints.

## Logging and redaction

Security decisions must be logged without logging secrets. See `OBSERVABILITY.md`.

Authentication failures, authorization failures, invalid lifecycle events, replay attempts, assignment conflicts, deletion conflicts, boot-trust failures and artifact-integrity failures require structured logs with stable error codes and correlation IDs.

The Studio live-log view reads only the already-redacted bounded in-memory log ring. The NDJSON export serves the same redacted stream with an explicit attachment filename and `nosniff` response policy.

Machine nickname logs record whether a nickname exists, not its content, unless the content is intentionally retained in a deletion audit record as non-secret operator metadata.

## Replay and sequencing

Installation events use installation-scoped idempotency keys. Lifecycle state must not regress or skip required stages.

Reporter telemetry authentication includes request freshness. Boot-trust challenges include random freshness material and short expiry.

Exact retry behavior must never create a new lifecycle transition or a second raw credential.

## Secure Boot

Secure Boot is a separate release gate. The current production package must not claim that the PXE chain, iPXE binary, Debian kernel or any future reporter delivery path is Secure Boot verified until the complete signed chain is implemented and tested on real UEFI Secure Boot firmware.

No code path may silently disable Secure Boot or downgrade to an unsigned boot path while reporting success.

## Secure defaults

Defaults favor:

- SSH keys over passwords,
- root login disabled,
- minimum necessary exposed listener surfaces,
- verified artifacts,
- explicit operator approval for destructive provisioning,
- explicit TPM key approval over first-contact auto-trust,
- local boot when there is no armed assignment,
- refusal rather than guessing when state is inconsistent,
- removal of unproven runtime components from production packaging.

## Security changes

Changes to trust boundaries, credential scope, package contents, boot transport, helper permissions, artifact trust, secret handling or authorization require documentation updates and an ADR when the architectural contract changes.

A security-relevant behavior change without focused tests and the appropriate real-VM gate is not mergeable.
