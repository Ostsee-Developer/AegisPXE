# Contributing to AegisPXE

AegisPXE prioritizes correctness, observability, security and maintainability over feature velocity.

## Before writing code

For a meaningful change, identify:

- affected domain,
- state transitions,
- trust boundary impact,
- required logs/events,
- error codes,
- tests,
- documentation impact.

If the change alters an architectural contract, add or update an ADR.

## Mandatory pull request checklist

A PR is not ready to merge until applicable items are satisfied:

- [ ] state changes are explicit and event-backed,
- [ ] operational paths use structured logging,
- [ ] correlation IDs are propagated,
- [ ] no secrets can enter normal logs,
- [ ] stable error codes exist for new failure modes,
- [ ] unit/integration tests cover behavior,
- [ ] provisioning behavior has an E2E plan or test,
- [ ] failure behavior is testable and observable,
- [ ] documentation is updated,
- [ ] no OS-specific runtime behavior leaked into shared generic helpers,
- [ ] privileged helper surface did not grow without security review.

## Architectural discipline

Do not add generic abstractions solely because future drivers might need them. Start with the vertical slice and extract shared pure behavior only when semantics are genuinely identical.

Do not place installer implementation syntax in profiles.

Do not infer lifecycle status.

Do not introduce interactive client boot menus into the core provisioning path.

Do not add arbitrary command execution to server/helper APIs.

## Logging discipline

Operational I/O and stateful functions log at boundaries. Pure transformations do not log merely to satisfy a logging quota.

Logs are structured and use project-standard fields from `OBSERVABILITY.md`.

## Error handling

Meaningful failures are returned or recorded with context. Silent suppression is forbidden.

Do not use `panic` for expected runtime failures. Panics are reserved for unrecoverable programmer/startup invariants where continuing would be unsafe.

## Go style

- use `gofmt`,
- keep interfaces small and consumer-driven,
- prefer explicit domain types over strings for state/policy enums,
- pass `context.Context` through I/O boundaries,
- use transactions for atomic state/event mutations,
- avoid global mutable state,
- prefer the standard library where practical,
- keep dependencies intentionally small.

A second implementation language requires an approved ADR.

## Documentation as code

Changes to architecture, lifecycle, observability, security, driver contracts, profile semantics or testing gates require the corresponding documentation change in the same PR.

A PR that changes the contract without changing its documentation should fail review even when tests pass.

## Review order

Reviewers should evaluate in this order:

1. correctness of state model,
2. security/trust boundaries,
3. observability/failure diagnosis,
4. OS-driver semantic correctness,
5. tests,
6. maintainability/style.

This prevents polished code from hiding an incorrect provisioning contract.