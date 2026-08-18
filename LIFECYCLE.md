# Installation Lifecycle

AegisPXE installation status is event-driven. The current status is a projection of accepted append-only events.

## Rule zero

**AegisPXE must never infer installation progress from unrelated activity.**

Examples of invalid inference:

- seed fetched -> installer started,
- kernel downloaded -> disk preparation,
- HTTP traffic -> profile applying,
- elapsed time -> OS installing.

Only an authenticated event for the installation may advance lifecycle state.

## Initial lifecycle

```text
CREATED
  -> QUEUED
  -> BOOTLOADER_CHECKIN
  -> INSTALLER_STARTED
  -> STORAGE_PREPARING
  -> OS_INSTALLING
  -> PROFILE_APPLYING
  -> FIRST_BOOT
  -> VALIDATING
  -> COMPLETED
```

A failure may be reported from any active stage:

```text
* -> FAILED
```

Administrative cancellation and expiry are terminal states distinct from runtime failure:

- `CANCELLED`
- `EXPIRED`

## Assignment and trust before lifecycle start

The immutable InstallationSpec exists before runtime lifecycle progress begins. An operator may then arm an Assignment that binds exactly one Machine to that InstallationSpec.

Assignment state is administrative/runtime control state, not inferred installer progress:

```text
unassigned
  -> armed
       -> consumed
       -> cancelled
```

Arming emits an auditable `INSTALLATION_ARMED` record. Cancellation emits `INSTALLATION_ASSIGNMENT_CANCELLED`.

An armed Assignment plus operator approval may make non-secret public boot material eligible. It does not authenticate the booting client and does not authorize release of lifecycle credentials.

Secret-bearing installer operations require cryptographic boot trust. The Assignment is consumed only when the server accepts an authenticated `INSTALLER_STARTED` event for the exact assigned Installation.

## Event shape

Every installation event contains at least:

- installation ID,
- monotonically increasing accepted sequence,
- event type,
- source/component identity,
- server receive timestamp,
- optional client timestamp,
- stable error code when applicable,
- bounded human-readable message,
- non-secret structured metadata.

## Authoritative sources

Different stages have explicit allowed sources.

Examples:

- `CREATED`: server/orchestrator,
- `QUEUED`: server/orchestrator,
- `BOOTLOADER_CHECKIN`: AegisPXE bootstrap/boot endpoint,
- `INSTALLER_STARTED`: authenticated installer integration,
- `STORAGE_PREPARING`: OS driver installer hook,
- `OS_INSTALLING`: OS driver installer hook,
- `PROFILE_APPLYING`: OS driver/runtime hook,
- `FIRST_BOOT`: installed OS first-boot agent/finalizer,
- `VALIDATING`: validation component,
- `COMPLETED`: validation component/server after required checks pass.

The state machine rejects events from sources not authorized for that event type.

## Monotonicity

Lifecycle state does not regress. Duplicate events are handled idempotently. Out-of-order events are either buffered by a deliberately documented mechanism or rejected; they must never silently rewrite history.

## Boot assignment consumption

The provisioning assignment remains armed through firmware retries, bootloader fetches, initrd/preseed construction and reads of non-secret boot material.

It is consumed only after the server accepts an authenticated `INSTALLER_STARTED` event for the currently assigned installation. Authentication must satisfy the provisioning trust contract; discovery identifiers alone are insufficient.

Consumption creates its own auditable state mutation.

## Failure model

`FAILED` records:

- failed stage,
- error code,
- reporting component,
- message,
- related log cursor/range when available.

The previous event history remains untouched.

A retry creates a new InstallationSpec unless the operation is explicitly an idempotent retry of the same stage. Reusing a failed mutable installation as a different desired state is forbidden.

## Machine policy after terminal state

After `COMPLETED`, `FAILED`, `CANCELLED` or `EXPIRED`, the active provisioning assignment is removed according to the transition's transaction. The default machine policy is then evaluated, normally resulting in local boot.

## Timeouts

Timeouts are explicit policy, not inferred progress. A timeout produces a defined error code/event and enough log context to diagnose what event was missing.

Examples:

- installer never reported `INSTALLER_STARTED`,
- no heartbeat/log activity after `OS_INSTALLING`,
- first boot did not report within configured deadline.

## UI projection

The Studio timeline displays accepted events in sequence order. It may show elapsed time, assignment state and trust gates, but it must visually distinguish those from authoritative lifecycle state and must never manufacture completed stages.
