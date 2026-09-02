-- Introduce job_outputs.role and widen jobs.operation to admit 'sanitize'
-- (ROLE-01, D-15, Phase 57). Both steps are additive schema preparation for
-- Phase 60 (sanitize engine) so that phase touches only the engine, queue and
-- container, never the schema.
--
-- Step 1: job_outputs.role. Every output row needs a role so `handleGetJob`
-- has something to select on once a second output exists (D-13's future
-- invariant: a done job has exactly one primary output). Closed CHECK, DB
-- default 'primary' -- the default is the source of truth, not a Go-layer
-- fallback (D-16); AddOutput deliberately never writes this column (see
-- internal/jobs/repo.go).
--
-- Why ADD COLUMN ... NOT NULL DEFAULT 'primary' needs no backfill: PostgreSQL
-- >= 11 materializes a constant column default as table metadata rather than
-- rewriting the heap, so every pre-existing row is read back as 'primary'
-- without a rewrite pass, and no ALTER TABLE ... UPDATE step is required.
-- This is PROVEN against the running PostgreSQL 18 instance by
-- TestOutputRoleDefaultAppliesToPreExistingRows (internal/jobs/output_role_test.go,
-- Task 3) -- a direct INSERT shaped exactly like the pre-migration schema
-- (no role column named), read back as 'primary' -- not asserted from
-- general Postgres knowledge.
--
-- Step 2: jobs_operation_check gains 'sanitize'. The constraint name
-- jobs_operation_check is Postgres's auto-generated name for the inline,
-- unnamed column CHECK declared in 0001_init.sql (the standard
-- <table>_<column>_check convention for an unnamed CHECK), already confirmed
-- live by the 0005_html_engine.sql/0006_audio_engine.sql/0009_ocr_engine.sql/
-- 0010_pandoc_engine.sql migrations against the sibling jobs_engine_check --
-- this migration applies the identical DROP/ADD pattern to jobs_operation_check
-- for the first time.
--
-- Intentional divergence #1 -- CLOSED BY PHASE 60 (2026-09-01). Left in place
-- rather than deleted, because a reader of this migration is reading a record
-- of what was true when it was applied.
--
-- As applied: the DB accepted operation='sanitize' while convert.Default had
-- no converter registered for it, and handleCreateJob rejected 'sanitize' with
-- a 422 unsupported_operation before any row was written (57-04) -- a
-- divergence with zero runtime consequence, recorded so a future reader would
-- not go hunting for a bug that did not exist.
--
-- Phase 60 registered convert.SanitizeConverter under the probe engine class,
-- so the registry and this CHECK now agree and the 422 is gone for sanitize
-- (and for sanitize only -- internal/api/pre60_pinning_test.go pins that the
-- other unregistered operations are still rejected byte-identically). A note
-- describing an intentional divergence that no longer exists is the same
-- class of defect as a comment promising a route that never existed, which is
-- why this says so here rather than only in a phase summary.
--
-- Intentional divergence #2: presets.operation (0001_init.sql, an identical
-- CHECK) is deliberately NOT widened by this migration. No system or user
-- preset for 'sanitize' has been requested; the two enums (jobs.operation,
-- presets.operation) diverge on purpose and this is the acknowledgment, not
-- an oversight to fix later.
--
-- Blast radius: two additive ALTERs, no backfill, no data touched, no
-- existing constraint renamed, no index added. Applied against a database
-- with a pre-existing job_outputs row to confirm the additive-default claim
-- against live data, not merely a fresh schema.

ALTER TABLE job_outputs
    ADD COLUMN role text NOT NULL DEFAULT 'primary'
        CHECK (role IN ('primary', 'report', 'preview', 'thumbnail'));

ALTER TABLE jobs DROP CONSTRAINT jobs_operation_check;
ALTER TABLE jobs ADD CONSTRAINT jobs_operation_check
    CHECK (operation IN ('convert', 'extract', 'archive', 'inspect', 'render', 'sanitize'));
