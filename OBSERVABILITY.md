# Observability Contract

Observability is part of AegisPXE correctness. A feature that cannot explain what it did, for which machine and installation, and why it failed is incomplete.

## Core rules

1. Every operational I/O path must emit structured logs.
2. Every meaningful state change must emit an append-only lifecycle or audit event.
3. Installation progress must never be inferred from unrelated activity.
4. Every externally triggered operation carries a correlation identifier.
5. Every installation-scoped operation carries the installation ID when one exists.
6. Every machine-scoped operation carries the machine ID when one exists.
7. Secrets are redacted centrally and must never be intentionally logged.
8. Errors use stable machine-readable error codes in addition to human-readable messages.
9. Logs must preserve enough context to debug a failed installation without reproducing it first.
10. Pure deterministic transformations should remain log-free and be covered by unit tests.

## What must log

Structured logging is mandatory for:

- HTTP/API requests at operational boundaries,
- machine discovery and identity resolution,
- policy evaluation,
- database state mutations,
- artifact resolution/download/verification,
- boot decision generation,
- Secure Boot package validation and per-Machine enforcement,
- driver render operations,
- seed access decisions,
- lifecycle event acceptance/rejection,
- installer telemetry ingestion,
- first-boot finalization,
- validation,
- privileged helper actions,
- security decisions,
- retries/timeouts,
- background jobs.

## What should not spam logs

Pure helpers such as these normally do not log:

- MAC normalization,
- hostname validation,
- enum parsing,
- deterministic template rendering internals,
- value conversion,
- collection sorting.

Their caller logs the operational outcome. This keeps logs useful instead of turning them into function-entry traces.

## Logger

The Go implementation should use `log/slog` behind a small project-owned observability package. Business packages receive a logger/context rather than creating private loggers.

Required common fields where applicable:

- `component`
- `operation`
- `request_id`
- `machine_id`
- `installation_id`
- `driver_id`
- `stage`
- `result`
- `error_code`
- `duration_ms`

Security/boot operations add the relevant bounded evidence fields rather than prose-only messages. Secure Boot operations use, where applicable:

- `secure_boot_policy`
- `secure_boot_state`
- `firmware`
- `upstream_release`
- `upstream_commit`
- `release_asset_sha256`
- `ipxe_shim_sha256`
- `ipxe_sha256`
- `shim_digest`

Cryptographic digests and public release identities are safe operational metadata. Binary payloads, private keys and secret material are never logged.

Example:

```json
{
  "level": "INFO",
  "component": "driver.debian13",
  "operation": "render_seed",
  "request_id": "req_01...",
  "machine_id": "mach_01...",
  "installation_id": "inst_01...",
  "driver_id": "debian13",
  "duration_ms": 12,
  "result": "success"
}
```

Secure Boot startup example:

```json
{
  "level": "INFO",
  "component": "boot.secureboot",
  "operation": "validate_assets",
  "secure_boot_policy": "required",
  "upstream_release": "v2.0.0",
  "upstream_commit": "12798ec29aa8a64d8675c4378b99f5fe28447afb",
  "release_asset_sha256": "sha256:...",
  "ipxe_shim_sha256": "sha256:...",
  "ipxe_sha256": "sha256:...",
  "result": "success"
}
```

A required-policy rejection must identify the Machine and stable reason without pretending that a firmware query value is remote attestation:

```json
{
  "level": "WARN",
  "component": "boot.secureboot",
  "operation": "evaluate",
  "request_id": "req_01...",
  "machine_id": "m_01...",
  "secure_boot_policy": "required",
  "secure_boot_state": "disabled",
  "error_code": "SEC023_SECURE_BOOT_REQUIRED",
  "result": "rejected"
}
```

## Correlation identifiers

AegisPXE uses distinct identifiers for distinct scopes:

- `request_id`: one inbound API/HTTP operation,
- `machine_id`: stable machine identity,
- `installation_id`: one immutable provisioning attempt,
- `job_id`: asynchronous work such as artifact download,
- `event_seq`: monotonic installation event sequence.

Logs must not use a MAC address as the sole correlation mechanism.

## Log classes

### System log

Operational logs from AegisPXE itself: API, database, artifact manager, drivers, helper, worker, boot and Secure Boot services.

### Installation log

A correlated view containing all logs associated with one InstallationSpec, including accepted installer telemetry.

### Audit log

Append-only administrative/security events such as:

- machine approved,
- machine blocked,
- installation created,
- profile revision selected,
- provisioning armed/cancelled,
- recovery material revealed,
- user/role changes.

Audit records must include actor, action, target, timestamp and result.

Secure Boot firmware observations are Machine inventory/security evidence and belong in structured system/security logs and the persisted Machine record. They do not create lifecycle progress.

## Installer log ingestion

Every production-capable OS driver must define how relevant native installer logs are streamed or uploaded to AegisPXE. Telemetry support is a required driver capability, not an optional enhancement.

Uploaded log chunks require:

- installation-scoped authentication,
- sequence numbers,
- source identity,
- bounded payload size,
- server-side timestamp in addition to client-reported timestamp,
- redaction before durable persistence where feasible.

The current Debian reporter runtime is suspended from production while its delivery mechanism is redesigned. Secure Boot does not reintroduce it into the native Debian initrd path.

## Error codes

Errors shown in the UI and lifecycle should include stable codes. Initial namespaces:

- `PXE`: discovery/boot transport,
- `MAC`: machine identity,
- `ART`: artifact management,
- `DRV`: driver/compiler,
- `INS`: installer runtime,
- `SEC`: authentication/authorization/security policy,
- `VAL`: final validation,
- `SYS`: AegisPXE internal platform errors.

Example: `ART002_ARTIFACT_HASH_MISMATCH`.

Secure Boot uses:

- `SEC023_SECURE_BOOT_REQUIRED`
- `SEC024_SECURE_BOOT_EVIDENCE_INVALID`
- `SEC025_SECURE_BOOT_ASSETS_INVALID`

Codes are API contracts once released. Renaming/removing a code requires an ADR or explicit compatibility decision.

## Secret redaction

Never log:

- passwords,
- LUKS recovery keys,
- SSH private keys,
- session cookies,
- bearer tokens,
- lifecycle tokens,
- seed access tokens,
- Vault contents.

The logging package must provide central redaction for known sensitive keys such as `token`, `password`, `secret`, `authorization`, `cookie`, `recovery_key` and variants. Redaction is defense in depth, not permission to pass secrets into log calls.

## Failure behavior

A swallowed operational error is a defect. Operational failures must either:

- be returned to a caller that records them, or
- be recorded locally with sufficient context and a stable error code.

`|| true`-style suppression is forbidden for meaningful provisioning actions. Best-effort cleanup may ignore a failure only when the code comments and logs explain why that failure cannot affect correctness.

For Secure Boot, failure is explicit and fail-closed when policy is `required`:

- invalid package-owned signed boot assets prevent startup,
- malformed firmware evidence is rejected,
- non-enabled/unknown/SetupMode/BIOs state does not authorize destructive installation boot material,
- no fallback path may silently switch to unsigned network boot while recording success.

## UI requirement

The Studio must provide installation-scoped views for:

- lifecycle timeline,
- system/driver logs,
- installer logs,
- validation results,
- audit events relevant to that installation.

Machine views must expose the normalized Secure Boot observation and observation time as security evidence without presenting that value as cryptographic attestation.

Debugging must not depend on SSH access to the AegisPXE host as the primary workflow.
