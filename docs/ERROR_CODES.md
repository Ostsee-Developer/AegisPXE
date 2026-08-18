# Error Code Contract

AegisPXE errors exposed to operators, lifecycle events or APIs use stable machine-readable codes plus a human-readable message.

## Format

```text
<namespace><number>_<SYMBOLIC_NAME>
```

Examples:

- `PXE001_DISCOVERY_RATE_LIMITED`
- `MAC001_MACHINE_IDENTITY_CONFLICT`
- `ART002_ARTIFACT_HASH_MISMATCH`
- `DRV001_DRIVER_SPEC_UNSUPPORTED`
- `INS001_INSTALLATION_SPEC_INVALID`
- `INS003_INSTALLER_STAGE_FAILED`
- `SEC002_INVALID_INSTALLATION_TOKEN`
- `VAL001_VALIDATION_FAILED`

## Namespaces

- `PXE`: bootstrap, discovery and boot decision transport,
- `MAC`: machine identity/discovery,
- `ART`: artifact resolution/download/integrity,
- `DRV`: driver compile/render/capability,
- `INS`: installation specification, native installer and first-boot runtime,
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

| Code | Meaning |
| --- | --- |
| `PXE001_DISCOVERY_RATE_LIMITED` | A discovery source exceeded the bounded request window and was refused before state mutation. |
| `MAC001_MACHINE_IDENTITY_CONFLICT` | Two trusted identity observations resolve to different stored machines. |
| `MAC002_MACHINE_IDENTITY_INVALID` | The supplied machine observation contains no usable identity or an invalid identifier. |
| `MAC003_MACHINE_NOT_FOUND` | A requested machine ID does not exist. |
| `MAC004_MACHINE_POLICY_INVALID` | A requested machine policy or policy mutation request is invalid. |
| `ART001_ARTIFACT_TRUST_FAILED` | Signed release metadata, its trust anchor or a verified checksum manifest could not establish trusted artifact provenance. |
| `ART002_ARTIFACT_HASH_MISMATCH` | Downloaded artifact content or an installer checksum manifest does not match its trusted SHA-256 identity. |
| `ART003_ARTIFACT_FETCH_FAILED` | Required artifact or release metadata could not be fetched within the bounded transport contract. |
| `DRV001_DRIVER_SPEC_UNSUPPORTED` | The pinned InstallationSpec requests state that the selected driver contract cannot implement without downgrade or reinterpretation. |
| `DRV002_DRIVER_RENDER_FAILED` | A driver accepted the InstallationSpec but could not deterministically render the required native material. |
| `INS001_INSTALLATION_SPEC_INVALID` | An InstallationSpec violates the immutable installation contract or attempts to supply server-owned identity metadata. |
| `INS002_INSTALLATION_NOT_FOUND` | A requested InstallationSpec does not exist. |
| `SYS001_STORAGE_FAILURE` | The persistent store could not complete an operation safely. |

The Go registry in `internal/fault` is authoritative for allocated codes. Documentation and source must change together when a new operator-visible code is introduced.
