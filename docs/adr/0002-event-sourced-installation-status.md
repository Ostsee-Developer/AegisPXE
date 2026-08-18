# ADR 0002: Event-sourced installation status

- Status: Accepted
- Date: 2026-08-18

## Context

Provisioning systems often infer progress from indirect observations such as seed requests, HTTP traffic or elapsed time. That produces attractive but incorrect status displays and makes debugging harder.

AegisPXE requires authoritative, explainable installation state.

## Decision

Installation progress is represented by an append-only event stream. Current status is a projection of accepted lifecycle events.

AegisPXE does not advance installation state because:

- a seed was fetched,
- an artifact was downloaded,
- a timeout interval elapsed,
- a bootloader made an HTTP request.

Each lifecycle transition is reported by an explicitly authorized source that has direct evidence the native installer/runtime reached that stage.

State/event persistence is atomic where required.

## Consequences

### Positive

- exact installation timeline,
- replayable/auditable state history,
- easier debugging,
- deterministic status UI,
- clean duplicate/replay handling,
- failures retain context instead of overwriting previous state.

### Negative

- OS drivers must implement real telemetry hooks,
- the system cannot fake smooth progress when a native installer exposes limited information,
- event sequencing/idempotency adds explicit design work.

These costs are accepted because correctness is more important than decorative progress.

## Guardrail

A new status stage must define:

- authoritative source,
- allowed predecessor states,
- idempotency behavior,
- failure semantics,
- tests,
- documentation.