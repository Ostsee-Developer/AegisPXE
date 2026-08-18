# Profile Schema Principles

Profiles describe OS-neutral desired state. They do not contain installer syntax or arbitrary execution hooks.

The concrete serialized schema will be versioned when implementation begins. These semantic boundaries are already normative.

## Identity

May describe:

- whether an administrative user is created,
- username,
- display/full name,
- SSH public-key reference,
- password-authentication policy,
- root-login policy,
- sudo policy.

Private keys and plaintext reusable passwords are not profile content.

## Storage

May describe intent such as:

- automatic whole-disk layout,
- LVM intent,
- encryption required/disabled,
- TPM2 auto-unlock requested when supported.

The OS driver decides the native implementation. Unsupported combinations fail preflight.

## Packages

Profiles may request packages by semantic package name appropriate to the target capability contract. The driver owns translation where package names differ.

Profiles must not embed package-manager shell commands.

## Firewall

Profiles describe policy such as default ingress/egress behavior and allowed services/ports. The driver owns native implementation.

## Hardening

Profiles may request supported security intents, for example:

- automatic security updates,
- SSH hardening,
- fail2ban-like brute-force protection where supported,
- kernel/network hardening where supported,
- audit service where supported.

Each hardening feature is represented in the driver capability matrix.

## Validation

A profile may require final assertions such as:

- admin identity exists,
- SSH key is installed,
- password login is disabled,
- requested firewall policy is active,
- disk encryption is active,
- TPM state matches requested intent.

Validation requirements affect `COMPLETED`: required checks must pass before completion.

## Forbidden profile content

Profiles must not contain:

- shell scripts,
- `runcmd`,
- `late_command`,
- Kickstart fragments,
- Preseed fragments,
- Cloud-Init YAML fragments,
- systemd commands,
- arbitrary filesystem paths outside typed file-policy features explicitly added by future ADR.

## Revisions

Published profile revisions are immutable. Editing a profile creates a new revision. An InstallationSpec pins an exact revision.

## Initial built-in intent

The first vertical slices may define two system profiles:

- `Standard`: secure SSH-key-first server provisioning without disk encryption.
- `Encrypted`: Standard plus supported full-disk/LVM encryption and optional TPM2 enrollment.

The implementation begins with `Standard`; `Encrypted` is not added until the Standard Debian E2E path is stable.