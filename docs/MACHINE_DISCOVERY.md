# Headless Machine Discovery Contract

Machine discovery is the first technical vertical slice of AegisPXE. It must be stable before any OS installer is implemented.

## Goal

When an unknown server or VM reaches the AegisPXE boot path for the first time, AegisPXE records it as a new pending machine, emits structured logs/events, exposes it in the Studio/API for administrator configuration, and then returns a non-provisioning decision.

The client never sees an AegisPXE OS/profile menu.

## Discovery input

The boot transport should submit only information it can reliably observe. Candidate identity observations include:

- MAC address(es),
- SMBIOS/system UUID where available,
- architecture,
- firmware mode,
- Secure Boot observation where reliably available,
- vendor/model,
- serial number where available.

Not all fields are trusted or always present. They are observations used by the machine identity resolver, not authentication credentials.

## Stable machine identity

AegisPXE assigns its own opaque `machine_id`.

Firmware identifiers are stored as observations and indexed according to explicit identity rules. The database must distinguish:

- stable AegisPXE identity,
- current network identifiers,
- historical observations,
- identity conflicts.

A changed NIC must not silently become a different machine when stronger identity evidence links it to an existing machine. Conversely, a duplicated/cloned SMBIOS UUID must not silently merge two machines.

Identity ambiguity produces an explicit conflict state/error for operator resolution.

## Unknown machine flow

1. Receive boot check-in with a `request_id`.
2. Validate bounded discovery payload.
3. Resolve identity observations.
4. If no machine matches, create one machine in `pending` state.
5. Append machine-discovered/audit event atomically with creation.
6. Record `first_seen` and `last_seen`.
7. Emit structured operational log with request and machine IDs.
8. Return a non-provisioning/local-exit boot decision.

No installation token, seed or profile material is issued.

## Known pending machine flow

A repeated boot:

1. resolves to the same `machine_id`,
2. updates `last_seen`,
3. records a bounded check-in event/log as designed,
4. does not create a duplicate machine,
5. returns the same non-provisioning decision.

The event model should avoid turning every low-level asset request into audit noise. A machine boot/check-in is meaningful; every file fetch is not.

## Policy decisions

Initial machine policy values:

- `pending`: discovered, awaiting administrator configuration,
- `local`: explicitly no provisioning,
- `provision`: may boot its current installation assignment,
- `blocked`: refuse AegisPXE provisioning.

The policy evaluator returns a typed decision and logs the decision reason.

Before the InstallationSpec domain exists, `provision` must still return a non-provisioning decision. Policy alone is never sufficient evidence that an installer may boot.

## Security

Discovery endpoints are intentionally low privilege.

Discovery may create/update bounded machine observation records but cannot:

- approve a machine,
- choose a profile,
- create an InstallationSpec,
- fetch another installation's seed,
- reveal secrets,
- mutate administrative policy.

Rate limits, input bounds and conflict handling are required before exposing discovery on an untrusted network.

Administrative Studio mutations require operator authentication/authorization. Until that boundary exists, the discovery Studio remains read-only rather than exposing unauthenticated approve/block/policy controls.

## Observability

At minimum, a first discovery should yield correlated records equivalent to:

```text
request accepted
identity observations validated
machine identity = new
machine created: pending
MACHINE_DISCOVERED event appended
boot decision = local/non-provisioning
request completed
```

A repeated check-in should make it obvious that the existing machine was resolved rather than created again.

## Failure cases to test

- malformed MAC,
- oversized payload,
- duplicate/ambiguous system UUID,
- database failure during machine + event transaction,
- repeated identical check-ins,
- concurrent first check-ins for the same identity,
- policy `blocked`,
- discovery rate limit,
- unknown identity with only weak MAC evidence,
- logging/redaction behavior.

## Studio requirement

The first UI/API view emphasizes operational clarity over visual complexity. A pending machine shows:

- machine ID,
- first/last seen,
- identity observations,
- pending status,
- any identity warning/conflict where available,
- recent discovery/boot events.

Operator actions to configure/approve a machine are introduced only with the administrative authentication/authorization boundary. The 0.0.3 Studio is therefore deliberately read-only.

No installer choices are rendered on the client itself.
