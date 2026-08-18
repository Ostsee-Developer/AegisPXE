# ADR 0001: Headless, server-authoritative provisioning core

- Status: Accepted
- Date: 2026-08-18

## Context

The predecessor project accumulated interactive GRUB menus, live-boot catalog behavior, client policies, one-shot provisioning and OS-specific installer logic in overlapping paths. This increased coupling and made failures hard to localize.

AegisPXE exists to provide reliable unattended provisioning of servers and virtual machines.

## Decision

AegisPXE will use a headless client boot model.

Clients do not display an AegisPXE OS/profile menu. The server resolves machine identity and returns one of a small set of authoritative decisions:

- discover/register and leave PXE,
- local boot/non-provisioning,
- refuse because blocked,
- boot exactly one armed installation.

Interactive live-boot/catalog behavior is outside the core provisioning product.

## Consequences

### Positive

- smaller bootloader surface,
- deterministic behavior,
- fewer conflicting client states,
- easier security review,
- easier automated E2E testing,
- central auditability.

### Negative

- administrators cannot choose an OS from the physical console,
- rescue/live-boot features require a separate future mechanism if ever needed.

## Guardrail

A proposal to add an interactive client menu to the provisioning core requires a superseding ADR and must demonstrate why centralized control cannot satisfy the requirement.