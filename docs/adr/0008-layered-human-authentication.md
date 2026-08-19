# ADR 0008: Layered human authentication and AegisPXE-owned authorization

- Status: Accepted
- Date: 2026-08-18

## Context

ADR 0004 established the split PXE/Studio listener roles and the trusted reverse-proxy boundary. In `0.1.0-dev.9`, a valid trusted-proxy identity was sufficient to create an AegisPXE operator session.

For the first beta-quality operator boundary, that model couples AegisPXE authorization too tightly to the external identity provider. A reverse proxy should be able to provide convenient Passkey/SSO identity without becoming the sole authority for destructive provisioning actions.

AegisPXE also needs a recovery path that remains usable if the external identity provider stops asserting users or the reverse proxy must be bypassed locally through an SSH tunnel.

Public hostnames and WebAuthn origins remain deployment-specific properties and cannot be compiled into AegisPXE.

## Decision

### 1. Trusted proxy establishes outer identity only

A valid trusted-proxy request may provide an external provider/subject to AegisPXE. That assertion does not create an operator session and does not assign an AegisPXE role.

The direct peer CIDR and forwarded HTTPS checks from ADR 0004 remain mandatory before proxy identity headers are considered.

### 2. AegisPXE requires an independent Passkey

Normal interactive access requires both:

1. a trusted outer identity assertion,
2. a successful AegisPXE WebAuthn assertion for the corresponding AegisPXE user.

Only after both proofs succeed may AegisPXE create its own server-side session.

WebAuthn relying-party ID and allowed origins are configuration. No deployment hostname is hardcoded.

### 3. AegisPXE owns user status and roles

A newly observed external identity is persisted as `pending_review` and receives no provisioning authority.

An AegisPXE administrator may approve it as `operator` or `admin`, moving it to `enrollment_required`. Successful Passkey enrollment activates the account.

The supported account states are:

- `pending_review`
- `enrollment_required`
- `active`
- `blocked`

The supported initial roles are:

- `operator`
- `admin`

The last non-blocked administrator cannot be blocked through the Store contract.

### 4. WebAuthn user handle is independent of the external subject

The visible proxy subject is not used as the WebAuthn user handle. AegisPXE generates and persists an opaque random handle for each AegisPXE user.

This keeps WebAuthn credential identity independent of display/user naming and leaves room for future provider subject migrations without redesigning the credential schema.

### 5. Initial administrator requires recovery-key proof

The first external identity is not automatically privileged.

If no administrator exists, that pending identity may claim the initial administrator role only by proving the local AegisPXE recovery key. It must then enroll an AegisPXE Passkey before becoming active.

The initial-admin claim is transactional so concurrent first-user requests cannot create multiple initial administrators.

### 6. Emergency login requires three factors

When no external identity is supplied, the browser may use the emergency path. It requires all of:

1. a known AegisPXE subject entered by the operator,
2. the existing AegisPXE Passkey for that account,
3. the local recovery key.

Successful recovery Passkey verification yields a one-time, five-minute ticket. A session is issued only after the recovery key is also verified and the ticket is consumed.

The recovery key alone cannot issue a browser session.

### 7. Session provenance is explicit

AegisPXE sessions store user ID, role and authentication method.

Normal sessions record:

`zoraxy+passkey`

Emergency sessions record:

`recovery+passkey+key`

This provenance is available to structured audit logs without recording authentication secrets.

### 8. Health/source trust and user identity are separate checks

A trusted proxy source may reach the Studio listener without an identity, for example for `/healthz` or to expose the explicit recovery path when the identity layer is unavailable.

Being on the trusted transport boundary is not equivalent to being an authenticated AegisPXE user.

### 9. Old session shortcuts are removed

The previous trusted-proxy-to-session shortcut and recovery-key-only web login are removed from the application path. The unified Dashboard is the only browser administration surface.

## Consequences

- Compromise or misconfiguration of the outer identity layer is insufficient by itself to obtain an AegisPXE operator session.
- AegisPXE maintains its own credential and authorization state, increasing local responsibility but making destructive provisioning authorization independently reviewable.
- New users can be discovered automatically without automatically becoming privileged.
- Recovery remains possible without weakening normal authentication.
- WebAuthn deployment properties remain configurable and domain-independent.
- Structured logs can distinguish normal and emergency sessions.
- Machine/installer trust remains entirely separate from human authentication.

## Relationship to ADR 0004

ADR 0004 remains authoritative for listener separation, trusted direct-peer/protocol validation, deployment-configured origins and the separation of human vs installer trust.

ADR 0008 supersedes the parts of ADR 0004 that stated a trusted proxy identity directly creates an AegisPXE operator session or that the local operator key is sufficient for browser login.
