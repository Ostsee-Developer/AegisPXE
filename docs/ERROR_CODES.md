# Error Codes

AegisPXE uses stable machine-readable error codes for operational failures, API responses and installation events.

## Format

`<DOMAIN><NUMBER>_<NAME>`

Examples:

- `PXE001_DISCOVERY_RATE_LIMITED`
- `MAC001_MACHINE_IDENTITY_CONFLICT`
- `INS001_INSTALLATION_SPEC_INVALID`
- `SYS001_STORAGE_FAILURE`

The symbolic code is stable. Human-readable messages may improve without changing automation contracts.

## Current registry

### PXE / boot edge

- `PXE001_DISCOVERY_RATE_LIMITED`: unauthenticated discovery request exceeded the bounded per-source rate.

### Machine

- `MAC001_MACHINE_IDENTITY_CONFLICT`: supplied identifiers resolve to different known machines. AegisPXE refuses to guess or merge them.
- `MAC002_MACHINE_IDENTITY_INVALID`: machine identity observations are invalid or insufficient.
- `MAC003_MACHINE_NOT_FOUND`: requested machine does not exist.
- `MAC004_MACHINE_POLICY_INVALID`: requested machine policy or policy mutation metadata is invalid.

### Installation

- `INS001_INSTALLATION_SPEC_INVALID`: an InstallationSpec violates the immutable installation contract, including invalid target/profile/storage metadata, missing canonical artifact digests, or caller-assigned server identity fields.
- `INS002_INSTALLATION_NOT_FOUND`: requested InstallationSpec does not exist.

### System

- `SYS001_STORAGE_FAILURE`: persistence, transaction, schema or storage decoding failed.

## Rules

1. Codes are declared centrally in the Go fault package before use across boundaries.
2. A lower-trust client may receive a deliberately generic failure while structured server logs retain the stable internal code.
3. Correlation/request identifiers connect boundary logs without exposing secret material.
4. Error codes never contain credentials, identifiers, paths or other request-specific secret data.
5. New externally meaningful domain failures receive a code before UI, API, driver or automation behavior depends on them.
