# Testing Strategy

AegisPXE is built vertical-slice first. Tests must prove both successful provisioning and useful failure observability without becoming a second implementation of the product.

## Core test rule

**One test should prove one contract.**

Tests should be small, explicit and easy to diagnose. We do not optimize for maximum assertion count or maximum coverage per test function.

There is deliberately no arbitrary line-count limit. Moving complexity into helpers merely to satisfy a number does not make a test simpler.

## What tests must not become

Avoid:

- reimplementing production algorithms inside tests,
- giant end-to-end behavior simulations in unit tests,
- asserting complete installer documents when only one contract changed,
- broad golden files for unstable or incidental output,
- duplicated assertions across unit, integration and E2E layers,
- preserving superseded implementation details as compatibility theater.

Prefer focused inputs/outcomes, small fixtures, semantic assertions and dedicated security/state/observability contract tests.

## Test layers

### Unit tests

Pure domain logic, validation, state transitions, redaction and deterministic driver rendering.

Unit tests must not require root, network access or a VM.

Examples:

- an InstallationSpec rejects a non-canonical artifact digest,
- Secure Boot policy `required` allows only normalized state `enabled`,
- incomplete UEFI Secure Boot evidence becomes `unknown` and therefore fails closed under required policy,
- a Secure Boot asset manifest rejects an unexpected release/commit/file set or tampered content,
- a Debian driver-v2 spec requires the signed Debian shim artifact,
- a legacy driver-v1 spec remains inspectable but cannot be rendered as the current boot contract,
- changing a signed telemetry path/body/timestamp invalidates its MAC,
- a secret field is redacted.

### Integration tests

Database transactions, event persistence, machine discovery, API authentication, helper boundaries, artifact metadata handling and driver/server integration.

Integration tests use temporary isolated state and must be repeatable.

Examples:

- discovery atomically persists Machine identity plus normalized Secure Boot evidence,
- required Secure Boot policy blocks an armed Machine before provisioning when state is disabled/unknown/SetupMode/unsupported,
- direct installation-material URLs cannot bypass that policy,
- an enabled UEFI Machine receives the current Debian driver-v2 boot script,
- that script keeps native Debian `initrd.gz` + Preseed and configures the pinned Debian shim without reporter/initramfs injection,
- InstallationSpec creation atomically persists immutable state plus its audit record,
- pending TPM trust cannot release a lifecycle credential in isolated reporter protocol tests.

### E2E tests

Real boot/install/reboot flows in disposable VMs. E2E is a release gate, not a manual bonus step.

Unit and integration tests do not reproduce firmware, shim, kernel or TPM virtualization behavior. Those properties are verified in a real VM.

E2E assertions remain outcome-oriented:

- expected Machine/platform-security state,
- expected boot decision,
- successful unattended installation,
- successful local reboot,
- expected SSH access and installed-state checks,
- useful logs/error code for deliberately failed security states.

## Stabilized Debian 13 baseline

The dev.21 baseline is the known-good production installer transport:

```text
verified Debian kernel
-> native Debian initrd.gz
-> preseed.cfg
-> Debian Installer
-> installed OS
-> deterministic local reboot
```

This path has completed the real Proxmox E2E, including successful reboot and SSH-key login. Regression tests must prevent reporter overlays, generated initramfs images, magic initrds, multi-initrd experiments or server-side repacking from re-entering this path accidentally.

Secure Boot work may add signed executable-verification components around this transport, but must not modify the native Debian initrd/Preseed delivery contract without a new explicit architecture decision and E2E proof.

## Secure Boot gate for dev.22

Secure Boot is not complete because package or Go tests pass. Before dev.22 may be merged/released, a real Proxmox OVMF fixture must prove the positive and negative paths in `docs/TESTING_SECUREBOOT.md`.

### Positive fixture

1. q35/OVMF UEFI VM.
2. Secure Boot enabled, SetupMode disabled, required Microsoft third-party UEFI CA available.
3. DHCP UEFI filename is `ipxe-shim.efi`.
4. Firmware starts the official signed iPXE shim and its matching signed second stage.
5. AegisPXE persists `secure_boot_state=enabled`.
6. A fresh Debian driver-v2 InstallationSpec pins `linux`, native `initrd.gz` and Debian `bootnetx64.efi` from one verified installer provenance.
7. Installation is armed and completes unattended.
8. Secure Boot remains enabled across reboot.
9. Deterministic local boot reaches installed Debian.
10. SSH-key login succeeds.
11. Logs correlate the signed asset validation, Machine state and provisioning decision.

### Negative fixtures

At minimum prove:

- Secure Boot disabled -> no destructive installation material under `required`, reason `secure_boot_required`, `SEC023_SECURE_BOOT_REQUIRED` logged.
- SetupMode active -> same fail-closed result.
- incomplete Secure Boot evidence -> normalized `unknown` and no required-policy provisioning.
- malformed Secure Boot evidence -> discovery rejection with `SEC024_SECURE_BOOT_EVIDENCE_INVALID`.
- stale/tampered TFTP second stage -> package helper restores package-owned validated bytes.
- tampered package-owned signed asset on a disposable server fixture -> server startup fails under `required` with `SEC025_SECURE_BOOT_ASSETS_INVALID`.

The iPXE Secure Boot variables are policy observations, not remote attestation. The E2E must actually keep firmware Secure Boot enabled and prove that the signed first-stage chain is the one being executed.

## Reporter/TPM status

The earlier Debian reporter/initramfs injection design is suspended after failing the real UEFI/vTPM E2E path. The production `.deb` intentionally does **not** contain the reporter executable and production PXE/Studio listener allowlists do not expose the dormant reporter runtime as a completed production feature.

TPM trust, challenge/proof and signed telemetry primitives may continue to have isolated unit/integration tests. A replacement reporter delivery mechanism must be designed around the stabilized Debian installer and pass a separate real UEFI/vTPM gate before it is re-enabled.

The previous requirement to inject a reporter executable into the Debian initrd is obsolete and must not be used as a dev.22 or RC gate.

## Release gate philosophy

A feature is not complete because unit tests pass. For provisioning/security features, completion means:

- deterministic contract tests,
- focused integration tests for state/boundaries,
- source formatting/tests/vet,
- race detector,
- vulnerability scan,
- package install/upgrade smoke where relevant,
- real E2E success path,
- real or integration failure path appropriate to the boundary,
- expected logs/events verified,
- documentation current.

## Failure tests

We deliberately test failures such as:

- invalid/missing artifact digest,
- signed boot asset mismatch,
- Secure Boot required but disabled/unknown/SetupMode,
- malformed UEFI evidence,
- invalid/expired lifecycle authentication,
- tampered signed reporter body/path/timestamp in isolated reporter tests,
- pending/revoked/wrong-machine TPM key,
- expired or mismatched boot-trust challenge,
- duplicate/replayed event,
- storage hook reports failure,
- first-boot validation fails,
- Machine is blocked while assignment exists,
- helper refuses an invalid privileged action.

Failure tests must assert behavior plus the relevant stable error code/log fields, not entire log streams.

## Observability assertions

Operational tests verify relevant records contain correlation identifiers and stable error codes.

Secure Boot tests verify bounded evidence fields such as:

- `secure_boot_policy`
- `secure_boot_state`
- Machine/Installation/Assignment IDs where applicable,
- signed asset digests where applicable,
- `SEC023`/`SEC024`/`SEC025` on failure.

Tests must also verify sensitive fixtures are redacted and never persisted in normal logs.

## Driver certification

A driver cannot be marked production-ready until all claimed capabilities are certified. The driver contract requires artifact resolution, deterministic boot/seed rendering, authenticated telemetry, first-boot behavior, requested-state validation and unattended E2E.

Where one capability is temporarily suspended, documentation must say so plainly instead of weakening the contract. The Debian reporter runtime is such a suspended capability today. Secure Boot certification is a separate executable-trust gate and does not imply reporter completion.

## CI design

The pull-request lane should remain high-signal:

1. **Project Constitution**: architecture/document invariants.
2. **Verify source**: `gofmt`, `go test ./...`, `go vet ./...`, binary/version check.
3. **Race detector**: `go test -race ./...`.
4. **Vulnerability scan**: official `govulncheck` with a patched Go toolchain.
5. **Package smoke**: build/install the `.deb`, service health, required payload and operational asset checks.

For dev.22 package smoke must verify:

- reporter executable is absent,
- Secure Boot manifest exists,
- signed `ipxe-shim.efi` and `ipxe.efi` exist,
- both contain signature tables,
- runtime health reports `secure_boot_policy=required` and valid package assets,
- TFTP copies match package-owned signed bytes,
- stale package-managed TFTP bytes are repaired,
- discovery accepts a syntactically valid enabled UEFI observation.

Long-running VM E2E remains a dedicated manual/automation gate and is not replaced by package smoke.

## Upgrade and RC matrix

Before `0.1.0-rc.1`, repeatedly verify:

- upgrade from the deployed dev line preserves `/etc/aegispxe/aegispxe.env`, operator identity material and SQLite state,
- schema migrations advance atomically,
- package-managed signed PXE assets refresh safely,
- at least several consecutive Secure Boot-enabled Debian installations complete,
- at least one negative platform-security run fails closed with useful logs,
- local boot remains deterministic after every successful install.

## Test maintenance

A test that blocks a correct architectural change because it asserts obsolete implementation details should be rewritten or removed, not preserved by compatibility code.

When production behavior is intentionally replaced, update the smallest relevant contract tests and let E2E verify the complete path.

Test readability is part of maintainability. A growing test file should be split by domain contract before it becomes a historical dumping ground.
