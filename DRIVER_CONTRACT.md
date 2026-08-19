# OS Driver Contract

An AegisPXE OS driver owns the complete installer-specific behavior for one supported OS family/release contract.

Drivers translate an immutable InstallationSpec and OS-neutral desired state into native installer behavior. They do not leak installer syntax back into profiles.

## Required capabilities

A production-capable driver must implement all of these concerns:

1. target identification,
2. artifact resolution and integrity metadata,
3. boot specification rendering,
4. unattended seed/configuration rendering,
5. authenticated installer telemetry integration,
6. first-boot runtime integration,
7. validation integration,
8. capability reporting,
9. deterministic tests,
10. E2E test fixture/plan.

A driver without telemetry or validation is incomplete and cannot be marked production-ready. Production telemetry must also satisfy the authentication contract for that driver.

## Conceptual Go interface

The exact interface may evolve, but the semantic contract is:

```go
type Driver interface {
    ID() string
    Version() string
    Capabilities() Capabilities

    ResolveArtifacts(ctx context.Context, target TargetOS) ([]ArtifactSpec, error)
    ValidateSpec(ctx context.Context, spec InstallationSpec) error
    RenderBoot(ctx context.Context, spec InstallationSpec) (BootSpec, error)
    RenderSeed(ctx context.Context, spec InstallationSpec) (SeedBundle, error)
    RenderTelemetry(ctx context.Context, spec InstallationSpec) (TelemetryBundle, error)
    RenderFirstBoot(ctx context.Context, spec InstallationSpec) (FirstBootBundle, error)
    RenderValidation(ctx context.Context, spec InstallationSpec) (ValidationBundle, error)
}
```

The implementation must remain testable without root and without booting a VM for deterministic render tests.

## Separation rule

Drivers may share pure utilities and stable domain types. They may not share runtime command lists merely because two Linux distributions happen to use the same command names.

Each driver is responsible for knowing:

- which phase runs in an installer environment,
- which phase runs in a target chroot,
- which phase runs after the installed OS booted,
- which native installer hooks actually exist,
- when system services can safely be started,
- which native logs represent real progress.

## Desired-state input

Drivers receive semantic intent such as:

- create admin account,
- install authorized SSH key,
- disable password authentication,
- install packages,
- apply firewall intent,
- encrypt storage,
- enroll TPM when supported,
- enable automatic security updates,
- run final validation.

Drivers do not receive arbitrary administrator-provided shell commands as part of normal profiles.

## Boot contract

`RenderBoot` returns a typed BootSpec containing only the data the boot transport needs: installation/driver identity, digest-pinned kernel/initrd references, a bounded argument set and a non-secret seed reference.

The BootSpec is a deterministic derivative of the immutable InstallationSpec. It is not an independent desired-state record and must not be persisted as a second source of truth.

Boot rendering must not embed secrets in public paths, kernel arguments or logs. Fetching or rendering boot material does not imply `INSTALLER_STARTED`.

Assignment consumption follows the separate one-shot public handoff contract in ADR 0005. A driver must not reinterpret assignment consumption as lifecycle progress.

### Secure Boot

A driver that advertises Secure Boot compatibility must define the complete executable trust chain, not merely a firmware flag. It must identify every signed boot component, the trust authority for each transition, the artifact-integrity mechanism used by AegisPXE and the negative behavior when validation fails.

Secure Boot observation is inventory/policy evidence and must not be described as remote attestation unless a separate cryptographic attestation protocol actually proves the state.

The Debian 13 driver contract v2 pins `linux`, native `initrd.gz` and Debian `bootnetx64.efi` from one verified Debian installer provenance. AegisPXE configures the Debian shim through iPXE's `shim` command but does not modify or repack the known-good Debian initrd. The first-stage signed iPXE chain is a platform concern described by ADR 0009.

No feature is silently downgraded when Secure Boot policy is `required`.

## Seed contract

Seed/configuration material is installation-scoped. Drivers must document how the native installer locates its seed and which requests are expected during a healthy installation.

When a seed is intentionally the final object of a destructive public handoff, its serving layer may consume the one-shot Assignment according to ADR 0005. That scheduling action still does not create installer lifecycle progress.

Secrets must not be inserted into public seed material merely because the native installer makes seed access convenient.

## Telemetry contract

The driver defines how native installer stages produce authoritative lifecycle events and log streams.

A stage may only be emitted when the native installer has actually reached that stage. The driver must document the native evidence/hook used for each emitted event.

Telemetry transport must authenticate the exact InstallationSpec and preserve idempotency, ordering, source authorization and request freshness. If the boot transport is cleartext, the driver must not send a reusable lifecycle credential in plaintext.

A telemetry integration must also define:

- how its runtime reporter becomes available to the installer,
- how machine/boot trust is established before credential release,
- how installer-native logs are bounded and uploaded,
- what happens when trust cannot be established,
- how failure telemetry is attempted from every supported installer stage.

### Debian 13 reporter status

The earlier Debian reporter/initramfs injection design is suspended after failing the real UEFI/vTPM E2E path. The production Debian transport is intentionally restored to native kernel + native initrd + Preseed and dev.22 Secure Boot does not reintroduce reporter injection.

TPM trust, signed telemetry and reporter source may remain as isolated protocol/runtime work, but they are not part of the current production Debian boot contract. A replacement reporter delivery mechanism must be designed around the proven installer path and pass its own E2E gate before production exposure.

## First-boot contract

Runtime-only operations execute only when the installed OS has booted into the environment required by those operations.

A driver must not start services or apply live-kernel state from a target chroot unless the native platform explicitly guarantees that behavior.

First-boot logic must be idempotent and report success/failure.

If a first-boot credential must cross a reboot boundary, plaintext credential material must not be written to the installed filesystem.

## Validation contract

A driver exposes final checks appropriate to the desired state. Required validation is derived from the InstallationSpec, not from assumptions about a default profile.

Examples:

- expected admin exists,
- authorized key installed,
- SSH configuration parses,
- requested firewall is active,
- requested services are healthy,
- encryption state matches intent,
- TPM enrollment matches intent when requested,
- expected boot mode/security properties are present when testable.

`SUCCESS` may only be emitted after required validation passes. Validation failure emits `FAILED` with a stable error code and correlated log context.

## Capability matrix

A driver reports supported features explicitly. Unsupported desired state must fail preflight before an installation is armed.

Capabilities may include:

- UEFI,
- BIOS if supported,
- Secure Boot compatibility,
- LVM,
- disk encryption,
- TPM2 enrollment,
- custom packages,
- firewall configuration,
- automatic updates,
- telemetry level,
- first-boot validation level.

No feature is silently downgraded.

## Versioning

An InstallationSpec stores driver ID and driver contract version. Driver contract versions are independent from the AegisPXE application version. A UI, packaging or unrelated server change therefore does not invalidate a driver contract.

A current driver may keep an explicitly identified legacy contract readable for safe migration or cleanup, but it must not silently reinterpret that legacy spec as satisfying a newer security capability. New InstallationSpecs use the current contract version.

Any behavior change that can alter rendered boot/seed/runtime semantics requires an intentional driver-version decision and tests. Updating a driver must not silently reinterpret an already armed installation's rendered contract. Breaking driver behavior requires documentation and may require an ADR.

For Debian 13, contract v1 remains readable as the pre-Secure-Boot legacy contract. Contract v2 is the first contract that pins the Debian signed shim artifact and may be authorized under a `required` Secure Boot policy.

## Initial drivers

Development order:

1. `debian13`
2. `ubuntu2404`
3. `ubuntu2604`
4. `centos` / Kickstart target selected after supported-version policy is defined

Do not create skeleton implementations for later drivers until their vertical slice begins.
