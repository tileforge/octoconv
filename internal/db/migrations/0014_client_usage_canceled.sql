-- Widen client_usage_daily's status domain to admit 'canceled' (52-REVIEW.md
-- WR-01, Phase 52 gap closure).
--
-- What is superseded and why: 0011_client_usage_daily.sql stated that
-- `canceled` is deliberately not accounted here, because a cancel never
-- reaches MarkDone/MarkFailed -- the guarded terminal-transition write site
-- this table is filled from. That was correct while the rollup was
-- reporting-only. Phase 52 made the same rollup the input to quota
-- enforcement (internal/jobs/admission.go `evaluate`), which turned an
-- unaccounted cancel into a quota-evasion path (52-REVIEW.md WR-01):
-- `create -> cancel -> create ...` consumes upload bandwidth, an S3 object, a
-- queue slot and possibly engine CPU against a daily cap that never advances.
-- This migration supersedes that 0011 statement; `Repo.Cancel` now calls
-- recordUsage with status = 'canceled' inside its guarded transition (see
-- internal/jobs/repo.go).
--
-- What is NOT done: no backfill. Historical canceled jobs are not
-- retro-inserted into this table -- the ledger becomes complete going
-- forward only. Backfilling would require re-deriving per-day byte totals
-- from jobs/job_inputs rows the RETN-01 purge may already have removed, and
-- would charge clients today for consumption an operator never intended to
-- meter.
--
-- Why status stays part of the primary key rather than becoming a filter:
-- (client_id, engine, day, status) is the UPSERT conflict target recordUsage
-- depends on, so a canceled job gets its own row, every existing consumer
-- that slices by status keeps working, and the admission read -- which sums
-- across statuses with no status filter -- picks the new rows up with zero
-- query change.
--
-- Blast radius: one CHECK constraint on one table. No column added or
-- dropped, no row modified, no index changed, and
-- internal/jobs/retention.go's PurgeUsage is status-agnostic so retention is
-- unaffected.
--
-- The constraint's actual name was confirmed live via `\d client_usage_daily`
-- against the running database before writing this migration:
-- client_usage_daily_status_check (PostgreSQL's auto-generated name for the
-- inline column CHECK in 0011, matching the expected convention). Dropped
-- and re-added under that same explicit name so a future migration can
-- reference it deterministically instead of depending on the auto-naming
-- rule again.
ALTER TABLE client_usage_daily
    DROP CONSTRAINT IF EXISTS client_usage_daily_status_check;

ALTER TABLE client_usage_daily
    ADD CONSTRAINT client_usage_daily_status_check
        CHECK (status IN ('done', 'failed', 'canceled'));
