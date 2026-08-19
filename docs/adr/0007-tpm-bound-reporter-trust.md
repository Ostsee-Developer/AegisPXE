# ADR 0007: TPM-bound explicit reporter enrollment and secret-free telemetry transport

- Status: Accepted
- Date: 2026-08-19

## Context

AegisPXE 0.1.0-dev.13 introduced authoritative installation lifecycle persistence and installation-correlated log ingestion, but intentionally did not place the lifecycle credential in public Preseed, URLs, kernel arguments or logs.

The Debian Installer path is delivered over the PXE-side HTTP listener. Sending a long-lived Bearer secret over that cleartext transport would expose the credential to passive network capture even if its initial release had been protected.

AegisPXE also needs an explicit machine trust boundary stronger than MAC address or SMBIOS UUID discovery identifiers. The first implementation must work with TPM 2.0 machines without silently degrading to identifier-only authentication.

## Decision

AegisPXE implements a TPM-bound **explicit enrollment** mode for the Debian 13 reporter.

### Machine key

The reporter creates a deterministic RSA primary key inside TPM 2.0. The private key is not exportable from the TPM. The reporter sends only its PKIX public key to AegisPXE.

The server fingerprints the public key with SHA-256 and binds the candidate key to the Machine. A candidate starts as `pending` and does not authorize secret release.

An administrator must explicitly approve the candidate in Studio. At most one boot-trust key may be approved for a Machine at a time. Revocation is explicit and auditable.

This version is **not manufacturer-chain remote attestation**. The server does not yet validate an EK certificate chain or measured-boot PCR quote. The security property is explicit operator enrollment plus subsequent proof of possession of the enrolled TPM-resident key. Full EK/AK attestation can strengthen this boundary in a later ADR without changing lifecycle credential scope.

### Fresh proof

For one exact InstallationSpec the server creates a short-lived random challenge bound to:

- challenge ID,
- installation ID,
- machine ID,
- approved key fingerprint,
- 256-bit nonce.

The reporter signs the canonical challenge with the TPM key. The server verifies the signature against the approved public key. Expired, mismatched and revoked bindings are rejected.

A consumed assignment remains eligible for this exact installation's trust proof because ADR 0005 consumes the destructive boot lease at Preseed handoff before the Debian Installer can report. A cancelled assignment is never eligible.

### Lifecycle credential release

After a valid fresh proof, AegisPXE creates one random installation-scoped lifecycle credential. Only its SHA-256 verifier is stored in the credential table.

The raw credential is encrypted with RSA-OAEP-SHA256 to the approved TPM key and returned as ciphertext. The reporter decrypts it inside the TPM. AegisPXE never returns the plaintext credential from the boot-trust API.

The challenge stores the encrypted response so an exact proof retry can receive the same ciphertext without minting or revealing a second credential.

### Telemetry over the PXE listener

The reporter does not transmit the lifecycle secret as a Bearer token over the cleartext PXE HTTP listener.

Instead it derives the request-authentication key as:

```text
K = SHA256(lifecycle_secret)
```

This equals the verifier already stored by the server. Reporter event and log requests use HMAC-SHA256 over a canonical request containing:

- protocol version,
- HTTP method,
- exact path,
- idempotency key,
- Unix timestamp,
- SHA-256 of the exact request body.

The server accepts only timestamps inside a bounded freshness window. The raw lifecycle secret therefore never crosses the network after TPM decryption.

The older Bearer telemetry handlers remain an internal compatibility surface but are not exposed through the cleartext PXE listener.

### Debian Installer reporter

The Debian 13 boot script injects a statically linked amd64 reporter binary and non-secret installation configuration into the initrd before the final Preseed object.

The Preseed hooks provide authoritative stage boundaries where Debian exposes them directly:

- `preseed/early_command` starts the reporter and queues `INSTALLER_STARTED`,
- `partman/early_command` queues `DISK_PREPARATION`,
- native Debian Installer syslog evidence identifies `OS_INSTALLING` and `PROFILE_APPLYING`,
- `preseed/late_command` queues `HARDENING` and installs the first-boot finalizer.

The reporter uploads bounded native installer log chunks correlated to the InstallationSpec.

### First boot

The raw lifecycle credential is held only in reporter process memory. For reboot handoff the installer persists only the RSA-OAEP ciphertext.

At first boot the same TPM-derived key decrypts the ciphertext. The finalizer reports `FIRST_BOOT`, performs validation, reports `VALIDATING`, and terminates with `SUCCESS` or `FAILED`.

`SUCCESS` and `FAILED` revoke the lifecycle credential server-side. After terminal reporting the finalizer removes the persisted ciphertext.

## Consequences

- Public PXE material still contains no lifecycle credential.
- Passive capture of reporter telemetry does not reveal a reusable lifecycle secret.
- MAC/SMBIOS identifiers cannot authorize telemetry or credential release.
- A newly observed TPM key requires explicit administrator approval.
- An attacker who convinces an administrator to approve the wrong candidate can still establish trust; operator verification is therefore a meaningful security action, not cosmetic UI.
- Full EK certificate validation and PCR/measured-boot attestation remain future hardening work.
- The Debian installer may intentionally wait at late command for operator approval rather than silently finish without an authenticated finalizer.
