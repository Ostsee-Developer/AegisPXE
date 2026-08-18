# Testing Strategy

AegisPXE tests prove contracts at the smallest useful boundary.

## Rule

**One test should prove one contract.**

Do not duplicate domain logic in shell tests or construct long synthetic workflows where a focused Go test can prove the behavior directly.

## Unit and domain tests

Use unit/domain tests for pure rules and immutable value behavior, including:

- machine identifier normalization and validation,
- boot-policy decisions,
- InstallationSpec validation and snapshot ownership,
- lifecycle transition rules,
- driver render logic once a driver exists.

## Store and API integration tests

Use integration tests where persistence, transactions or HTTP boundaries are the behavior under test, including:

- discovery identity resolution,
- atomic audit-event persistence,
- immutable InstallationSpec create/read round trips,
- API decoding and bounded failure behavior.

## End-to-end tests

Complete provisioning workflows belong in real E2E. CI must not pretend that twenty HTTP calls are equivalent to twenty real firmware/PXE boots.

The 0.0.3 discovery milestone uses packaged installation plus disposable VM PXE runs for its real repeatability gate. The Debian 13 milestone will use disposable VMs for installer lifecycle E2E once the driver and immutable assignment path exist.

## Normal CI

Normal CI stays intentionally small:

- `gofmt`,
- `go test ./...`,
- `go vet ./...`,
- canonical binary build/version check,
- Debian package smoke installation and health/stage-1 checks,
- one real discovery request through the installed package.

Long-running VM E2E is a separate gate and must not be expanded into a shell loop inside normal CI.
