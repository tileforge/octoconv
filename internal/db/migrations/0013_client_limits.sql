-- Create client_limits: per-(client, engine) quota storage, plus the
-- clients.enforcement_mode switch, and an index for the in-flight occupancy
-- count the admission gate will run (QUOT-01/QUOT-04, Phase 52).
--
-- Why a dedicated table keyed (client_id, engine): D-01 mirrors
-- client_usage_daily's (client_id, engine, day, status) key one level
-- shallower, so the admission gate's daily check (Plan 03) is a primary-key
-- read joined one-to-one against this table rather than an aggregation.
-- There is no client-wide tier and no inheritance resolution in this phase --
-- expressing "1 GB/day across everything" costs one row per engine, which is
-- cheaper than an inheritance rule that must be applied correctly at every
-- read site, given a handful of internal consumers (D-01).
--
-- Blast radius: additive only. No existing table, column, constraint or row
-- is modified; jobs, job_events and client_usage_daily are untouched by this
-- migration.
CREATE TABLE client_limits (
    client_id       uuid NOT NULL REFERENCES clients (id) ON DELETE CASCADE,
    engine          text NOT NULL,
    -- NULL is the unlimited value (D-02, QUOT-01) -- never a sentinel
    -- number. All three columns are nullable with NO default: an absent row
    -- for a (client_id, engine) pair is equally unlimited (D-02), so a row
    -- inserted with every column NULL is behaviorally identical to no row at
    -- all, and both must stay that way for ROADMAP SC1 to hold.
    --
    -- max_concurrent is explicitly ALSO allowed to be 0, and 0 is NOT a
    -- sentinel or a disabled/unset marker: an explicitly-set
    -- max_concurrent = 0 means this client may run no jobs on this engine at
    -- all, which is how ROADMAP SC1's "allowed engine set" is expressed
    -- without a second column. 0 and NULL are deliberately distinguishable.
    max_concurrent  int,
    max_daily_bytes bigint,
    max_daily_jobs  bigint,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    CHECK (max_concurrent IS NULL OR max_concurrent >= 0),
    CHECK (max_daily_bytes IS NULL OR max_daily_bytes >= 0),
    CHECK (max_daily_jobs IS NULL OR max_daily_jobs >= 0),
    PRIMARY KEY (client_id, engine)
);

-- Deliberately NO CHECK (engine IN (...)) allow-list, for the same reason
-- 0011_client_usage_daily.sql documents for client_usage_daily -- sharpened
-- here because the stakes are higher: the admission read this table serves
-- runs INSIDE Repo.Create's transaction (Plan 03), ahead of `INSERT INTO
-- jobs`. A stale allow-list here would not merely fail to enforce a limit --
-- it would abort every `INSERT INTO jobs` for a newly added engine, i.e. it
-- would break conversion for that engine entirely, not just leave it
-- unmetered. Accounting and quota storage must never be able to break
-- conversion.

-- Reuses the set_updated_at() function already defined in 0001_init.sql
-- (precedent: 0002_client_api_keys.sql, 0011_client_usage_daily.sql) -- never
-- redefined here.
CREATE TRIGGER client_limits_set_updated
    BEFORE UPDATE ON client_limits
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Per-client enforcement switch (D-13). Default 'observe' is what makes
-- ROADMAP SC1 hold for already-provisioned clients: combined with the
-- NULL/absent-row semantics above, both defaults independently preserve
-- today's behavior for every existing client until an operator explicitly
-- opts one in. No new trigger is needed -- clients_set_updated already
-- exists from 0002_client_api_keys.sql and covers this column too.
ALTER TABLE clients
    ADD COLUMN enforcement_mode text NOT NULL DEFAULT 'observe'
        CHECK (enforcement_mode IN ('observe', 'enforce'));

-- The exact predicate the admission gate's occupancy COUNT filters on
-- (D-06): queued + active jobs for a given (client_id, engine). That COUNT
-- runs on every job creation while holding a per-(client_id, engine)
-- advisory lock (Plan 03), so an unindexed scan would hold the lock for the
-- duration of a scan of all in-flight rows across every client and engine.
-- The existing jobs_inflight_idx (0001_init.sql) is keyed on created_at
-- alone and cannot serve a client_id/engine equality filter. If the D-06
-- predicate ever changes, this index must change with it.
CREATE INDEX jobs_client_engine_inflight_idx ON jobs (client_id, engine)
    WHERE status IN ('queued', 'active');
