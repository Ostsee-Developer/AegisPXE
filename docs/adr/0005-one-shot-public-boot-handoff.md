# ADR 0005: One-shot destructive public boot handoff

- Status: Accepted
- Date: 2026-08-19
- Supersedes: the assignment-consumption timing described in ADR 0003 sections 4 and 6

## Context

The first real Debian 13 unattended installation completed successfully, including GRUB installation and the AegisPXE late hook. On the following reboot the machine entered PXE again while the same assignment was still `armed`, so AegisPXE served the destructive installer a second time.

ADR 0003 originally kept public boot reads retryable and deferred assignment consumption until a future authenticated `INSTALLER_STARTED` event. That is cryptographically clean but operationally unsafe before installer telemetry exists: a completed machine can be reinstalled repeatedly simply because network boot remains ahead of the local disk.

AegisPXE must not infer `INSTALLER_STARTED`, `SUCCESS`, validation or any other runtime lifecycle state from HTTP reads. It also must not leave a destructive provisioning lease indefinitely reusable.

## Decision

An armed installation assignment is a one-shot destructive boot lease.

The boot script, kernel and initrd reads remain retryable and non-consuming. The rendered `preseed.cfg` is intentionally the final network object fetched by the iPXE script immediately before `boot`. Before AegisPXE returns that Preseed successfully, it atomically transitions the assignment from `armed` to `consumed` and records `INSTALLATION_ASSIGNMENT_CONSUMED`.

This consumption means only:

> AegisPXE committed the final public boot handoff for this destructive provisioning attempt.

It does **not** mean:

- `INSTALLER_STARTED`,
- disk preparation started,
- OS installation succeeded,
- hardening succeeded,
- validation succeeded,
- the machine is cryptographically trusted.

After consumption there is no active armed assignment. A machine whose policy remains `provision` therefore receives the normal `installation_not_armed` local-boot decision on its next PXE discovery and the firmware can continue to the installed disk.

If the Preseed handoff is consumed but the client subsequently fails to boot or install, an operator must create and arm a new installation attempt. AegisPXE deliberately prefers a recoverable stopped provisioning attempt over an automatic destructive reinstall loop.

## Security

This change does not release lifecycle credentials or other secrets. Public boot authorization still requires operator approval plus an armed assignment, and secret release remains blocked without cryptographic boot trust.

Assignment consumption is scheduling state, not authenticated installer telemetry. Future telemetry will append authenticated runtime lifecycle events independently and will not retroactively reinterpret `INSTALLATION_ASSIGNMENT_CONSUMED` as installer success.

## Consequences

- A completed Debian installation does not automatically reinstall on the next network-first reboot.
- Boot script and artifact transport remain retryable until the final Preseed handoff.
- A network failure after handoff consumption requires an explicit new provisioning attempt rather than an implicit retry.
- Dashboard assignment state can move to `CONSUMED` before any authenticated installer lifecycle status exists; the UI must present those concepts separately.
- Cryptographic boot trust and authenticated installer telemetry remain required for the 0.1.0 release-candidate boundary.
