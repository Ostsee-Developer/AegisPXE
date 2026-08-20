# Managed Agent Security Contract

This document defines the security contract for the installed `aegispxe-agent` introduced in AegisPXE `0.2.0-dev.1`.

The managed Agent is a post-install control-plane component. It is not the suspended installer reporter and it does not gain installer lifecycle authority merely by being installed or online.

## Identity model

Every installation gets exactly one Agent identity.

The Agent identity is bound to:

- one canonical random Agent UUID,
- one immutable InstallationSpec ID,
- one Machine ID,
- one build generation,
- one target architecture,
- one capability ceiling.

The Agent UUID, Installation ID and Machine ID are identifiers and may be embedded in the binary. They are not secrets and must never be accepted as sufficient authentication factors.

The initial package must contain no private key, reusable enrollment token or plaintext long-lived credential.

## Trust layers

Managed-Agent trust is layered:

1. **Build binding**: the binary and signed manifest identify the Agent, Installation, Machine, architecture, generation and capability ceiling.
2. **Artifact integrity**: package and manifest digests must match the server-side build record.
3. **Bootstrap enrollment**: a short-lived single-use secret authorizes only the first enrollment of the exact Agent/Installation/Machine tuple.
4. **Client key ownership**: the Agent creates its private key locally; the private key never leaves the managed Machine.
5. **Mutual authentication**: after enrollment, normal Agent traffic uses server-authenticated TLS plus the Agent client identity.
6. **Runtime authorization**: requested operations must be allowed by both current server policy and the build-time capability ceiling.

Failure at any layer fails closed.

## Network exposure

The Agent is outbound-only by default.

It must not open a general inbound administration port. AegisPXE polling, heartbeat, update discovery and later job retrieval are initiated by the Agent toward the dedicated Agent control-plane listener.

The public PXE listener, Studio listener and Agent control plane are separate trust surfaces and must not silently inherit each other's authentication assumptions.

## Enrollment rules

Enrollment credentials are:

- generated from cryptographically secure randomness,
- stored server-side only as a fixed-size verifier/hash,
- bound to Agent ID, Installation ID and Machine ID,
- time-limited,
- single-use,
- revocable,
- excluded from logs, URLs, kernel arguments, public boot data and package metadata.

The installer may persist the one-time bootstrap material only in a root-owned regular file with narrow permissions for the first installed-OS boot. Successful enrollment consumes the server credential and removes the client bootstrap file.

A reused credential, expired credential, revoked credential, wrong Agent, wrong Installation or wrong Machine is rejected and audited.

## Client keys and certificates

The Agent generates its private key locally at first enrollment. AegisPXE receives only the public key or CSR required to issue the Agent client identity.

Client identity records are Agent-scoped. Revocation of an Agent or certificate must immediately remove its ability to authenticate future control-plane requests.

Certificate/private-key rotation must not change the Agent UUID. Agent identity and credential generation are separate concepts.

## Capability ceiling

The build-time capability ceiling is immutable for one Agent generation.

The ceiling applies to remotely requested privileged behavior, not to the minimal protocol required for the Agent to remain manageable. The dev.1 baseline protocol includes authenticated heartbeat, bounded basic inventory and the managed signed-update mechanism.

Future examples of explicit privileged capabilities may include:

- `diagnostics.read`,
- `logs.read`,
- `service.status`,
- `service.restart`,
- `system.reboot`.

No capability implies another capability unless the contract explicitly says so.

No Agent may expose a generic arbitrary-shell endpoint. A future script/job mechanism, if introduced, requires a separately bounded capability, payload contract, timeout/output limits, audit semantics and security review.

## Build security

Per-installation builds run as bounded asynchronous jobs.

Build inputs must be typed values from persisted AegisPXE state. User-controlled strings must not be concatenated into shell commands. The builder uses fixed tool paths/arguments, fixed output roots and a clean temporary working directory.

Build jobs require:

- explicit architecture allowlisting,
- bounded runtime,
- bounded concurrency,
- deterministic version/generation metadata,
- SHA-256 package digest,
- signed manifest,
- structured success/failure logs,
- cleanup that cannot escape the configured build root.

A build failure leaves the installation unarmable and must be diagnosable from Studio/API without SSH to the AegisPXE host.

## Update security

The Agent accepts updates only from its owning AegisPXE trust domain.

Before installation, the Agent/updater verifies at least:

- exact Agent UUID,
- exact Installation ID,
- expected architecture,
- update generation policy,
- expected package digest,
- signed manifest,
- allowed capability ceiling encoded by that generation.

Transport success alone does not establish update integrity.

The updater keeps the previous known-good package until the new Agent generation passes local startup/health confirmation. Failed confirmation triggers rollback where the previous package remains usable.

AegisPXE records requested, downloaded, verified, installed, confirmed, failed and rollback outcomes as structured state/audit transitions when those phases exist.

## Heartbeat security

Heartbeat requests are authenticated Agent traffic after enrollment.

Heartbeat payloads are bounded and validated. Server receipt time is authoritative for presence. Client timestamps and uptime are telemetry only.

Presence projection is server-side. Initial policy is:

```text
last authenticated heartbeat < 90s   -> online
90s to < 180s                         -> degraded
>= 180s                               -> offline
```

The exact thresholds may become configuration, but a client may never set its own server-side `online` state.

Heartbeat receipt must not create or infer installation lifecycle progress.

## Persistence

Schema v9 introduces first-class managed-Agent records for:

- Agent identity/current presence projection,
- build generations,
- enrollment credential verifier state,
- issued/revoked client identities.

Raw enrollment secrets and client private keys are never stored in the AegisPXE database.

Only the latest bounded heartbeat snapshot is required for dev.1. Heartbeat history is not stored as an unbounded append-only stream.

## Logging and audit

Managed-Agent operations use structured logging and append-only audit events for state mutation.

Useful non-secret fields include:

- `request_id`,
- `job_id`,
- `machine_id`,
- `installation_id`,
- `agent_id`,
- `agent_build_id`,
- `agent_generation`,
- `agent_version`,
- `agent_state`,
- `update_state`,
- public cryptographic digests/fingerprints.

Never log:

- bootstrap credentials,
- private keys,
- Authorization headers,
- client-key material,
- session/cookie material,
- plaintext secrets contained in future job output.

Managed-Agent domain errors use the `AGT` namespace. Security-policy failures may use `SEC` where the failure belongs to a shared authentication/authorization boundary rather than Agent business state.

## Dev.1 non-goals

`0.2.0-dev.1` does not claim:

- TPM remote attestation of the installed Agent,
- installer-stage telemetry authority,
- arbitrary remote command execution,
- remote interactive shell access,
- unrestricted package installation,
- immutable protection against a local root attacker.

TPM-backed client-key protection and measured-boot attestation may strengthen this model later without changing the per-installation Agent identity contract.

## Release gate

The managed Agent is not considered production-capable merely because it compiles.

Dev.1 requires focused tests for identity validation, schema migration, duplicate Agent prevention, credential replay rejection once enrollment is implemented, wrong-Agent package rejection, signed update verification, heartbeat timeout projection, update failure/rollback and secret-free diagnostics. A real provisioning fixture must demonstrate that the Agent package is installed without modifying the stabilized Secure Boot/native-initrd path.

See ADR 0010 for the architectural decision and `TESTING.md`, `OBSERVABILITY.md` and `SECURITY.md` for the project-wide contracts.
