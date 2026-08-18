# AegisPXE Agent Instructions

These rules apply to humans and coding agents working in this repository.

## Mission

Build AegisPXE as a security-first, headless provisioning platform for servers and VMs. PXE is transport. Provisioning correctness is the product.

## Non-negotiable rules

1. No interactive client-side OS/profile menu in the core path.
2. No lifecycle progress inferred from seed access, elapsed time or unrelated traffic.
3. Every meaningful state mutation must have an auditable event.
4. Operational I/O/state/security paths require structured logging.
5. Pure deterministic conversions should remain log-free and covered by tests.
6. No secrets in logs, URLs, public boot configuration or normal metadata.
7. No arbitrary shell/command API in the privileged helper.
8. Profiles contain desired state, never OS-installer syntax or arbitrary shell.
9. OS-specific runtime semantics belong to the owning driver.
10. No driver is production-ready without telemetry, validation and E2E coverage.
11. Installation specs are immutable once armed.
12. Unsupported capabilities fail preflight; never silently downgrade.
13. Documentation changes are mandatory when contracts change.
14. Prefer supported/LTS OS targets where possible.
15. Keep Go dependencies small and deliberate.

## Required reading before architectural changes

- `ARCHITECTURE.md`
- `OBSERVABILITY.md`
- `SECURITY.md`
- `LIFECYCLE.md`
- `DRIVER_CONTRACT.md`
- `TESTING.md`

## Implementation order

Do not jump ahead of the current vertical slice.

Initial order:

1. headless machine discovery and registration,
2. Debian 13 Standard,
3. Debian 13 Encrypted,
4. Ubuntu 24.04 LTS,
5. Ubuntu 26.04 LTS,
6. CentOS/Kickstart.

Do not create speculative skeleton drivers for later milestones.

## Observability review

For each new operational function ask:

- What operation is this?
- Which `request_id`, `machine_id`, `installation_id` or `job_id` applies?
- What success record is useful?
- What failure code is stable?
- Could any field contain a secret?
- Does a state mutation need an event in the same transaction?

Do not add entry/exit logs to pure helpers just to increase log coverage.

## State-machine review

Before adding or changing a state transition:

- identify the authoritative event source,
- define allowed predecessor state(s),
- define idempotency/replay behavior,
- define failure behavior,
- update `LIFECYCLE.md` if the public contract changes,
- add tests for accepted and rejected transitions.

## Driver review

A driver must document native installer evidence for every lifecycle event it emits. Shared helpers may format data, but must not erase OS-specific runtime distinctions.

## Security review

Any change that expands privileged helper permissions, credential scopes, secret handling, trust boundaries or artifact trust requires explicit security review and usually an ADR.

## Done means diagnosable

A feature is not complete until a failed execution produces enough correlated logs/events/error codes to diagnose the failure from the AegisPXE Studio or API without first SSHing into the AegisPXE host.