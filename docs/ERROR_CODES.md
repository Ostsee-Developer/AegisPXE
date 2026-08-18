# Error Code Contract

AegisPXE errors exposed to operators, lifecycle events or APIs use stable machine-readable codes plus a human-readable message.

## Format

```text
<namespace><number>_<SYMBOLIC_NAME>
```

Examples:

- `PXE001_BOOT_DECISION_FAILED`
- `MAC001_MACHINE_IDENTITY_CONFLICT`
- `ART002_ARTIFACT_HASH_MISMATCH`
- `DRV001_DRIVER_RENDER_FAILED`
- `INS003_INSTALLER_STAGE_FAILED`
- `SEC002_INVALID_INSTALLATION_TOKEN`
- `VAL001_VALIDATION_FAILED`

## Namespaces

- `PXE`: bootstrap, discovery and boot decision transport,
- `MAC`: machine identity/discovery,
- `ART`: artifact resolution/download/integrity,
- `DRV`: driver compile/render/capability,
- `INS`: native installer and first-boot runtime,
- `SEC`: authentication, authorization, trust and secret handling,
- `VAL`: desired-state validation,
- `SYS`: internal service/storage/platform failures.

## Rules

1. A released code is an API/operations contract.
2. Human-readable messages may improve without changing the code.
3. Codes are not reused for unrelated failures.
4. Sensitive values never appear in the code or attached message.
5. An error event records the stage/component and relevant correlation IDs.
6. A new operational failure mode normally receives a code when operators may need to distinguish or automate around it.

## Registry

Allocated with the first executable machine-domain slice:

| Code | Meaning |
| --- | --- |
| `MAC001_MACHINE_IDENTITY_CONFLICT` | Two trusted identity observations resolve to different stored machines. |
| `MAC002_MACHINE_IDENTITY_INVALID` | The supplied machine observation contains no usable identity or an invalid identifier. |
| `MAC003_MACHINE_NOT_FOUND` | A requested machine ID does not exist. |
| `MAC004_MACHINE_POLICY_INVALID` | A requested machine policy or policy mutation request is invalid. |
| `SYS001_STORAGE_FAILURE` | The persistent store could not complete an operation safely. |

The Go registry in `internal/fault` is authoritative for allocated codes. Documentation and source must change together when a new operator-visible code is introduced.
