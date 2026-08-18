# AegisPXE Roadmap

The roadmap is deliberately vertical. A milestone is complete only when its success and failure paths are observable and repeatable.

## 0.0.x: Foundation and discovery

### 0.0.1 Project constitution

- architecture contract,
- observability contract,
- security model,
- lifecycle model,
- driver contract,
- testing contract,
- Debian packaging contract,
- ADR process,
- contribution/agent rules.

### 0.0.2 Machine domain

- stable machine IDs,
- identity observations,
- `pending/local/provision/blocked` policy,
- append-only machine/audit events,
- structured logging,
- persistence,
- first executable `aegispxe` Go service,
- native `.deb` build from CI,
- clean package install test on a fresh supported Debian environment.

The `.deb` is part of the implementation path from this milestone onward. A source-tree-only success does not satisfy the gate.

### 0.0.3 Headless PXE discovery E2E

- unknown VM checks in,
- machine appears in Studio/API as pending,
- repeat boot does not duplicate the machine,
- no provisioning material is exposed,
- client leaves the provisioning path,
- complete correlated logs available,
- E2E runs against the packaged installation rather than an ad-hoc source checkout.

Gate: repeated discovery E2E, target 20 clean repetitions plus identity-conflict/failure tests.

## 0.1.0: Debian 13 Standard

- Debian 13 driver,
- verified artifacts,
- immutable InstallationSpec,
- unattended installer configuration,
- real installer lifecycle telemetry,
- installer log ingestion,
- first-boot finalizer,
- desired-state validation,
- headless local boot after terminal state.

Gate: 10 consecutive unattended E2E successes and useful telemetry for intentionally failed runs, using a clean AegisPXE `.deb` installation.

## 0.2.0: Debian 13 Encrypted

- encryption capability,
- recovery secret storage,
- TPM2 capability/preflight where supported,
- encryption/TPM validation.

Only begins after Debian Standard is stable.

## 0.3.0: Ubuntu Server 24.04 LTS

Separate Ubuntu driver with native Subiquity/Autoinstall/NoCloud semantics and native telemetry.

No Debian runtime implementation is reused where installer semantics differ.

## 0.4.0: Ubuntu Server 26.04 LTS

Supported through its own tested target contract. Pure shared Ubuntu helpers may be extracted only after both Ubuntu targets prove the semantics are actually identical.

## 0.5.0: CentOS/Kickstart

Choose the concrete supported CentOS target/release policy at this milestone and record it in an ADR before implementation.

## Later milestones

Only after all core drivers are reliable:

- richer Provisioning Studio,
- public automation API,
- CLI workflows,
- external integrations,
- scaling/performance work,
- optional plugins outside the provisioning critical path.

Interactive live-boot catalogs remain outside the core roadmap.
