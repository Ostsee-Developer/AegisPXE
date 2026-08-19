# Security Model

AegisPXE provisions privileged systems over a network. Security properties therefore belong to the architecture, not to optional hardening profiles.

## Trust boundaries

Primary boundaries:

- firmware/PXE client to AegisPXE boot service,
- installer/reporter to AegisPXE API,
- browser/CLI administrator to AegisPXE server,
- trusted reverse proxy/SSO to the AegisPXE Studio listener,
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
4. **Cryptographic boot trust** proves possession of an explicitly enrolled machine-bound key with freshness/replay protection before secret release.

Operator approval plus an armed assignment may authorize delivery of non-secret public boot material. Lifecycle credential release and authenticated runtime telemetry additionally require cryptographic boot trust.

The Debian 13 reporter implements the first cryptographic boot-trust mode as explicit enrollment of a TPM 2.0-resident RSA key followed by short-lived signed challenges. A newly observed key is not trusted automatically and requires explicit administrator approval. See ADR 0007.

This mode is TPM-bound explicit enrollment, not manufacturer-chain remote attestation. EK certificate-chain verification and measured-boot PCR quotes remain future hardening work. A non-TPM fallback must be a separately reviewed explicit security mode and must never be selected silently.

See `docs/adr/0003-provisioning-trust-model.md` and `docs/adr/0007-tpm-bound-reporter-trust.md`.

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

Each installation has a cryptographically random lifecycle credential scoped to exactly one immutable InstallationSpec. It may authorize only explicitly defined installer operations, such as:

- report lifecycle event,
- upload installer log chunk,
- report validation result,
- fetch secret-bearing installation material if a future driver truly requires it.

It must not authorize administrative APIs or access another installation.

The Debian reporter receives the credential only after successful TPM-bound trust proof. AegisPXE returns the raw credential only as RSA-OAEP-SHA256 ciphertext encrypted to the administrator-approved machine key. The reporter decrypts it through TPM 2.0 and keeps plaintext credential material in process memory only.

The installed first-boot finalizer receives only the encrypted ciphertext. It re-derives the same TPM primary key after reboot and decrypts the ciphertext through the TPM. Plaintext lifecycle credential material is not deliberately written to disk.

Credential values must not appear in query strings, public boot scripts, kernel arguments, Preseed content, ordinary logs or audit records.

### Reporter request authentication

The PXE listener may be cleartext HTTP, therefore the reporter does not send the raw lifecycle credential as a Bearer token on that surface.

Reporter requests derive a MAC key as `SHA256(lifecycle_secret)` and authenticate the exact request using HMAC-SHA256 over protocol version, HTTP method, exact path, idempotency key, timestamp and body digest. The server enforces a bounded freshness window before accepting the request.

The cleartext PXE listener exposes only these signed reporter telemetry routes. The older Bearer telemetry handlers remain an internal compatibility surface and are not allowlisted on the PXE listener.

The fixed-size lifecycle verifier stored by the server is therefore security-sensitive authentication material even though it is not the raw credential. It must receive the same database access protection as other credential verifiers.

## Seed security

Seeds are installation-scoped. Fetching or rendering a seed is not equivalent to claiming or starting the installation.

The Debian 13 Standard driver uses initrd preseeding. Its rendered Preseed contains desired-state configuration and SSH public keys but no lifecycle credential, reusable password, private key or recovery secret. The assignment-gated iPXE script loads the verified Debian initrd, injects the AegisPXE reporter binary and non-secret reporter configuration, then injects the served `preseed.cfg` as `/preseed.cfg` into iPXE's magic initrd. Debian Installer consumes that file as native initrd preseed material.

The reporter binary, reporter configuration and Preseed are non-secret public boot material. They are available only while the exact InstallationSpec remains armed for an operator-approved Machine. The reporter configuration contains identifiers and API origin only, never lifecycle secret material.

AegisPXE does not use Debian `preseed/url` for this path and does not maintain a custom CPIO/initrd repacker.

If a future driver requires secret-bearing seed delivery, that path additionally requires cryptographic boot trust and an explicitly reviewed release mechanism.

## Boot assignment

Per ADR 0005, an armed assignment is a one-shot destructive boot lease.

Boot script, reporter, reporter configuration, kernel and initrd reads remain retryable and non-consuming. The rendered `preseed.cfg` is intentionally the final network object fetched by the iPXE script. Immediately before AegisPXE returns that Preseed successfully, it atomically transitions the assignment from `armed` to `consumed` and records the assignment-consumption event.

Consumption means only that AegisPXE committed the final public boot handoff for the destructive attempt. It does not mean `INSTALLER_STARTED`, successful installation or cryptographic trust.

A consumed assignment may complete TPM boot trust for that exact InstallationSpec because reporter execution occurs after the final Preseed handoff. A cancelled assignment must never authorize challenge issuance, proof completion or lifecycle credential release.

At most one assignment may be armed for one Machine. Arming, consuming and cancelling assignments are auditable state mutations.

## TPM reporter enrollment

The Debian reporter creates a deterministic RSA primary key in TPM 2.0. The private key is not exported by AegisPXE code. Only the PKIX public key is sent to the server.

The server:

- validates supported key parameters,
- fingerprints the public key with SHA-256,
- binds the candidate to the Machine,
- stores it as `pending`,
- requires explicit administrator approval before it can authorize a challenge,
- permits at most one approved boot-trust key per Machine,
- supports explicit revocation.

The administrator approval step is security-sensitive. Approving the wrong candidate establishes trust in that key. Studio therefore exposes the fingerprint and enrollment state instead of silently auto-approving first contact.

## Boot-trust freshness and replay

A trust challenge is bound to:

- challenge ID,
- installation ID,
- machine ID,
- approved key fingerprint,
- random 256-bit nonce,
- short expiry.

The reporter signs a canonical challenge with the TPM key. The server verifies the signature against the approved public key and current installation/assignment binding before releasing credential ciphertext.

A successful proof cannot mint a second lifecycle credential. Exact retries return the already generated encrypted response. Revoked keys and cancelled installation bindings are rejected.

## Secret handling

Secrets include:

- recovery keys,
- passwords,
- private keys,
- bearer tokens,
- HMAC authentication material,
- session credentials,
- lifecycle credentials,
- recovery material.

Secrets must not be placed in:

- normal logs,
- audit details,
- URLs/query strings,
- public GRUB/iPXE configuration,
- machine metadata,
- error messages.

Secret storage is accessed through narrow abstractions. Recovery material is revealed only through an explicit authorized action that itself creates an audit event.

## Artifact integrity

Provisioning artifacts are identified by cryptographic digest and provenance metadata. A driver may not return an artifact as usable until integrity verification succeeds.

A hash mismatch is a hard failure. AegisPXE must never silently use an existing file whose digest does not match the expected artifact identity.

Where upstream signatures or signed checksum metadata are available, drivers/artifact resolvers should verify them in addition to the final digest.

The packaged Debian reporter is part of the trusted AegisPXE package payload. The package build and smoke test must verify that the reporter executable is present at the fixed path served by the boot endpoint.

## Administrative authorization

The initial role model should remain small. Security-sensitive actions require explicit permission checks, including:

- approving/blocking/deleting machines,
- creating/arming/cancelling installations,
- approving or revoking TPM boot-trust keys,
- changing profiles,
- managing other trusted keys,
- revealing recovery secrets,
- changing system settings.

All such actions produce audit events.

### Bootstrap operator boundary

The first Debian vertical path uses a deliberately narrow bootstrap operator mechanism before richer user/RBAC support exists.

- AegisPXE generates a random 256-bit bootstrap operator key under `/var/lib/aegispxe/operator.key` by default.
- The key file is a regular file with no group/other access; symlinks and unsafe existing permissions are rejected.
- The key value is not logged and is exchanged for a short-lived server-side session rather than stored in browser storage.
- Session cookies are HttpOnly and SameSite=Strict. HTTPS/trusted-proxy sessions use the Secure attribute.
- Every browser mutation requires a CSRF value bound to the server-side session.
- Login attempts are rate limited.
- Direct bootstrap login and mutations are refused on cleartext non-loopback network HTTP.

The bootstrap operator does not satisfy Machine/installer trust and does not authorize release of lifecycle credentials merely by existing.

The bootstrap key remains a local/recovery path when a separately authenticated reverse proxy is used for normal Studio access.

### Trusted reverse-proxy Studio boundary

AegisPXE may accept a human operator identity asserted by an explicitly configured reverse proxy/SSO boundary. This does not make arbitrary proxy headers trusted.

The trust decision requires all of the following:

- the **direct TCP peer address** is contained in configured `AEGISPXE_TRUSTED_PROXY_CIDRS`,
- the configured protocol header has the exact value `https`,
- the configured identity header contains a bounded non-empty identity.

The identity and protocol header names are deployment configuration. The reverse proxy must overwrite or remove client-supplied instances of these headers before forwarding.

A request outside the configured proxy network cannot gain operator authority by forging the same headers. When Trusted Proxy mode is enabled, the Studio backend accepts only loopback recovery traffic or a request satisfying this trusted-proxy contract.

A trusted proxy identity is exchanged for the same short-lived server-side AegisPXE operator session and CSRF contract as other browser administration. Audit actors are recorded as `proxy:<identity>`. AegisPXE does not need to store the reverse proxy's Passkey/WebAuthn credentials.

Reverse-proxy operator trust is human administrative authentication only. It does not satisfy cryptographic boot trust, authenticate an installer, or directly release lifecycle credentials.

No external Studio hostname or public origin is compiled into the application. See `docs/adr/0004-studio-trusted-proxy.md`.

## Input validation

External identifiers, URLs, filenames, paths, hostnames, MAC addresses, driver IDs, profile values, TPM public keys, signatures, timestamps and telemetry bodies are validated at domain boundaries.

Path traversal and user-controlled filesystem destinations are forbidden. Internal paths should be constructed from typed IDs and fixed roots.

## Logging and redaction

Security decisions must be logged without logging secrets. See `OBSERVABILITY.md`.

Authentication failures, authorization failures, invalid lifecycle events, replay attempts, assignment conflicts, boot-trust failures and artifact integrity failures require structured security logs with stable error codes.

Bootstrap/trusted-proxy operator logs may contain request ID, direct remote address, actor after authentication, decision result and a non-secret cause class. They must never contain the bootstrap key, session cookie or CSRF value.

Boot-trust logs may contain installation ID, machine ID, public-key fingerprint, challenge ID, decision result and error code. They must never contain lifecycle plaintext, request HMACs or TPM private material.

## Replay and sequencing

Installation events include an idempotency mechanism. The server rejects invalid state regressions and handles duplicate reports deterministically.

A replayed request must not create duplicate state transitions.

Cryptographic boot trust uses server-generated freshness/challenge material. Reporter telemetry also includes a freshness timestamp in its authenticated canonical request. Previously valid traffic must not become a new lifecycle transition merely by replay.

## Secure defaults

Defaults favor:

- SSH keys over passwords,
- root login disabled,
- minimum necessary exposed services,
- verified artifacts,
- supported/LTS operating systems where available,
- explicit TPM key approval over first-contact auto-trust,
- local boot when there is no explicit provisioning assignment,
- refusal rather than guessing when state is inconsistent.

## Security changes

Changes to trust boundaries, credential scope, helper permissions, artifact trust, secret handling or authorization require documentation updates and an ADR when the architectural contract changes.

A security-relevant behavior change without tests is not mergeable.
