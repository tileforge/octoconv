-- Create client_usage_daily: the per-client consumption rollup (ACCT-01..04, RETN-01, Phase 51).
--
-- Why a dedicated rollup instead of aggregating `jobs` on the fly: usage
-- history must survive the `jobs`/`job_events` retention purge this same
-- phase introduces (RETN-01, D-12) -- aggregating over `jobs` would silently
-- truncate consumption history to whatever the operational retention window
-- happens to be. This table also grows as clients x engines x days, which is
-- predictable and small, rather than with raw job volume, and gives Phase 52
-- somewhere to record reservations distinct from consumption (a derived
-- query over `jobs` has nowhere to put that).
--
-- `canceled` is deliberately not accounted here: a cancel is user-initiated
-- and never reaches MarkDone/MarkFailed, so it never reaches the guarded
-- terminal-transition write site this table is filled from (Plan 02).
--
-- Retention: this table's 400-day window is ENFORCED BY the reconciler purge
-- added in Plan 04 (RETN-01, D-11/D-12/D-13) -- this comment documents the
-- policy, it is not the mechanism. A comment-only retention policy does not
-- satisfy RETN-01.
--
-- Phase 52 will add the reserved-vs-consumed distinction via its own ALTER
-- migration on top of this table; this table intentionally records
-- consumption only, not reservations.
--
-- Blast radius: additive only. No existing table, column, or constraint is
-- touched.
CREATE TABLE client_usage_daily (
    client_id     uuid NOT NULL REFERENCES clients (id) ON DELETE CASCADE,
    engine        text NOT NULL,
    day           date NOT NULL,
    -- D-04: failures are counted too (a failed conversion still burned CPU
    -- and occupied a worker slot), with status kept as a column rather than
    -- pre-filtered out, so any consumer can slice by it. Only the two
    -- terminal statuses that reach this write site are legal here; they
    -- come from the jobs.StatusDone/jobs.StatusFailed Go constants, not
    -- from arbitrary data, so a CHECK on them is safe and kept.
    status        text NOT NULL CHECK (status IN ('done', 'failed')),
    job_count     bigint NOT NULL DEFAULT 0,
    bytes_in      bigint NOT NULL DEFAULT 0,
    bytes_out     bigint NOT NULL DEFAULT 0,
    wall_seconds  double precision NOT NULL DEFAULT 0,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    -- D-01 phrased the key as (client_id, engine, day); it grows to include
    -- `status` here because D-04 stores status as a column instead of
    -- pre-filtering to one outcome -- this is the UPSERT conflict target
    -- Plan 02's rollup write depends on, so the divergence from D-01's
    -- wording is recorded here rather than silently introduced.
    PRIMARY KEY (client_id, engine, day, status)
);

-- Deliberately NO CHECK (engine IN (...)) allow-list, unlike jobs.engine.
-- This is the opposite of the codebase's usual closed-allowlist habit and
-- would otherwise look like an omission, so: the rollup INSERT runs inside
-- the guarded terminal-transition transaction (Plan 02). A CHECK here that
-- had not been updated in lockstep with a future NNNN_<engine>_engine.sql
-- migration would not merely fail to record accounting -- it would abort
-- that transaction and leave every job of the new engine unable to reach
-- `done`. Accounting must never be able to break conversion.

-- Index-driven purge target for Plan 04's `DELETE ... WHERE day < cutoff` --
-- without this index that purge would be a sequential scan over the whole
-- table on every reconciler tick.
CREATE INDEX client_usage_daily_day_idx ON client_usage_daily (day);

-- Reuses the set_updated_at() function already defined in 0001_init.sql
-- (precedent: 0002_client_api_keys.sql) -- never redefined here.
CREATE TRIGGER client_usage_daily_set_updated
    BEFORE UPDATE ON client_usage_daily
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
