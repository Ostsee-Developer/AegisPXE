# Error-code contract

AegisPXE errors that cross a domain or operational boundary use stable machine-readable codes. Human-readable messages may improve over time; the code remains the integration and diagnostic contract.

## Discovery and machine domain

- `PXE001_DISCOVERY_RATE_LIMITED`: unauthenticated discovery request exceeded the bounded per-source rate.
- `MAC001_MACHINE_IDENTITY_CONFLICT`: supplied identifiers resolve to different known machines. AegisPXE refuses to guess or merge them.
- `MAC002_MACHINE_IDENTITY_INVALID`: machine identity observations are invalid or insufficient.
- `MAC003_MACHINE_NOT_FOUND`: requested machine does not exist.
- `MAC004_MACHINE_POLICY_INVALID`: requested machine policy or policy mutation metadata is invalid.

## Installation domain

- `INS001_INSTALLATION_SPEC_INVALID`: an InstallationSpec violates the immutable installation contract, including invalid target/profile/storage metadata, missing canonical artifact digests, or caller-assigned server identity fields.
- `INS002_INSTALLATION_NOT_FOUND`: requested InstallationSpec does not exist.

## System/storage

- `SYS001_STORAGE_FAILURE`: persistence, transaction, schema or storage decoding failed.

## Rules

- Stable codes are logged at the point where the failure becomes authoritative.
- Secrets, credentials and recovery material never appear in codes, messages or normal structured fields.
- A lower-trust client may receive a deliberately generic failure while the server log preserves the stable internal code and correlation/request ID.
- New externally meaningful domain failures receive a code before they are relied on by UI, API, driver or automation behavior.
