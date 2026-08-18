# AegisPXE Architecture

## Purpose

AegisPXE is a security-first, headless provisioning platform. PXE is a transport mechanism, not the product. The product is a centrally controlled provisioning workflow that discovers machines, assigns immutable installation specifications, invokes OS-native unattended installers, records authoritative events and logs, and validates the final system.

## Non-goals

The core must not become:

- an interactive boot menu,
- a live ISO catalog,
- a rescue-tool launcher,
- a generic remote shell,
- a collection of OS-specific shell snippets sharing hidden state.

Features outside provisioning belong in separate plugins or projects and must not enter the critical provisioning path.

## Core domains

### Machine

A physical server or virtual machine known to AegisPXE.

A machine has stable identity records and a small authoritative policy:

- `pending`: discovered but not approved/configured,
- `local`: no provisioning assignment; leave PXE and continue local boot,
- `provision`: an installation assignment may be booted,
- `blocked`: AegisPXE must refuse provisioning.

A machine must never select an OS or profile locally.

### InstallationSpec

An immutable snapshot describing one provisioning attempt. It contains, at minimum:

- installation ID,
- machine ID,
- OS driver ID and driver version,
- OS release,
- immutable profile revision,
- artifact identities and digests,
- storage/security settings,
- lifecycle credential identity,
- creation metadata.

Once armed, an InstallationSpec is never modified. A changed profile creates a new revision and a new installation.

### Profile

A profile is OS-neutral desired state. It describes intent, not implementation commands.

Profiles may express identity, SSH policy, packages, storage intent, encryption, firewall intent, hardening and validation requirements.

Profiles must not contain:

- arbitrary shell,
- cloud-init syntax,
- preseed syntax,
- Kickstart syntax,
- systemctl commands,
- installer-specific hooks.

### Driver

An OS driver owns all installer-specific behavior for a supported target. Debian, Ubuntu and CentOS are intentionally separate implementation domains.

A driver translates an InstallationSpec into native boot, seed, telemetry, first-boot and validation material.

No shared helper may hide different runtime semantics between OS families.

### Artifact

A verified boot/install object such as a kernel, initrd or installer image. Artifact identity includes origin, version and digest. Unverified artifacts must never be selected for provisioning.

### Lifecycle

An append-only event stream for an installation. Current status is a projection of accepted events, never a guess derived from unrelated activity.

### Observability

Structured logs, installation events and audit records are first-class domain output. Observability is not a later integration.

## Process boundaries

Initial process model:

- `aegispxe-server`: unprivileged API, UI and orchestration process.
- `aegispxe-worker`: unprivileged artifact and background job worker where separation is useful.
- `aegispxe-helper`: minimal privileged helper with a typed allowlisted API.
- `aegispxe-cli`: administrative CLI using the same domain/API contracts as the UI.

The privileged helper must not expose arbitrary command execution or a generic shell primitive.

## Boot flow

The client boot path is intentionally boring:

1. Firmware starts network boot.
2. AegisPXE bootstrap identifies/checks in the machine.
3. Server resolves machine identity and policy.
4. Unknown machine: record discovery, return a non-provisioning decision and leave PXE.
5. Pending/local machine: leave PXE.
6. Blocked machine: refuse AegisPXE provisioning.
7. Provision machine with an armed installation: return exactly one boot specification.
8. Installer boots and reports authenticated lifecycle events.
9. Assignment is consumed only after an authenticated `INSTALLER_STARTED` event.
10. First boot applies runtime-only desired state and validates the system.
11. Successful validation emits `COMPLETED`; the machine returns to local boot policy.

There is no graphical or textual OS-selection menu on the client.

## State mutation rule

Every meaningful state mutation must produce an auditable record in the same logical operation. Database transactions must be used where state and event persistence must be atomic.

Silent state changes are defects.

## Dependency rule

Domain packages must not import UI packages or OS driver implementations. Drivers may depend on stable domain contracts, not the reverse.

A second implementation language requires an ADR showing a concrete requirement that Go cannot reasonably satisfy.

## Initial package direction

```text
cmd/
  aegispxe-server/
  aegispxe-helper/
  aegispxe-cli/
internal/
  machine/
  installation/
  profile/
  lifecycle/
  observability/
  artifact/
  security/
  driver/
  drivers/
    debian13/
    ubuntu2404/
    ubuntu2604/
    centos/
web/
docs/
  adr/
test/
  integration/
  e2e/
```

This layout is a direction, not permission to create empty abstractions before the first vertical slice needs them.

## Vertical-slice rule

Expansion is ordered:

1. machine discovery,
2. Debian 13 Standard,
3. Debian 13 Encrypted,
4. Ubuntu 24.04 LTS,
5. Ubuntu 26.04 LTS,
6. CentOS/Kickstart.

A later slice must not begin until the preceding slice has repeatable E2E coverage and useful failure telemetry.