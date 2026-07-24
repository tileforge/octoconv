# Phase 38 Discussion Log

**Date:** 2026-07-23
**Mode:** discuss (default)

## Framing

Phase 38 (Schema Foundation) is a near-fully-locked additive migration. Carried forward without re-asking: migration convention (`000N_*.sql`, embedded runner), `canceled` already in the `status` CHECK (no migration), the listing keyset index `jobs_client_created_idx` already exists, zero new Go deps, all columns nullable/additive.

## Gray areas presented

1. Migration scope — 4 progress columns only vs also folding in PUSH-02's `progress_webhook` flag
2. Column types & CHECK constraints
3. (option) "accept defaults"

**User selection:** all three — interpreted as "make sensible decisions on both real gray areas, defaults where obvious, and proceed." No deep-dive turns.

## Decisions captured (see 38-CONTEXT.md)

- **Scope (D-02):** exactly the four progress/ETA columns; PUSH-02 flag deferred to Phase 42 (may live in existing `options jsonb`, no pre-commit). `canceled` needs no migration.
- **Types (D-03..06):** `progress_pct smallint CHECK(0..100)`, `eta_seconds int CHECK(>=0)`, `source_duration_seconds double precision CHECK(>0)`, `progress_updated_at timestamptz` — all nullable, no defaults → zero behavior change.
- **Indexes (D-07):** none new — existing `jobs_client_created_idx` covers Phase 40's keyset listing.
- **Fail-closed CHECKs at DB layer (D-08):** matches the existing status/engine/operation CHECK precedent on `jobs`.

## Deferred

- PUSH-02 `progress_webhook` opt-in flag → Phase 42.
- Populating/exposing the columns → Phases 40 (read) / 41 (write).
