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
- `INS003_INSTALLATION_ASSIGNMENT_INVALID`
- `SEC001_CRYPTOGRAPHIC_BOOT_TRUST_REQUIRED`
- `VAL001_FIRST_BOOT_VALIDATION_FAILED`

## Namespaces

- `PXE`: bootstrap, discovery and boot decision transport,
- `MAC`: machine identity/discovery,
- `ART`: artifact resolution/download/integrity,
- `DRV`: driver compile/render/capability,
- `INS`: installation specification, assignment, native installer and first-boot runtime,
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
| `MAC005_MACHINE_DELETE_CONFLICT` | A machine cannot be deleted while InstallationSpecs or other guarded provisioning ownership still exists. |
| `ART001_ARTIFACT_TRUST_FAILED` | Signed release metadata, its trust anchor or a verified checksum manifest could not establish trusted artifact provenance. |
| `ART002_ARTIFACT_HASH_MISMATCH` | Downloaded artifact content or an installer checksum manifest does not match its trusted SHA-256 identity. |
| `ART003_ARTIFACT_FETCH_FAILED` | Required artifact or release metadata could not be fetched within the bounded transport contract. |
| `DRV001_DRIVER_SPEC_UNSUPPORTED` | The pinned InstallationSpec requests state that the selected driver contract cannot implement without downgrade or reinterpretation. |
| `DRV002_DRIVER_RENDER_FAILED` | A driver accepted the InstallationSpec but could not deterministically render the required native material. |
| `INS001_INSTALLATION_SPEC_INVALID` | An InstallationSpec violates the immutable installation contract or attempts to supply server-owned identity metadata. |
| `INS002_INSTALLATION_NOT_FOUND` | A requested InstallationSpec does not exist. |
| `INS003_INSTALLATION_ASSIGNMENT_INVALID` | An assignment request violates Machine approval, InstallationSpec ownership or assignment state rules. |
| `INS004_INSTALLATION_ASSIGNMENT_CONFLICT` | A Machine already has a different armed Installation assignment. |
| `INS005_INSTALLATION_ASSIGNMENT_NOT_FOUND` | No assignment exists for the requested Installation or active Machine lookup. |
| `INS006_INSTALLER_TELEMETRY_INVALID` | An installer/reporter telemetry request violates body, stage, source, idempotency or other validation rules. |
| `INS007_INSTALLER_TELEMETRY_CONFLICT` | Telemetry would regress/skip lifecycle state, reuse an idempotency key inconsistently, or conflict with already accepted state. |
| `INS008_INSTALLER_LOG_LIMIT_EXCEEDED` | An installation log upload exceeded the bounded per-chunk or per-installation limit. |
| `INS009_INSTALLATION_DELETE_CONFLICT` | An InstallationSpec cannot be deleted while its destructive assignment is still armed. |
| `INS101_DEBIAN_BASE_INSTALL_FAILED` | Debian Installer reported failure of its native `bootstrap-base` step. |
| `INS102_DEBIAN_PROFILE_INSTALL_FAILED` | Debian Installer reported failure of its native `pkgsel` step. |
| `INS103_DEBIAN_BOOTLOADER_FAILED` | Debian Installer reported failure of its native GRUB installation step. |
| `INS104_HARDENING_FAILED` | The AegisPXE Debian late-command/hardening transaction aborted before successful completion. |
| `SEC001_CRYPTOGRAPHIC_BOOT_TRUST_REQUIRED` | A secret-bearing provisioning operation requires cryptographic machine/boot proof that has not been established. |
| `SEC002_OPERATOR_AUTHENTICATION_FAILED` | Bootstrap/recovery operator authentication failed without disclosing which credential detail was wrong. |
| `SEC003_OPERATOR_AUTH_RATE_LIMITED` | The remote exceeded the bounded operator authentication-attempt window. |
| `SEC004_OPERATOR_SESSION_REQUIRED` | An operator mutation was attempted without a valid unexpired server-side session. |
| `SEC005_OPERATOR_CSRF_INVALID` | An authenticated browser mutation did not present the CSRF value bound to its operator session. |
| `SEC006_SECURE_OPERATOR_TRANSPORT_REQUIRED` | Operator authentication or mutation was attempted over an untrusted cleartext network transport. |
| `SEC007_OPERATOR_USER_PENDING_REVIEW` | An external operator identity exists but still requires review. |
| `SEC008_OPERATOR_USER_BLOCKED` | The operator identity is blocked. |
| `SEC009_OPERATOR_USER_NOT_FOUND` | The requested operator identity does not exist. |
| `SEC010_OPERATOR_PASSKEY_REQUIRED` | The operation requires a passkey-authenticated operator session. |
| `SEC011_OPERATOR_PASSKEY_FAILED` | Passkey verification/enrollment failed. |
| `SEC012_OPERATOR_AUTHORIZATION_DENIED` | The authenticated operator lacks the role required for the operation. |
| `SEC013_OPERATOR_RECOVERY_FAILED` | Bootstrap/recovery authentication could not safely complete. |
| `SEC014_OPERATOR_WEBAUTHN_NOT_CONFIGURED` | WebAuthn/passkey configuration required by the requested flow is unavailable. |
| `SEC015_INSTALLER_CREDENTIAL_REQUIRED` | Installation-scoped runtime authentication material has not been issued or was omitted. |
| `SEC016_INSTALLER_CREDENTIAL_INVALID` | Installation-scoped Bearer/HMAC authentication did not verify. |
| `SEC017_INSTALLER_CREDENTIAL_EXPIRED` | Installation runtime authentication expired, was revoked, or used an unacceptable freshness timestamp. |
| `SEC018_BOOT_TRUST_ENROLLMENT_REQUIRED` | No administrator-approved machine-bound boot-trust key is available for the requested trust operation. |
| `SEC019_BOOT_TRUST_KEY_INVALID` | A submitted or stored boot-trust public key/binding violates the supported key contract. |
| `SEC020_BOOT_TRUST_PROOF_INVALID` | The signed boot-trust challenge proof or its binding did not verify. |
| `SEC021_BOOT_TRUST_CHALLENGE_EXPIRED` | A boot-trust challenge was presented outside its short validity window. |
| `SEC022_BOOT_TRUST_REPLAY_REJECTED` | A previously consumed boot-trust challenge could not be safely treated as an idempotent retry. |
| `VAL001_FIRST_BOOT_VALIDATION_FAILED` | The installed OS failed one or more mandatory AegisPXE first-boot validation checks. |
| `SYS001_STORAGE_FAILURE` | The persistent store could not complete an operation safely. |

The Go registry in `internal/fault` is authoritative for service-generated codes. Driver-native `INS1xx` and validation `VAL1xx` codes are part of the documented lifecycle contract and must remain stable once released. Documentation and source must change together when a new operator-visible code is introduced.
