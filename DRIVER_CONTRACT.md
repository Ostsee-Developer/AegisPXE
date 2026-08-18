# OS Driver Contract

An AegisPXE OS driver owns the complete installer-specific behavior for one supported OS family/release contract.

Drivers translate an immutable InstallationSpec and OS-neutral desired state into native installer behavior. They do not leak installer syntax back into profiles.

## Required capabilities

A production-capable driver must implement all of these concerns:

1. target identification,
2. artifact resolution and integrity metadata,
3. boot specification rendering,
4. unattended seed/configuration rendering,
5. installer telemetry integration,
6. first-boot runtime integration,
7. validation integration,
8. capability reporting,
9. deterministic tests,
10. E2E test fixture/plan.

A driver without telemetry or validation is incomplete and cannot be marked production-ready.

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

For example, the fact that Debian and Ubuntu both use systemd does not mean installer-time `systemctl` semantics are interchangeable.

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

`RenderBoot` returns a typed BootSpec containing only the data the boot transport needs, for example kernel/initrd references and a bounded argument set.

Boot rendering must not embed secrets in public paths or logs.

## Seed contract

Seed/configuration material is installation-scoped and may be requested multiple times. Drivers must not assume one fetch equals one installer start.

The driver documents how the native installer locates its seed and which requests are expected during a healthy installation.

## Telemetry contract

The driver defines how native installer stages produce authoritative lifecycle events and log streams.

A stage may only be emitted when the native installer has actually reached that stage. The driver must document the evidence/hook used for each emitted event.

## First-boot contract

Runtime-only operations execute only when the installed OS has booted into the environment required by those operations.

A driver must not start services or apply live-kernel state from a target chroot unless the native platform explicitly guarantees that behavior.

First-boot logic must be idempotent and report success/failure.

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

`COMPLETED` may only be emitted after required validation passes.

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
- telemetry level.

No feature is silently downgraded.

## Versioning

An InstallationSpec stores driver ID and driver version. Updating a driver must not change an already armed installation's rendered contract without an explicit migration/rebuild decision.

Breaking driver behavior requires tests and documentation, and may require an ADR.

## Initial drivers

Development order:

1. `debian13`
2. `ubuntu2404`
3. `ubuntu2604`
4. `centos` / Kickstart target selected after supported-version policy is defined

Do not create skeleton implementations for later drivers until their vertical slice begins.