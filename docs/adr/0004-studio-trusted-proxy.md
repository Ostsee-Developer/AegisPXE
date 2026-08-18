# ADR 0004: Studio listener and trusted reverse-proxy boundary

- Status: Accepted; authentication/session sections superseded in part by ADR 0005
- Date: 2026-08-18

## Context

AegisPXE has two HTTP audiences with materially different network requirements.

PXE firmware and iPXE clients must reach discovery and boot material directly on the provisioning network. Browser administration should instead sit behind a TLS/SSO boundary such as a reverse proxy with Passkey authentication. Treating both audiences as one listener caused two problems: a loopback-safe Studio default also made PXE HTTP unreachable, while making the shared listener network-reachable exposed browser routes on the provisioning port.

AegisPXE must also support deployments where the reverse proxy is not on the AegisPXE host. In that case the Studio backend may need a non-loopback bind, but arbitrary clients must not gain operator authority by forging common proxy headers.

Public DNS names and external origins are deployment properties and must not be compiled into the application.

## Decision

### 1. Separate HTTP listener roles

AegisPXE runs distinct PXE and Studio listener surfaces in the same control-plane process.

The PXE surface permits only:

- health,
- discovery,
- `/boot/` provisioning transport.

It does not expose Studio or operator routes.

The Studio surface permits only:

- browser Dashboard routes,
- read-only Machine inventory API required by the browser surface,
- health.

It does not expose discovery or `/boot/` routes.

The package default for the PXE listener is network reachable because that is a functional requirement of PXE. The Studio listener remains loopback by default.

### 2. Configuration, not compiled origins

Listener addresses and trusted-proxy metadata are environment/package configuration. Browser navigation uses relative paths. PXE boot URLs continue to derive from the actual PXE request.

No public Studio hostname, DNS suffix, reverse-proxy URL or installer callback origin is compiled into AegisPXE.

### 3. Trusted reverse-proxy transport and identity

AegisPXE accepts an asserted reverse-proxy identity only when all of the following are true:

1. the direct TCP peer IP is contained in an explicitly configured trusted proxy CIDR/address,
2. the configured forwarded-protocol header is exactly `https`,
3. when an identity is asserted, the configured identity header contains a bounded non-empty value.

Header names are configurable. They are not trust signals when the direct peer is outside the configured proxy network.

The reverse proxy is required to overwrite or remove client-supplied instances of the configured headers before forwarding.

A trusted proxy source without an identity may reach the Studio transport boundary for health checks and the explicit recovery path. Transport trust is not human authentication.

### 4. AegisPXE session remains an application responsibility

As superseded by ADR 0005, a trusted proxy identity alone no longer creates an AegisPXE operator session.

The external subject selects the AegisPXE user. AegisPXE then requires its own Passkey proof and applies its own user status/role authorization before issuing a session. Browser mutations remain protected by session-bound CSRF.

See `docs/adr/0005-layered-human-authentication.md` for the current human-authentication contract.

### 5. Non-loopback Studio binding fails closed

A non-loopback Studio listener is accepted only when Trusted Proxy configuration is enabled. When Trusted Proxy mode is active, Studio requests are accepted only from:

- loopback, for local recovery access, or
- a request satisfying the trusted direct-peer and forwarded-HTTPS transport contract.

Human identity and Passkey authorization are evaluated independently after that source/transport gate.

Host firewalls should additionally restrict the Studio backend port to the reverse proxy where possible.

### 6. Local operator key becomes a recovery factor

As superseded by ADR 0005, the local operator key is not a standalone browser login. It is retained as a local recovery factor and as the proof required to claim the initial administrator before Passkey enrollment.

### 7. Operator trust and installer trust remain separate

Reverse-proxy identity and AegisPXE human Passkey authentication apply to human operator actions only. They do not satisfy cryptographic Machine boot trust and cannot release lifecycle credentials.

A future installer telemetry endpoint may share the same external DNS origin as the browser Dashboard, but such paths must use installation-scoped cryptographic authorization rather than interactive human authentication. Any reverse-proxy authentication exception for those paths must therefore correspond to an AegisPXE installer trust gate.

## Consequences

- PXE HTTP remains reachable without exposing the administrative Dashboard on the PXE listener.
- Studio can live behind a reverse proxy without treating proxy identity as sufficient authorization for destructive provisioning.
- Forged forwarded-protocol or identity headers from ordinary network clients do not create operator authority.
- Trusted proxy health checks need no fake user identity.
- Local recovery remains usable without creating a key-only web login.
- Deployments may change domains and external origins without rebuilding AegisPXE.
- Installer authentication remains independently reviewable and cannot be accidentally replaced by human authentication trust.
