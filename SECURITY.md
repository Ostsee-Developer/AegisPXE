# Security Model

AegisPXE provisions privileged systems over a network. Security properties therefore belong to the architecture, not to optional hardening profiles.

## Current implementation status

The `0.1.0-dev.22` line builds Secure Boot as a separately gated security property on top of the dev.21 E2E-proven Debian installer transport.

- Debian 13 provisioning keeps the real-VM-proven kernel + native Debian initrd + Preseed transport.
- New Debian driver-v2 InstallationSpecs additionally pin Debian `bootnetx64.efi` through the same signed Debian release/checksum chain as kernel and initrd.
- The package builds from the official iPXE v2.0.0 Secure Boot bundle, pins the release tag to its exact upstream commit, validates the GitHub release-asset SHA-256/size, requires PE signature tables and records per-file hashes in a package manifest.
- With `AEGISPXE_SECURE_BOOT_POLICY=required`, the server fails startup when package-owned signed iPXE assets do not match that manifest and refuses destructive provisioning unless the Machine is observed with UEFI Secure Boot enabled and SetupMode disabled.
- The actual firmware signature checks plus the signed iPXE and Debian shim/kernel chain are the executable trust boundary. The iPXE `SecureBoot`/`SetupMode` values are policy observations, not cryptographic remote attestation.
- Secure Boot remains a release/E2E gate until the real OVMF fixture passes both positive and negative tests. Source tests alone do not authorize a Secure Boot claim.
- TPM boot-trust, signed reporter telemetry, credential-release and reporter source remain available for isolated testing, but reporter delivery is still outside the production Debian installer path.

AegisPXE must not advertise a trust, telemetry or Secure Boot property merely because its underlying primitives compile or pass unit tests.

## Trust boundaries

Primary boundaries:

- UEFI firmware to the signed PXE first stage,
- PXE client to AegisPXE boot service,
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
4. **Secure Boot enforcement** may independently gate destructive boot material. Under required policy, firmware must be UEFI and the observed Secure Boot state must be enabled; the InstallationSpec must use the Secure-Boot-capable driver contract.
5. **Cryptographic boot trust** is the separate proof required before future secret release. Its TPM-bound primitives exist, but their Debian runtime delivery remains suspended until E2E-proven.

Operator approval plus an armed assignment may authorize delivery of non-secret public boot material only after every configured platform gate also passes. Secret-bearing operations must never be authorized solely from discovery identifiers or a Secure Boot observation.

See `docs/adr/0003-provisioning-trust-model.md`, `docs/adr/0007-tpm-bound-reporter-trust.md` and `docs/adr/0009-secure-boot-chain.md`.

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

The Debian 13 installer transport remains intentionally simple:

```text
verified Debian kernel
verified native Debian initrd.gz
installation-scoped preseed.cfg injected as /preseed.cfg by iPXE
Debian bootnetx64.efi configured as the Secure Boot shim verifier
boot
```

The shim is an additional signed artifact and verifier configuration. AegisPXE does not repack, append to or inject runtime binaries into the Debian initrd.

The rendered Preseed contains desired-state configuration and SSH public keys but no lifecycle credential, reusable administrator password, private key or recovery key.

Reporter binaries, reporter configuration, custom newc overlays, multi-initrd EFI handoffs and server-side repacked initramfs images are not part of the stabilized production boot path.

The Preseed is the final network object in the destructive public handoff. Immediately before returning the rendered Preseed, AegisPXE atomically consumes the armed assignment. This prevents automatic re-entry into the same destructive installation on the next PXE boot.

Consumption is scheduling state only. It does not mean `INSTALLER_STARTED`, successful installation, validation or cryptographic trust.

## Secure Boot chain

The intended x86-64 UEFI chain is:

```text
UEFI Secure Boot firmware
  -> official iPXE v2.0.0 ipxe-shim.efi
  -> official signed iPXE v2.0.0 ipxe.efi
  -> AegisPXE discovery/policy decision
  -> verified Debian kernel + native initrd + Preseed
  -> Debian bootnetx64.efi configured through iPXE shim command
  -> Debian Installer
  -> installed Debian shim/grub/kernel on local reboot
```

The package build pins official iPXE release `v2.0.0` to upstream commit `12798ec29aa8a64d8675c4378b99f5fe28447afb`. The build rejects unexpected release metadata, missing GitHub SHA-256 metadata, size/digest mismatch, unsafe archive members or missing PE signature tables. Only the two expected x86-64 Secure Boot EFI files are extracted.

A package manifest records the release, upstream commit, release-asset SHA-256 and the exact SHA-256/size of `ipxe-shim.efi` and `ipxe.efi`. Runtime startup independently verifies those package-owned files as regular non-symlink files against the manifest. Under `required` policy, validation failure is fatal and logs `SEC025_SECURE_BOOT_ASSETS_INVALID`.

The package materializes the signed chain into the configured TFTP root as `ipxe-shim.efi` and `ipxe.efi`. UEFI Secure Boot DHCP configuration must hand the client `ipxe-shim.efi`; serving the second-stage `ipxe.efi` directly is not the supported Secure Boot entry path.

Machine discovery records `efi/SecureBoot` and `efi/SetupMode` as one of `enabled`, `disabled`, `setup_mode`, `unknown` or `unsupported`. Malformed evidence is rejected with `SEC024_SECURE_BOOT_EVIDENCE_INVALID`. Required-policy provisioning with anything other than `enabled` is denied with `SEC023_SECURE_BOOT_REQUIRED`.

Those variables are not remote attestation. A hostile client able to execute outside the intended signed chain could lie about ordinary discovery fields. The supported security claim therefore depends on actual UEFI signature enforcement and the signed first-stage chain, not on trusting the query value in isolation. TPM/PCR attestation remains separate future work.

No code path may silently disable Secure Boot or downgrade to an unsigned boot path while reporting success.

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

The Debian artifact resolver constrains expected origin/path/provenance and verifies downloaded artifact content against Debian signed `InRelease` metadata and the pinned installer checksum manifest. Driver v2 requires kernel, initrd and Debian `bootnetx64.efi` to share one verified installer version and provenance.

Upstream signature/checksum verification failures are fail-closed and use stable error codes.

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

UEFI Secure Boot state values are normalized from one-byte firmware settings and malformed values fail closed instead of being coerced to disabled/enabled.

Machine nicknames are trimmed, bounded to 80 Unicode code points and reject control characters.

Path traversal and user-controlled filesystem destinations are forbidden. Internal paths should be constructed from typed IDs and fixed roots.

Request bodies are bounded before parsing on security-sensitive endpoints.

## Logging and redaction

Security decisions must be logged without logging secrets. See `OBSERVABILITY.md`.

Authentication failures, authorization failures, invalid lifecycle events, replay attempts, assignment conflicts, deletion conflicts, boot-trust failures, artifact-integrity failures and Secure Boot decisions require structured logs with stable error codes and correlation IDs.

Secure Boot startup logs include the active policy, pinned upstream release/commit and cryptographic hashes of package-owned signed assets. Machine discovery and provisioning decisions include the normalized Secure Boot state and policy. Failure logs record stable SEC023-025 codes without embedding binary material.

The Studio live-log view reads only the already-redacted bounded in-memory log ring. The NDJSON export serves the same redacted stream with an explicit attachment filename and `nosniff` response policy.

Machine nickname logs record whether a nickname exists, not its content, unless the content is intentionally retained in a deletion audit record as non-secret operator metadata.

## Replay and sequencing

Installation events use installation-scoped idempotency keys. Lifecycle state must not regress or skip required stages.

Reporter telemetry authentication includes request freshness. Boot-trust challenges include random freshness material and short expiry.

Exact retry behavior must never create a new lifecycle transition or a second raw credential.

## Secure defaults

Defaults favor:

- UEFI Secure Boot required for destructive provisioning,
- SSH keys over passwords,
- root login disabled,
- minimum necessary exposed listener surfaces,
- verified artifacts,
- explicit operator approval for destructive provisioning,
- explicit TPM key approval over first-contact auto-trust,
- local boot when there is no armed assignment or platform security gate fails,
- refusal rather than guessing when state is inconsistent,
- removal of unproven runtime components from production packaging.

## Security changes

Changes to trust boundaries, credential scope, package contents, boot transport, helper permissions, artifact trust, secret handling or authorization require documentation updates and an ADR when the architectural contract changes.

A security-relevant behavior change without focused tests and the appropriate real-VM gate is not mergeable.
