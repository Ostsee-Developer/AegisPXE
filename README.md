# AegisPXE

AegisPXE is a security-first, headless provisioning platform for servers and virtual machines.

It is not an interactive PXE menu and not a general-purpose live-boot catalog. AegisPXE centrally discovers machines, assigns immutable installation specifications, boots the native unattended installer for the selected operating system, records authoritative lifecycle events and logs, and validates the resulting system.

## Core principles

1. **Headless clients**: machines never choose an operating system or profile locally.
2. **Server-authoritative provisioning**: every boot decision is made by AegisPXE.
3. **No inferred progress**: installation status changes only because an authenticated component reported a real event.
4. **Observability is mandatory**: operational I/O, state transitions, security decisions and driver operations use structured logging and correlation identifiers.
5. **Failure is data**: errors are preserved with stable error codes, context and installation-scoped logs.
6. **Immutable installation specs**: a running installation cannot change because a profile or driver was edited later.
7. **OS-native drivers**: Debian, Ubuntu and CentOS own their installer-specific behavior. Cross-driver runtime shortcuts are forbidden.
8. **Security by construction**: least privilege, scoped tokens, verified artifacts, secret redaction and a minimal privileged helper are architectural requirements.
9. **No silent state changes**: every meaningful state mutation produces an auditable event.
10. **E2E before expansion**: one complete vertical provisioning path must be reliable before another operating system or major feature is added.
11. **Small tests, sharp contracts**: one test proves one contract; complete workflows belong in E2E rather than oversized unit tests.
12. **Package what we test**: executable milestones ship and are validated as a native Debian package, not only from a source checkout.

## Initial target platform

AegisPXE will be implemented in Go and initially target:

- Debian 13
- Ubuntu Server 24.04 LTS
- Ubuntu Server 26.04 LTS
- CentOS via Kickstart after the Debian and Ubuntu paths are proven stable

The first engineering milestone is intentionally smaller: **headless machine discovery and registration**, with complete structured logging and lifecycle events. Debian provisioning comes only after discovery is reproducible and observable.

## Project constitution

These documents are normative for implementation and review:

- [Architecture](ARCHITECTURE.md)
- [Observability](OBSERVABILITY.md)
- [Security](SECURITY.md)
- [Installation lifecycle](LIFECYCLE.md)
- [OS driver contract](DRIVER_CONTRACT.md)
- [Testing strategy](TESTING.md)
- [Debian packaging contract](PACKAGING.md)
- [Contribution rules](CONTRIBUTING.md)
- [Coding-agent instructions](AGENTS.md)
- [Profile schema principles](docs/PROFILE_SCHEMA.md)
- [Error-code contract](docs/ERROR_CODES.md)
- [Machine discovery contract](docs/MACHINE_DISCOVERY.md)
- [Roadmap](docs/ROADMAP.md)
- [Architecture decisions](docs/adr/)

The Project Constitution workflow verifies that foundational contracts remain present and documented.

## Current milestone

**0.0.x: Foundation and headless discovery.**

The next implementation step is the Machine domain and repeatable discovery loop. No OS installer code should be added until machine identity, pending-state registration, logging, events and non-provisioning exit behavior are proven stable.
