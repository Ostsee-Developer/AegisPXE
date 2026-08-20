# ADR 0010: Per-installation managed agent identity and control plane

- Status: Accepted for 0.2.0-dev.1 implementation
- Date: 2026-08-20

## Context

AegisPXE needs a persistent installed-OS management channel so that a provisioned Machine can report presence and later support bounded diagnostics, log collection and typed maintenance jobs without relying on ad-hoc first-boot scripts or SSH as the primary control path.

A universal agent binary with only runtime-supplied identity would make package copying, policy drift and per-installation privilege separation harder to reason about. AegisPXE already treats an InstallationSpec as one immutable provisioning attempt, so the managed agent should be bound to that same installation boundary.

The previous installer reporter design is intentionally separate. It attempted to provide authenticated telemetry inside the installer environment and is currently suspended from the production Debian boot path. The managed agent introduced here starts from the installed operating system and must not be used to infer installer lifecycle stages that it did not authoritatively observe.

## Decision

AegisPXE will build one managed-agent package for each InstallationSpec.

Each build uses the common `aegispxe-agent` source code but embeds non-secret installation identity and policy metadata at build time:

- a random canonical UUID identifying the Agent,
- the exact InstallationSpec ID,
- the exact Machine ID,
- the AegisPXE agent version,
- the build generation,
- the target architecture,
- the installation-specific capability ceiling,
- public trust material required to authenticate the owning AegisPXE instance and signed updates.

The Agent UUID is an identifier, not an authentication secret. Copying or extracting the package must not be sufficient to authenticate as the Agent.

The Debian package name remains `aegispxe-agent`. Individual builds are distinguished by version/build metadata, Agent identity, generation and their signed manifest rather than by creating a different Debian package name for every Machine.

## Build and installation boundary

Agent creation is installation-scoped. AegisPXE persists Agent identity before a build is attempted. Builds are asynchronous, bounded jobs and are not performed synchronously inside the installation-creation HTTP request.

An installation may not be armed for destructive provisioning until its required Agent build is `ready`. A failed Agent build is therefore a visible provisioning preflight failure rather than a silent post-install defect.

The native Debian installer path remains intact. AegisPXE must not repack the Debian initrd to deliver this Agent. The Agent package is installed through the OS driver's supported target-system installation mechanism.

## Authentication and enrollment

The build contains no reusable enrollment secret, private key or bearer credential.

AegisPXE creates a separate short-lived, single-use enrollment credential for the exact Agent/Installation/Machine tuple. The installer may place that credential on the installed system only through a protected installation-scoped handoff. It must not appear in public boot configuration, kernel arguments, package metadata or logs.

On first installed-OS start, the Agent generates its own private key locally and enrolls with AegisPXE. Successful enrollment binds the Agent UUID, Installation ID, Machine ID and client public key. AegisPXE then issues the long-lived client identity used for mutually authenticated Agent traffic. The bootstrap credential is marked consumed server-side and removed from the client.

Replays, cross-installation credentials and identity mismatches fail closed.

## Network model

The Agent initiates communication to AegisPXE. It does not require a remotely reachable inbound management listener on the managed Machine.

The Agent control plane is distinct from the public PXE listener and the browser Studio listener. Long-lived authenticated Agent traffic uses TLS with explicit server trust and client authentication after enrollment.

## Capability model

The Agent has a small mandatory protocol baseline required to remain manageable, including heartbeat/presence, basic self-identification and the signed managed-update protocol.

Privileged or remotely requested operations are separate capabilities. Their maximum set is embedded in each Agent build as a capability ceiling. Runtime server policy may grant only a subset of that ceiling:

```text
effective capabilities = build capability ceiling ∩ current server policy
```

A server request outside the build ceiling is rejected by the Agent even if the server-side policy is misconfigured.

`0.2.0-dev.1` does not introduce an arbitrary shell, generic root command endpoint or unrestricted package-execution primitive.

## Heartbeat and lifecycle separation

Heartbeat is Agent presence evidence, not installation lifecycle evidence.

AegisPXE computes `online`, `degraded` and `offline` from authenticated heartbeat receipt time. The Agent does not self-assert those server-side states.

An Agent heartbeat, successful enrollment, successful update or successful local boot must not manufacture `INSTALLER_STARTED`, `OS_INSTALLING`, `SUCCESS` or any other installer lifecycle stage. Any future use of the installed Agent as a first-boot finalizer or validator requires an explicit lifecycle-authority contract and tests.

## Managed updates

AegisPXE remains the update authority for Agents it created.

An Agent update is accepted only when the signed update metadata matches the exact Agent identity and Installation identity, targets the local architecture, has a strictly acceptable generation, matches the expected cryptographic digest and validates under the configured AegisPXE update-signing trust anchor.

Update signing is cryptographically separate from transport authentication. The update manifest remains verifiable even if transport handling changes.

A small updater helper performs replacement/restart and preserves the previous known-good package for rollback. An update is not considered successful until the new generation returns healthy after restart and AegisPXE receives confirmation.

## Persistence and audit

Agent, Agent-build, enrollment and certificate state are first-class persisted records. Meaningful state mutations produce append-only audit events in the same logical operation.

Operational logs use Agent-specific structured fields where applicable:

- `agent_id`,
- `agent_build_id`,
- `agent_generation`,
- `agent_version`,
- `agent_state`,
- `update_state`,
- `installation_id`,
- `machine_id`,
- `request_id` or `job_id`.

The `AGT` stable error-code namespace is reserved for managed-Agent build/runtime errors. Security failures that are broader platform authentication properties may continue to use the `SEC` namespace when appropriate.

Secrets, private keys, bootstrap credentials and bearer material must never be logged.

## Consequences

- Creating many installations can create many Agent binaries and Debian packages. Build caching and bounded concurrency are required in the implementation.
- Package identity is installation-specific even though the source and Debian package name are shared.
- Agent policy changes that increase the hard capability ceiling require a new build generation; runtime policy cannot exceed the installed ceiling.
- A copied package does not become a valid clone because enrollment and client authentication remain installation-bound.
- The managed Agent can later carry diagnostics, log retrieval and typed jobs without redesigning identity or update trust.
- The Debian production boot chain remains independent from the suspended installer-reporter design.
