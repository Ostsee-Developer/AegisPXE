## What changed

Describe the behavior and why it belongs in the current vertical slice.

## State and lifecycle

- [ ] I identified every meaningful state mutation introduced or changed.
- [ ] State mutations create/retain the required append-only event/audit record.
- [ ] No lifecycle progress is inferred from unrelated activity.
- [ ] Replay/idempotency behavior is covered where applicable.

## Observability

- [ ] Operational I/O/state/security paths emit structured logs.
- [ ] Correlation identifiers are propagated where applicable.
- [ ] New operator-relevant failures have stable error codes.
- [ ] Failure-path tests verify useful logs/events, not only returned errors.
- [ ] Pure deterministic helpers were not given noisy entry/exit logging merely for coverage.

## Security

- [ ] No secret can enter normal logs, URLs or public boot configuration.
- [ ] Authorization/trust boundaries were reviewed.
- [ ] The privileged helper surface did not grow, or the change has explicit security review/ADR.
- [ ] Unsupported security/capability combinations fail preflight rather than silently downgrade.

## Driver boundaries

- [ ] OS-specific installer/runtime behavior remains inside the owning driver.
- [ ] Profiles contain desired state, not installer syntax or arbitrary shell.
- [ ] Driver telemetry and validation remain part of the feature contract.

## Verification

- [ ] Tests are small and contract-focused rather than reproducing production logic.
- [ ] One test does not attempt to validate an entire subsystem.
- [ ] Unit tests pass.
- [ ] Integration tests pass where applicable.
- [ ] The E2E plan/test is updated for provisioning behavior.
- [ ] At least one relevant failure mode is covered.
- [ ] CI jobs have distinct failure meanings and do not duplicate the same logical test unnecessarily.

## Packaging

- [ ] Package-owned files/permissions/service behavior are updated when applicable.
- [ ] The generated `.deb` remains installable from a clean supported environment once executable milestones begin.
- [ ] Package installation does not silently invent network/provisioning configuration.

## Documentation

- [ ] Architecture/security/lifecycle/observability/driver/testing/packaging docs are updated when their contract changed.
- [ ] An ADR is added or updated for an architectural decision.
- [ ] User/operator documentation is updated where applicable.

## Notes for reviewers

Call out the highest-risk assumption, native installer hook, trust-boundary change, observability decision or test that protects the most important contract in this PR.
