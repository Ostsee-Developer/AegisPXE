# ADR 0003: Provisioning trust model

- Status: Accepted
- Date: 2026-08-18

## Context

AegisPXE discovers machines before it can trust them. MAC addresses and SMBIOS UUIDs are useful stable identifiers, but they are observable/spoofable values and therefore cannot authenticate a client for secret-bearing provisioning operations.

The Debian 13 Standard path now has immutable InstallationSpecs, verified artifacts, typed BootSpecs and deterministic Preseed rendering. The next step must define which facts authorize scheduling, which facts authenticate a booting client, and when installation-scoped credentials may be released.

Debian supports initrd preseeding. iPXE can construct a magic initrd by loading the normal Debian initrd and injecting an additional downloaded file at a chosen path. AegisPXE can therefore serve the non-secret Preseed as assignment-gated public boot material and have iPXE inject it as `/preseed.cfg`, which Debian Installer loads as initrd preseeding. This avoids a Debian `preseed/url` credential channel and avoids maintaining a custom CPIO/initrd repacker.

## Decision

AegisPXE separates administrative approval from cryptographic machine trust.

### 1. Discovery identity

Discovery observations such as MAC and SMBIOS UUID identify a machine record. They never authenticate a client.

### 2. Operator approval

An operator may approve a discovered machine for provisioning. Approval means only that AegisPXE administrators intend to provision that machine record. It does not prove that a later network client presenting the same identifiers is the same physical or virtual machine.

### 3. Cryptographic boot trust

Secret-bearing installer operations require cryptographic proof bound to the machine or to an explicitly enrolled provisioning credential. The concrete first implementation is a later vertical slice and must satisfy all of the following:

- challenge-response or equivalent proof of possession,
- server-generated freshness/nonces to prevent replay,
- machine and installation scope,
- no credential values in URLs, query strings, normal logs or machine metadata,
- revocation and expiry,
- structured security logs with stable error codes,
- deterministic rejection when trust is absent, expired, replayed or mismatched.

TPM-backed attestation is the preferred first hardware-backed mechanism for capable systems. A non-TPM fallback, if later supported, requires its own explicit security decision and must not downgrade silently.

### 4. Assignment

A provisioning assignment binds exactly one Machine to exactly one immutable InstallationSpec. At most one assignment may be armed for a machine at a time.

Assignment states are:

- `armed`: selected for the next trusted provisioning attempt,
- `consumed`: the server accepted an authenticated `INSTALLER_STARTED` event for that installation,
- `cancelled`: administratively revoked before consumption.

Firmware retries, iPXE check-ins, BootSpec rendering, artifact reads and Preseed reads never consume an assignment.

### 5. Secret release

Operator approval and an armed assignment are necessary but not sufficient to release lifecycle credentials or other installation secrets. Secret-bearing operations additionally require cryptographic boot trust.

### 6. Debian Preseed transport

For Debian 13 Standard, AegisPXE uses initrd preseeding without a custom initrd build step. The assignment-gated boot script loads the verified Debian `initrd.gz`, then instructs iPXE to fetch the rendered non-secret `preseed.cfg` and inject it as `/preseed.cfg` into the magic initrd.

The Preseed endpoint is classified as public boot material rather than secret release. It is reachable only while the exact InstallationSpec is armed for an operator-approved Machine. The Preseed contains no lifecycle credential, reusable password, private key or recovery secret.

The lifecycle credential remains behind the cryptographic boot-trust boundary and is not embedded into public boot scripts, kernel arguments, InstallationSpec metadata or Preseed content.

### 7. Studio boundary

Until operator authentication and authorization exists, Studio may display trust, InstallationSpec and assignment state but must not expose approve, arm or cancel mutations.

## Consequences

- A spoofed MAC or SMBIOS UUID can never obtain secret-bearing installer authority by itself.
- AegisPXE can expose richer read-only provisioning state in Studio without weakening authorization.
- Debian can perform native initrd preseeding without a credential-bearing `preseed/url` and without AegisPXE owning archive-repacking code.
- Public boot reads remain retryable and non-consuming; only authenticated installer telemetry may advance runtime lifecycle state.
- The first real provisioning E2E must not be called security-complete until cryptographic boot trust and authenticated installer telemetry are proven.
