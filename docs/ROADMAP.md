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

- unknown VM checks in through an iPXE bootstrap,
- machine appears in the embedded Studio/API as pending,
- repeat boot does not duplicate the machine,
- machine detail exposes identifiers and audit/discovery timeline,
- discovery and Studio reads remain low privilege/read-only,
- no provisioning material is exposed,
- `provision` fails closed until an immutable InstallationSpec is armed,
- client leaves the provisioning path,
- complete correlated discovery and policy-decision logs are available,
- body/rate bounds protect the unauthenticated discovery edge,
- E2E runs against the packaged installation rather than an ad-hoc source checkout.

Gate: packaged 20-repeat discovery contract plus a real disposable-VM PXE run, identity-conflict/failure tests and useful correlated logs.

Status: complete. The packaged 0.0.3 development path passed the real UEFI PXE repeat, identity-conflict, invalid-identity and reinstall persistence gates.

## 0.1.0: Debian 13 Standard

- immutable InstallationSpec foundation,
- Debian 13 driver,
- verified artifacts,
- unattended installer configuration,
- layered provisioning trust and assignment,
- minimal authenticated operator create/approve/arm/cancel path,
- assignment-authorized public boot transport,
- cryptographic boot trust for secret-bearing installer operations,
- real installer lifecycle telemetry,
- installer log ingestion,
- first-boot finalizer,
- desired-state validation,
- headless local boot after terminal state.

`0.1.0-dev.1` establishes the immutable InstallationSpec domain and persistence contract only. It intentionally exposes no administrative creation endpoint and does not boot an installer.

`0.1.0-dev.2` establishes Debian 13 amd64 artifact trust: signed `trixie/InRelease` verification through the Debian archive keyring, resolution from `current` to a versioned installer build, trusted checksum-manifest verification, and SHA-256 verification of the actual netboot kernel/initrd bytes. InstallationSpecs pin the verified source/version/digest/size/provenance metadata.

`0.1.0-dev.3` completes the InstallationSpec/BootSpec split. InstallationSpec remains the only authoritative desired-state record, artifact roles are unique, driver contract versioning is independent from the AegisPXE application version, and the Debian 13 driver validates the pinned artifact set before deterministically rendering a bounded secret-free BootSpec. BootSpec is not independently persisted.

`0.1.0-dev.4` resolves the selected profile revision into a versioned immutable ProfileSnapshot, pins the whole-device installation target, establishes the first strict Debian Standard capability gate, renders the native Debian Preseed SeedBundle, and adds correlated verified-artifact loading. Driver render, artifact verification and schema migration paths are structured-log observable and use stable failure codes. Seed/key/credential content is excluded from normal logs.

`0.1.0-dev.5` separates discovery identity, operator approval, armed assignment and cryptographic boot trust. It persists a single-active Machine-to-Installation assignment with audit events, defines public-boot versus secret-release trust gates, and adds read-only Studio InstallationSpec/assignment/trust views.

`0.1.0-dev.6` implements assignment-authorized public Debian boot transport. A `provision` Machine with an armed Assignment chains from discovery to an installation-scoped iPXE script. The script loads the verified Debian kernel and initrd, fetches the rendered non-secret `preseed.cfg`, and uses iPXE magic-initrd injection to place it at `/preseed.cfg` for native Debian initrd preseeding. No custom initrd repacker and no Debian `preseed/url` channel are introduced. All public reads remain non-consuming and do not imply `INSTALLER_STARTED`.

`0.1.0-dev.7` introduces the minimal bootstrap operator authentication boundary required before administrative Studio mutations. A local 256-bit bootstrap key is exchanged over direct TLS or loopback for an 8-hour server-side session. Browser mutations require session-bound CSRF, login attempts are rate-limited, and cleartext non-loopback HTTP remains read-only. Machine policy, Installation arm and Assignment cancel endpoints are already protected by this wrapper, but dev.7 intentionally renders no mutation buttons yet.

The next Studio slice exposes authenticated approve/arm/cancel controls and creates Debian Standard InstallationSpecs through a deliberate workflow that resolves trusted artifacts, snapshots the profile and requires explicit target-disk confirmation. It must not bypass the bootstrap operator boundary.

Cryptographic boot trust then gates lifecycle-credential release and authenticated installer telemetry. TPM-backed attestation is the preferred first hardware-backed path for capable systems; any non-TPM fallback requires an explicit security decision and must not silently downgrade.

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

- richer Provisioning Studio workflows beyond the minimal authenticated provisioning controls,
- public automation API,
- CLI workflows,
- external integrations,
- scaling/performance work,
- optional plugins outside the provisioning critical path.

Interactive live-boot catalogs remain outside the core roadmap.
