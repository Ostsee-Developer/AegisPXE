# Testing Strategy

AegisPXE is built vertical-slice first. Tests must prove both successful provisioning and useful failure observability.

## Test layers

### Unit tests

Pure domain logic, validation, state transitions, redaction and deterministic driver rendering.

Unit tests must not require root, network access or a VM.

### Integration tests

Database transactions, event persistence, machine discovery, API authentication, helper protocol boundaries, artifact metadata handling and driver/server integration.

Integration tests use temporary isolated state and must be repeatable.

### E2E tests

Real boot/install/first-boot flows in disposable VMs. E2E is a release gate, not a manual bonus step.

## First milestones

### Milestone 0: Machine discovery

Repeatedly prove:

1. unknown VM PXE boots,
2. AegisPXE records exactly one stable machine identity,
3. discovery event/logs are present,
4. machine appears as `pending`,
5. no seed/provisioning access is granted,
6. PXE exits to local boot/non-provisioning behavior,
7. repeated boots update `last_seen` without creating duplicate machines.

Target before moving on: at least 20 repeated discovery boots with no inconsistent state.

### Milestone 1: Debian 13 Standard

A disposable VM must complete:

PXE -> installer -> storage -> OS -> profile -> first boot -> validation -> local reboot

with no manual input.

Before adding Debian Encrypted, Standard should complete at least 10 consecutive clean E2E runs and at least one intentionally failed run must produce useful stage/error/log output.

## Release gate philosophy

A feature is not complete because unit tests pass. For provisioning features, completion means:

- deterministic compiler tests,
- integration tests for server state,
- E2E success path,
- E2E or integration failure path,
- expected logs/events verified,
- documentation current.

## Failure tests

We deliberately test failures such as:

- invalid/missing artifact digest,
- installer never starts,
- invalid lifecycle token,
- duplicate/replayed event,
- storage hook reports failure,
- first-boot validation fails,
- machine is blocked while assignment exists,
- helper refuses an invalid privileged action.

Failure tests must assert both behavior and observability.

## Observability assertions

Operational tests should verify relevant records contain correlation identifiers and stable error codes.

Tests must also verify sensitive fixtures are redacted and never persisted in normal logs.

## Driver certification

A driver cannot be marked production-ready until it demonstrates:

- artifact resolution,
- deterministic boot render,
- deterministic seed render,
- authenticated telemetry,
- first-boot execution,
- requested-state validation,
- full unattended E2E,
- useful failed-install telemetry.

## CI

Initial CI should enforce at least:

- `gofmt`,
- `go test ./...`,
- `go vet ./...`,
- static/security checks chosen by the project,
- documentation contract checks,
- no generated/uncommitted diffs where generation is used.

E2E VM tests may run in a dedicated environment but must remain a required release gate before a supported target is declared stable.