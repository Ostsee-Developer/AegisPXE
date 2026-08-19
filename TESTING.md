# Testing Strategy

AegisPXE is built vertical-slice first. Tests must prove both successful provisioning and useful failure observability without becoming a second implementation of the product.

## Core test rule

**One test should prove one contract.**

Tests should be small, explicit and easy to diagnose. We do not optimize for maximum assertion count or maximum coverage per test function.

A test is a review concern when understanding what it proves requires mentally executing a large fixture, a copied implementation, or hundreds of lines of setup/output.

There is deliberately no arbitrary line-count limit. Moving complexity into test helpers merely to satisfy a number does not make a test simpler.

## What tests must not become

Avoid:

- reimplementing production algorithms inside tests,
- giant end-to-end behavior simulations in unit tests,
- asserting complete installer documents when only one contract changed,
- large inline JSON/YAML/Preseed/Cloud-Init blobs for a single assertion,
- broad golden files for unstable or incidental output,
- duplicated assertions across unit, integration and E2E layers,
- one test that attempts to validate an entire subsystem.

Prefer:

- focused inputs,
- focused expected outcomes,
- small builders/fixtures with meaningful defaults,
- table-driven tests only when cases share the same contract,
- semantic assertions over exact full-document equality,
- dedicated contract tests for security, state and observability invariants.

Golden files are acceptable only for intentionally stable external formats where reviewing the whole generated artifact provides real value. They must not become snapshots that approve unrelated churn.

## Test layers

### Unit tests

Pure domain logic, validation, state transitions, redaction and deterministic driver rendering.

Unit tests must not require root, network access or a VM.

A unit test should normally exercise one domain rule. Examples:

- `pending -> provision` is allowed,
- an InstallationSpec rejects a non-canonical artifact digest,
- a caller mutation cannot change an immutable InstallationSpec snapshot,
- a secret field is redacted,
- a Debian driver renders the required installer directive,
- changing a signed telemetry path/body/timestamp invalidates its MAC,
- a boot-trust public key outside the supported RSA contract is rejected.

Do not test the entire Debian seed to prove one directive exists.

### Integration tests

Database transactions, event persistence, machine discovery, API authentication, helper protocol boundaries, artifact metadata handling and driver/server integration.

Integration tests use temporary isolated state and must be repeatable.

Each integration test should focus on one boundary or transaction. Examples:

- discovery atomically persists one machine and one event,
- InstallationSpec creation atomically persists immutable state plus its audit record,
- pending TPM key enrollment cannot release a lifecycle credential,
- an explicitly approved TPM key may complete a fresh challenge for the exact InstallationSpec,
- a consumed assignment remains eligible for the exact installation's trust proof,
- a cancelled assignment rejects trust completion,
- a valid reporter HMAC reaches lifecycle validation while a tampered body is rejected before lifecycle mutation.

### E2E tests

Real boot/install/first-boot flows in disposable VMs. E2E is a release gate, not a manual bonus step.

E2E owns the complete workflow. Unit and integration tests should not reproduce E2E behavior in miniature.

E2E assertions stay outcome-oriented:

- expected lifecycle terminal state,
- expected required events,
- expected machine/install identity,
- expected validation result,
- useful logs for failure,
- expected local-boot behavior after the one-shot handoff.

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

PXE -> installer -> storage -> OS -> profile -> hardening -> first boot -> validation -> local reboot

with no installer input after the operator's explicit provisioning/trust decisions.

Before adding Debian Encrypted, Standard should complete at least 10 consecutive clean E2E runs and at least one intentionally failed run must produce useful stage/error/log output.

### Milestone 1 trust/reporter gate

The TPM reporter path requires a VM with TPM 2.0 enabled. Before merging the first reporter implementation, a real E2E run must prove:

1. reporter binary and non-secret configuration are injected into the Debian Installer initrd,
2. the reporter creates/recreates the same TPM-bound public key across installer and first boot,
3. first contact produces a pending key and no lifecycle credential is released,
4. administrator approval is required and is audited,
5. the approved key completes a fresh installation/machine/key-bound challenge,
6. the server returns only TPM-encrypted lifecycle credential ciphertext,
7. the raw lifecycle credential does not appear in Preseed, iPXE, URLs, logs or installed files,
8. reporter event/log traffic authenticates without transmitting the raw credential,
9. the exact lifecycle reaches `INSTALLER_STARTED`, `DISK_PREPARATION`, `OS_INSTALLING`, `PROFILE_APPLYING`, `HARDENING`, `FIRST_BOOT`, `VALIDATING`, `SUCCESS`,
10. installer and validation logs are correlated to the InstallationSpec,
11. terminal reporting revokes runtime credential authority,
12. the next PXE-first reboot chains the installed local bootloader instead of reinstalling.

The first TPM E2E is intentionally a manual hardware/virtual-TPM gate if CI does not provide a TPM device. Passing normal Go/package CI is necessary but not sufficient to declare this trust path production-ready.

## Release gate philosophy

A feature is not complete because unit tests pass. For provisioning features, completion means:

- small deterministic contract tests,
- focused integration tests for state/boundaries,
- E2E success path,
- E2E or integration failure path,
- expected logs/events verified,
- documentation current.

## Failure tests

We deliberately test failures such as:

- invalid/missing artifact digest,
- installer never starts,
- invalid/expired lifecycle authentication,
- tampered signed reporter body/path/timestamp,
- pending/revoked/wrong-machine TPM key,
- expired or mismatched boot-trust challenge,
- cancelled assignment attempting secret release,
- duplicate/replayed event,
- storage hook reports failure,
- first-boot validation fails,
- machine is blocked while assignment exists,
- helper refuses an invalid privileged action.

Failure tests must assert both behavior and observability, but should assert only the relevant error code/event/log fields rather than an entire log stream.

## Observability assertions

Operational tests should verify relevant records contain correlation identifiers and stable error codes.

Tests must also verify sensitive fixtures are redacted and never persisted in normal logs.

For the reporter path, tests must explicitly guard against logging or persistence of lifecycle plaintext, request authentication material and TPM private data.

Do not assert timestamps, formatting details or unrelated log fields unless they are part of the contract under test.

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

These requirements should be covered by separate focused tests plus E2E rather than a single oversized driver test.

## CI design

CI should remain intentionally small and precise.

The normal pull-request lane should prefer a few high-signal jobs rather than dozens of partially overlapping checks:

1. **contracts/docs**: project constitution and static repository invariants,
2. **go**: `gofmt`, `go test ./...`, `go vet ./...`,
3. **package smoke**: build the `.deb`, install it in a clean supported environment and verify required runtime payloads.

For the Debian reporter slice, package smoke must verify the fixed amd64 reporter executable exists at `/usr/lib/aegispxe/reporters/aegispxe-reporter-amd64` because the PXE boot endpoint serves that exact packaged path.

Long-running VM provisioning E2E belongs in a dedicated gate and must not be duplicated by giant unit/integration suites. TPM hardware/virtualization behavior is specifically an E2E concern and must not be declared green merely because the Go TPM calls compile.

CI must not run the same logical test in multiple jobs merely to increase activity. Every job should have a distinct failure meaning.

When a CI failure occurs, its job and test name should make the failed contract obvious without reading hundreds of unrelated log lines.

## Test maintenance

A test that blocks a correct architectural change because it asserts old implementation details should be rewritten or removed, not preserved by compatibility code.

When production behavior is intentionally replaced, update the smallest relevant contract tests and let E2E verify the complete path.

Test readability is part of maintainability. A growing test file should be split by domain contract before it becomes a historical dumping ground.
