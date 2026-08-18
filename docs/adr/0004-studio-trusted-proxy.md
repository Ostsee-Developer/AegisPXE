# ADR 0004: Studio listener and trusted reverse-proxy boundary

- Status: Accepted
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

- Studio/operator UI,
- read-only Machine inventory API required by the browser surface,
- health.

It does not expose discovery or `/boot/` routes.

The package default for the PXE listener is network reachable because that is a functional requirement of PXE. The Studio listener remains loopback by default.

### 2. Configuration, not compiled origins

Listener addresses and trusted-proxy metadata are environment/package configuration. Browser navigation uses relative paths. PXE boot URLs continue to derive from the actual PXE request.

No public Studio hostname, DNS suffix, reverse-proxy URL or installer callback origin is compiled into AegisPXE.

### 3. Trusted reverse-proxy authentication

AegisPXE accepts an asserted reverse-proxy operator identity only when all of the following are true:

1. the direct TCP peer IP is contained in an explicitly configured trusted proxy CIDR/address,
2. the configured forwarded-protocol header is exactly `https`,
3. the configured identity header contains a bounded non-empty identity.

Header names are configurable. They are not trust signals when the direct peer is outside the configured proxy network.

The reverse proxy is required to overwrite or remove client-supplied instances of the configured headers before forwarding.

### 4. Session and CSRF remain AegisPXE responsibilities

A trusted proxy identity is exchanged for the same short-lived server-side AegisPXE operator session used by the bootstrap path. Browser mutations still require the session-bound CSRF token.

Audit actors use `proxy:<identity>` so proxy-authenticated mutations remain attributable without storing the proxy's Passkey/WebAuthn credentials.

### 5. Non-loopback Studio binding fails closed

A non-loopback Studio listener is accepted only when Trusted Proxy configuration is enabled. When Trusted Proxy mode is active, Studio requests are accepted only from:

- loopback, for local recovery/bootstrap access, or
- a request satisfying the Trusted Proxy authentication contract.

Host firewalls should additionally restrict the Studio backend port to the reverse proxy where possible.

### 6. Bootstrap operator becomes recovery path

The local bootstrap operator key remains available on loopback for recovery and initial administration. It is not the preferred normal browser authentication mechanism in reverse-proxy/SSO deployments.

### 7. Operator trust and installer trust remain separate

Reverse-proxy SSO authenticates human operator actions only. It does not satisfy cryptographic Machine boot trust and cannot release lifecycle credentials.

A future installer telemetry endpoint may share the same external DNS origin as Studio, but such paths must use installation-scoped cryptographic authorization rather than interactive reverse-proxy SSO. Any reverse-proxy authentication exception for those paths must therefore correspond to an AegisPXE installer trust gate.

## Consequences

- PXE HTTP remains reachable without exposing the administrative Studio on the PXE listener.
- Studio can live behind a reverse proxy using Passkeys or another SSO method without AegisPXE implementing a second WebAuthn credential store.
- Forged `X-Forwarded-Proto`, identity or similar headers from ordinary network clients do not create operator authority.
- Existing local bootstrap recovery remains usable.
- Deployments may change domains and external origins without rebuilding AegisPXE.
- Installer authentication remains independently reviewable and cannot be accidentally replaced by human SSO trust.
