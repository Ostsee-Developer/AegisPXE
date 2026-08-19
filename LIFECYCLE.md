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

A consumed assignment remains eligible for boot-trust completion for that exact InstallationSpec because the reporter cannot prove possession until after the initrd and final Preseed handoff have already occurred. A cancelled assignment never authorizes trust completion or credential release.

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

The initial authority mapping is:

- `CREATED`: server,
- `QUEUED`: server,
- `PXE_BOOTED`: server boot path,
- `INSTALLER_STARTED`: authenticated installer reporter,
- `DISK_PREPARATION`: authenticated installer reporter,
- `OS_INSTALLING`: authenticated installer reporter from native Debian Installer evidence,
- `PROFILE_APPLYING`: authenticated installer reporter from native Debian Installer evidence,
- `HARDENING`: authenticated installer late hook,
- `FIRST_BOOT`: authenticated installed-OS finalizer,
- `VALIDATING`: authenticated validator/finalizer,
- `SUCCESS`: authenticated validator/finalizer,
- `FAILED`: authenticated installer, finalizer, or validator while runtime is active.

The state machine rejects events from sources not authorized for that stage.

## TPM-bound reporter trust

The Debian 13 reporter establishes trust before authenticated runtime telemetry is accepted.

1. The reporter creates a deterministic RSA key inside TPM 2.0 and sends only its public key for enrollment.
2. A newly discovered key is `pending` and must be explicitly approved by an administrator in Studio.
3. The server issues a short-lived random challenge bound to the exact InstallationSpec, Machine and approved key fingerprint.
4. The reporter signs the canonical challenge with the TPM-resident private key.
5. After successful verification, the server creates one installation-scoped lifecycle credential and returns it only as RSA-OAEP-SHA256 ciphertext to the approved TPM key.
6. The reporter decrypts the credential through the TPM. Plaintext credential material is held in reporter process memory only.

This is explicit TPM-bound enrollment, not manufacturer-chain remote attestation. EK certificate-chain validation and measured-boot PCR quotes are future hardening work. See ADR 0007.

## Reporter request authentication

The cleartext PXE listener does not expose the legacy Bearer telemetry routes.

The reporter derives a request MAC key as `SHA256(lifecycle_secret)` and signs every event/log request with HMAC-SHA256 over:

- protocol version,
- exact HTTP method,
- exact request path,
- idempotency key,
- Unix timestamp,
- SHA-256 of the exact JSON body.

The server accepts only a bounded clock-skew window and validates the MAC before parsing or accepting the report. The raw lifecycle credential is therefore not transmitted after TPM decryption.

The server stores the fixed-size lifecycle verifier, credential expiry/revocation state and last-use timestamp. Terminal lifecycle state revokes the credential.

## Debian 13 reporter boundaries

The Debian 13 driver injects the reporter binary and non-secret reporter configuration into the installer initrd before the final Preseed object.

Authoritative hooks are:

- `preseed/early_command`: reporter daemon start and `INSTALLER_STARTED`,
- `partman/early_command`: `DISK_PREPARATION`,
- Debian Installer native syslog `bootstrap-base` selection: `OS_INSTALLING`,
- Debian Installer native syslog `pkgsel` selection: `PROFILE_APPLYING`,
- `preseed/late_command`: `HARDENING` plus first-boot finalizer installation.

Known native installer failure evidence emits `FAILED` with stable error codes. Installer syslog is uploaded in bounded, contiguous installation-scoped chunks and passes through server-side sensitive-line redaction before durable persistence.

The late hook intentionally waits for TPM trust approval and encrypted credential handoff rather than silently completing an installation that cannot authenticate its first-boot finalizer.

## First boot

Before reboot, the installer copies only the encrypted lifecycle credential ciphertext into the installed system. The plaintext lifecycle credential is not written to disk.

On first boot the same TPM-derived key decrypts the ciphertext and the finalizer reports:

```text
FIRST_BOOT
  -> VALIDATING
       -> SUCCESS
       -> FAILED
```

Initial validation checks include the configured administrator account, SSH authorized keys and permissions, the AegisPXE SSH hardening fragment, `sshd -t`, and the AegisPXE sudo policy. Validation output is uploaded as an installation-correlated log chunk.

After a terminal report the server revokes the lifecycle credential and the finalizer removes the persisted ciphertext.

## Idempotency and ordering

Lifecycle state does not regress. Every client report carries an installation-scoped idempotency key.

- Replaying the same key with identical content returns the already accepted event.
- Reusing the same key with different content is rejected as a conflict.
- Skipping a required stage is rejected.
- A terminal state cannot transition again.

This makes network retries safe without silently rewriting history.

## Installer log stream

Each accepted log chunk has:

- installation-local contiguous sequence number,
- idempotency key,
- source,
- server timestamp,
- optional client timestamp,
- bounded content,
- server-computed digest after redaction.

Sequence gaps are rejected. Known sensitive line patterns are redacted before durable storage. The initial limits are 128 KiB per chunk and 16 MiB per installation.

## Machine boot after handoff or terminal state

Once no armed Assignment remains, provisioning policy resolves to local boot. AegisPXE actively attempts the installed local bootloader instead of relying solely on firmware to continue after an iPXE exit.

For x86_64 UEFI the local boot chain checks Debian vendor loaders, the standard `\\EFI\\BOOT\\BOOTX64.EFI` fallback path, and then generic local disk boot. Legacy BIOS uses the local disk boot path.

## Timeouts

Timeouts are explicit policy, not inferred progress. A timeout produces a defined error code/event and enough log context to diagnose what report was missing.

Examples:

- installer never reported `INSTALLER_STARTED`,
- no telemetry/log activity after `OS_INSTALLING`,
- first boot did not report within the configured deadline.

## UI projection

Studio displays accepted lifecycle events in sequence order and installer logs scoped to the relevant installation. Assignment state and trust gates remain visible but visually distinct from authoritative lifecycle state.

Studio may show elapsed time. It must never manufacture a completed stage.

See ADR 0005 for one-shot public boot handoff semantics, ADR 0006 for authenticated telemetry persistence, and ADR 0007 for TPM-bound reporter trust and secret-free telemetry transport.
