# Installation Lifecycle

AegisPXE installation status is event-driven. The current status is a projection of accepted append-only lifecycle events.

## Rule zero

**AegisPXE must never infer installation progress from unrelated activity.**

Examples of invalid inference:

- seed fetched -> installer started,
- kernel downloaded -> disk preparation,
- HTTP traffic -> profile applying,
- elapsed time -> OS installing.

Only an event from the source authorized for that installation stage may advance lifecycle state.

## Canonical lifecycle

```text
CREATED
  -> QUEUED
  -> PXE_BOOTED
  -> INSTALLER_STARTED
  -> DISK_PREPARATION
  -> OS_INSTALLING
  -> PROFILE_APPLYING
  -> HARDENING
  -> FIRST_BOOT       (when required)
  -> VALIDATING
  -> SUCCESS
```

`HARDENING` may transition directly to `VALIDATING` for drivers that do not require a first-boot finalizer.

A runtime failure terminates the lifecycle as `FAILED`. Terminal state never rewrites or removes earlier accepted events.

## Assignment and lifecycle are different state machines

The immutable InstallationSpec exists before runtime lifecycle progress begins. An operator may then arm an Assignment that binds exactly one Machine to that InstallationSpec.

Assignment state controls whether a destructive boot payload may be handed out:

```text
unassigned
  -> armed
       -> consumed
       -> cancelled
```

Lifecycle state records what authoritative components report happened during that installation.

Arming creates `QUEUED`. The server records `PXE_BOOTED` when the armed machine reaches the AegisPXE provisioning chain.

Per ADR 0005, an armed Assignment is consumed at the final public Preseed handoff so the same destructive installer is not selected again on the next PXE boot. `CONSUMED` is scheduling state only. It does not mean `INSTALLER_STARTED`, `SUCCESS`, validation, or cryptographic trust.

A consumed assignment may remain eligible for an installation-scoped trust protocol when a production reporter exists for that driver. A cancelled assignment never authorizes trust completion or credential release.

## Event shape

Every lifecycle event contains at least:

- installation ID,
- monotonically increasing accepted sequence,
- stage,
- explicit source identity,
- server receive timestamp,
- optional client timestamp,
- request ID,
- idempotency key,
- stable error code for `FAILED`,
- bounded human-readable message,
- bounded non-secret structured metadata.

## Authoritative sources

The lifecycle authority mapping is:

- `CREATED`: server,
- `QUEUED`: server,
- `PXE_BOOTED`: server boot path,
- `INSTALLER_STARTED`: authenticated installer reporter,
- `DISK_PREPARATION`: authenticated installer reporter,
- `OS_INSTALLING`: authenticated installer reporter from native installer evidence,
- `PROFILE_APPLYING`: authenticated installer reporter from native installer evidence,
- `HARDENING`: authenticated installer late hook,
- `FIRST_BOOT`: authenticated installed-OS finalizer,
- `VALIDATING`: authenticated validator/finalizer,
- `SUCCESS`: authenticated validator/finalizer,
- `FAILED`: authenticated installer, finalizer, or validator while runtime is active.

The state machine rejects events from sources not authorized for that stage.

## Current Debian 13 production status

The Debian reporter runtime is currently suspended from the production package after the earlier initramfs-injection design failed the real UEFI/vTPM E2E path. The production Debian 13 transport intentionally uses the native Debian kernel, native `initrd.gz`, verified Debian shim and final Preseed handoff without a reporter overlay.

Therefore the current packaged Debian driver can authoritatively advance the server-side lifecycle through `PXE_BOOTED`, but it does **not** manufacture `INSTALLER_STARTED`, later installer stages, `SUCCESS` or `FAILED` from HTTP activity, elapsed time, local-boot observations or the existence of an installer log file.

The Debian Preseed late hook still writes bounded local validation markers to `/var/log/aegispxe-installer.log` on the installed host. Those markers are valuable E2E evidence, but until an authenticated production telemetry path exists they are not server-side lifecycle events.

A successful local reboot with Secure Boot enabled and working SSH access is likewise E2E validation evidence, not an authenticated `SUCCESS` lifecycle report.

## Suspended TPM-bound reporter protocol

The following protocol remains the intended trust model for a future production reporter and is covered only by isolated protocol/runtime tests while reporter delivery is suspended:

1. The reporter creates a deterministic RSA key inside TPM 2.0 and sends only its public key for enrollment.
2. A newly discovered key is `pending` and must be explicitly approved by an administrator in Studio.
3. The server issues a short-lived random challenge bound to the exact InstallationSpec, Machine and approved key fingerprint.
4. The reporter signs the canonical challenge with the TPM-resident private key.
5. After successful verification, the server creates one installation-scoped lifecycle credential and returns it only as RSA-OAEP-SHA256 ciphertext to the approved TPM key.
6. The reporter decrypts the credential through the TPM. Plaintext credential material is held in reporter process memory only.

This is explicit TPM-bound enrollment, not manufacturer-chain remote attestation. EK certificate-chain validation and measured-boot PCR quotes are future hardening work. See ADR 0007.

## Reporter request authentication

When a production reporter is reintroduced, the cleartext PXE listener must not expose a reusable plaintext lifecycle credential.

The suspended protocol derives a request MAC key as `SHA256(lifecycle_secret)` and signs every event/log request with HMAC-SHA256 over:

- protocol version,
- exact HTTP method,
- exact request path,
- idempotency key,
- Unix timestamp,
- SHA-256 of the exact JSON body.

The server accepts only a bounded clock-skew window and validates the MAC before parsing or accepting the report. The raw lifecycle credential is therefore not transmitted after TPM decryption.

The server stores the fixed-size lifecycle verifier, credential expiry/revocation state and last-use timestamp. Terminal lifecycle state revokes the credential.

These primitives do not make the current Debian package reporter-capable by themselves.

## Debian 13 reporter boundaries

The previous design injected a reporter binary and non-secret reporter configuration into the Debian installer initrd and mapped native installer evidence onto authenticated lifecycle events. That delivery design is obsolete for the current production Debian driver and must not be reintroduced as an RC shortcut.

A replacement reporter must preserve the proven Secure Boot installer path:

```text
verified Debian kernel
-> native Debian initrd.gz
-> verified Debian bootnetx64.efi via iPXE shim
-> final Preseed handoff
-> Debian Installer
```

It must define a delivery mechanism that does not repack the native initrd, pass a separate real UEFI/vTPM E2E gate, and only then may it emit authoritative `INSTALLER_STARTED` through terminal lifecycle events.

## First boot

The first-boot finalizer protocol remains part of the lifecycle design but is not active in the current production Debian package while reporter delivery is suspended.

When re-enabled, first boot must report:

```text
FIRST_BOOT
  -> VALIDATING
       -> SUCCESS
       -> FAILED
```

Required validation is derived from the InstallationSpec and may include the configured administrator account, SSH authorized keys and permissions, SSH configuration, sudo policy and other requested state.

If a lifecycle credential must cross reboot, plaintext credential material must not be written to disk.

## Idempotency and ordering

Lifecycle state does not regress. Every authenticated client report carries an installation-scoped idempotency key.

- Replaying the same key with identical content returns the already accepted event.
- Reusing the same key with different content is rejected as a conflict.
- Skipping a required stage is rejected.
- A terminal state cannot transition again.

This makes network retries safe without silently rewriting history.

## Installer log stream

The production telemetry contract requires each accepted log chunk to have:

- installation-local contiguous sequence number,
- idempotency key,
- source,
- server timestamp,
- optional client timestamp,
- bounded content,
- server-computed digest after redaction.

Sequence gaps are rejected. Known sensitive line patterns are redacted before durable storage. The initial limits are 128 KiB per chunk and 16 MiB per installation.

The current Debian package does not ingest installer logs through this authenticated channel because the reporter runtime is suspended. Local `/var/log/aegispxe-installer.log` remains E2E/debug evidence on the installed host only.

## Machine boot after handoff or terminal state

Once no armed Assignment remains, provisioning policy resolves to local boot. AegisPXE actively attempts the installed local bootloader instead of relying solely on firmware to continue after an iPXE exit.

For x86_64 UEFI the local boot chain checks Debian vendor loaders, the standard `\\EFI\\BOOT\\BOOTX64.EFI` fallback path, and then generic local disk boot. Legacy BIOS uses the local disk boot path.

## Timeouts

Timeouts are explicit policy, not inferred progress. A telemetry-capable driver may produce a defined timeout error/event with enough log context to diagnose what authoritative report was missing.

Examples once authenticated runtime telemetry is active:

- installer never reported `INSTALLER_STARTED`,
- no telemetry/log activity after `OS_INSTALLING`,
- first boot did not report within the configured deadline.

A driver without active authenticated telemetry must not fabricate those stages or timeout transitions from elapsed time alone.

## UI projection

Studio displays accepted lifecycle events in sequence order and installation-scoped logs that actually exist. Assignment state and trust gates remain visible but visually distinct from authoritative lifecycle state.

For the current Debian production driver, later reporter-owned lifecycle stages remain absent rather than being inferred from a successful Preseed handoff or local reboot.

Studio may show elapsed time. It must never manufacture a completed stage.

See ADR 0005 for one-shot public boot handoff semantics, ADR 0006 for authenticated telemetry persistence, and ADR 0007 for the suspended TPM-bound reporter trust protocol.
