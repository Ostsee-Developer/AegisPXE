# ADR 0007: TPM-bound explicit reporter enrollment and secret-free telemetry transport

- Status: Accepted design; Debian runtime delivery suspended pending E2E-proven replacement
- Date: 2026-08-19

## Implementation note: dev.21 stabilization

The trust and signed-telemetry protocol in this ADR remains the intended security design. The reporter **delivery mechanism is not currently production-active**.

The dev.14 through dev.20 experiments that injected, overlaid, concatenated or repacked the reporter into the Debian Installer initramfs repeatedly failed on the real UEFI/vTPM VM before `INSTALLER_STARTED`. In dev.21 those boot-transport experiments are removed from the production handler and package, and Debian returns to the last real-VM-proven kernel + native initrd + Preseed contract.

Accordingly:

- boot-trust and signed telemetry primitives remain available for isolated tests,
- the production `.deb` does not ship the reporter binary,
- Debian Preseed does not invoke the reporter,
- no release may claim authenticated Debian runtime telemetry or first-boot reporter validation until a redesigned reporter delivery path passes the documented real UEFI/vTPM E2E gate.

The remainder of this ADR defines the protocol that a future proven delivery mechanism must use.

## Context

AegisPXE 0.1.0-dev.13 introduced authoritative installation lifecycle persistence and installation-correlated log ingestion, but intentionally did not place the lifecycle credential in public Preseed, URLs, kernel arguments or logs.

The Debian Installer path is delivered over the PXE-side HTTP listener. Sending a long-lived Bearer secret over that cleartext transport would expose the credential to passive network capture even if its initial release had been protected.

AegisPXE also needs an explicit machine trust boundary stronger than MAC address or SMBIOS UUID discovery identifiers. The design must work with TPM 2.0 machines without silently degrading to identifier-only authentication.

## Decision

AegisPXE uses a TPM-bound **explicit enrollment** protocol for a future Debian reporter runtime.

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

A consumed assignment may remain eligible for this exact installation's trust proof because ADR 0005 consumes the destructive boot lease at the final public handoff before a future installer reporter can report. A cancelled assignment is never eligible.

### Lifecycle credential release

After a valid fresh proof, AegisPXE creates one random installation-scoped lifecycle credential. Only its SHA-256 verifier is stored in the credential table.

The raw credential is encrypted with RSA-OAEP-SHA256 to the approved TPM key and returned as ciphertext. The reporter decrypts it through the TPM. AegisPXE never returns the plaintext credential from the boot-trust API.

The challenge stores the encrypted response so an exact proof retry can receive the same ciphertext without minting or revealing a second credential.

### Telemetry over the PXE listener

The reporter must not transmit the lifecycle secret as a Bearer token over the cleartext PXE HTTP listener.

Instead it derives the request-authentication key as:

```text
K = SHA256(lifecycle_secret)
```

This equals the verifier stored by the server and is therefore itself security-sensitive authentication material. Reporter event and log requests use HMAC-SHA256 over a canonical request containing:

- protocol version,
- HTTP method,
- exact path,
- idempotency key,
- Unix timestamp,
- SHA-256 of the exact request body.

The server accepts only timestamps inside a bounded freshness window. The raw lifecycle secret therefore never crosses the network after TPM decryption.

The older Bearer telemetry handlers remain an internal compatibility surface and are not exposed through the cleartext PXE listener.

### Reporter delivery requirements

A replacement Debian reporter delivery mechanism must satisfy all of the following before it can be enabled:

- preserve the last known-good Debian installer boot contract or prove a replacement on the real UEFI/vTPM fixture,
- keep reporter configuration non-secret,
- keep the lifecycle credential out of public boot material,
- provide deterministic failure diagnostics before the Debian UI starts,
- avoid firmware/iPXE behavior that is only unit-tested but not E2E-proven,
- support the first-boot TPM identity continuity required by this protocol.

### Intended lifecycle integration

Once a delivery mechanism passes E2E, native Debian boundaries may provide authoritative reports such as:

- installer start,
- disk preparation,
- base OS installation,
- profile/package application,
- hardening,
- first boot,
- validation,
- terminal success/failure.

These stages must be based on real native evidence and must never be inferred from unrelated HTTP reads.

## Consequences

- Public PXE material must never contain the lifecycle credential.
- Passive capture of signed reporter telemetry must not reveal the raw reusable lifecycle secret.
- MAC/SMBIOS identifiers cannot authorize telemetry or credential release.
- A newly observed TPM key requires explicit administrator approval.
- The stored lifecycle verifier is authentication material and a database compromise can therefore affect telemetry-authentication integrity.
- An attacker who convinces an administrator to approve the wrong candidate can establish trust in that key; operator verification is a real security action.
- Full EK certificate validation and PCR/measured-boot attestation remain future hardening work.
- Until reporter delivery passes E2E, the production Debian path remains intentionally without authenticated runtime reporter telemetry rather than pretending an unproven transport is safe.
