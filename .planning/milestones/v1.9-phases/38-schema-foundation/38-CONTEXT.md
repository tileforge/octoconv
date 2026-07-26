# Phase 38: Schema Foundation - Context

**Gathered:** 2026-07-23
**Status:** Ready for planning

<domain>
## Phase Boundary

Lay down the durable schema every later v1.9 phase reads or writes: an additive, zero-behavior-change migration `0007_job_progress.sql` adding four nullable columns to `jobs` for progress/ETA/denominator/throttle. No application behavior changes in this phase — existing `POST /v1/jobs` inserts and `GET /v1/jobs/{id}` responses are byte-for-byte unchanged. This phase carries no direct v1.9 requirement; it unblocks Phase 40 (listing reads the denormalized `progress_pct`) and Phase 41 (progress/ETA writes).

**In scope:** the four nullable columns + their fail-closed CHECK constraints, applied via the existing embedded-migration runner.
**Out of scope:** any read/write code that populates or exposes these columns (Phases 40/41), the cancel path (Phase 39 — needs no schema change), the PUSH-02 opt-in flag (Phase 42), any index changes.
</domain>

<decisions>
## Implementation Decisions

### Migration
- **D-01:** New migration file `internal/db/migrations/0007_job_progress.sql`, following the existing `000N_<slug>.sql` convention (last is `0006_audio_engine.sql`), applied by the existing embedded `db.Migrate()` runner. Pure `ALTER TABLE jobs ADD COLUMN`.
- **D-02:** Migration scope is EXACTLY these four columns — nothing else. The `canceled` status needs NO schema change (already in the `jobs.status` CHECK constraint and Go `StatusCanceled` const since `0001_init.sql:50`). The listing keyset index also needs NO change (see D-05).

### Column definitions (all nullable, NO default → existing rows/inserts unaffected → zero behavior change)
- **D-03:** `progress_pct  smallint  CHECK (progress_pct BETWEEN 0 AND 100)` — nullable. `null` = indeterminate (the honest value for non-ffmpeg engines and pre-first-tick av/audio). `smallint` holds 0–100 tidily; the DB-layer CHECK is fail-closed, matching the project's fail-closed idiom.
- **D-04:** `eta_seconds  int  CHECK (eta_seconds >= 0)` — nullable (seconds remaining; well within `int`). `null` when not derivable.
- **D-05:** `source_duration_seconds  double precision  CHECK (source_duration_seconds > 0)` — nullable. ffprobe reports fractional seconds (e.g. `30.000000`); `double precision` matches the Go float probe and keeps sub-second accuracy for the percent denominator. Stored value is always positive; `null` = unknown (never store 0, which would be a div-by-zero denominator).
- **D-06:** `progress_updated_at  timestamptz` — nullable. Feeds Phase 41's time-gate throttle and any staleness detection; distinct from the existing `updated_at` (which the `jobs_set_updated` trigger bumps on every write).

### Index & constraint decisions
- **D-07:** NO new indexes. Phase 40's keyset listing (`WHERE client_id=$1 ORDER BY created_at DESC, id`) is already covered by the existing `jobs_client_created_idx ON jobs (client_id, created_at DESC)` (`0001_init.sql:69`). The progress columns are read as part of a single job row (by PK or in the client-scoped listing), never filtered/sorted on, so they need no index.
- **D-08:** Fail-closed CHECK constraints live at the DB layer (not app-only), consistent with the project's existing `status`/`engine`/`operation` CHECK constraints on this same table.

### Claude's Discretion
- Exact column ordering in the `ALTER TABLE` and comment wording follow the existing migration files' style.
- Whether to add the four `ADD COLUMN`s as one `ALTER TABLE jobs ADD COLUMN … ADD COLUMN …` or four separate statements — planner/executor picks whatever matches the repo's migration style; functionally equivalent (all additive, all in one migration file).
</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Schema + migration convention
- `internal/db/migrations/0001_init.sql` — the `jobs` table (columns, types, the `status` CHECK that already includes `'canceled'` at :50, and the existing indexes incl. `jobs_client_created_idx` at :69). The new columns must match this file's type/style conventions (`int`, `timestamptz`, DB-layer CHECKs).
- `internal/db/migrations/0006_audio_engine.sql` — the most recent migration; mirror its file-header/style for `0007`.
- `internal/db/db.go` — the embedded `Migrate()` runner that applies `migrations/*.sql` in order (no change needed; the new file is picked up automatically).

### Requirements + research driving the columns
- `.planning/REQUIREMENTS.md` — PROG-02 (persist source_duration) is the only requirement that directly needs a column here; PROG-01/03/04 and JLIST-02 read these columns in later phases.
- `.planning/research/SUMMARY.md` + `.planning/research/ARCHITECTURE.md` — specify the `0007_job_progress.sql` column set and the "canceled needs no migration" finding.
</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- Embedded migration runner (`db.Migrate()` over `migrations/*.sql`) — adding `0007_job_progress.sql` is the entire mechanism; no runner change.
- `jobs_client_created_idx (client_id, created_at DESC)` already exists — the keyset-pagination index Phase 40 needs is already in place.

### Established Patterns
- Additive `NNNN_<slug>.sql` migrations, applied in lexical order; DB-layer CHECK constraints on `jobs` (status/engine/operation) are the precedent for the new progress/duration CHECKs.
- The `jobs_set_updated` trigger bumps `updated_at` on write — `progress_updated_at` is a separate, purpose-specific timestamp so high-frequency progress writes (Phase 41) don't have to reason about the general `updated_at`.

### Integration Points
- These columns are the read surface for Phase 40 (`Repo.List` selects `progress_pct`/`status`) and the write surface for Phase 41 (`Repo.UpdateProgress` sets `progress_pct`/`eta_seconds`/`progress_updated_at`; the worker persists `source_duration_seconds`).
</code_context>

<specifics>
## Specific Ideas

Keep this migration boring and minimal — four nullable columns with fail-closed CHECKs, one file, zero behavior change. The value is entirely in being a safe, additive foundation the risky phases (41 progress-core) can build on without a schema change mid-flight. Verify: migration applies cleanly on both a fresh DB and a populated one, and `GET /v1/jobs/{id}` output is unchanged (the new columns are not yet selected/exposed by any handler).
</specifics>

<deferred>
## Deferred Ideas

- **PUSH-02 opt-in `progress_webhook` flag** — deferred to Phase 42, which will decide whether it needs a dedicated column or can live in the existing `options jsonb` (like other per-job options). Not pre-added here to avoid committing a column that may be unnecessary.
- Populating/exposing any of these columns — Phases 40 (read) and 41 (write); this phase only creates them.
</deferred>

---

*Phase: 38-schema-foundation*
*Context gathered: 2026-07-23*
