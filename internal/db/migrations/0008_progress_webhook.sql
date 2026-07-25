-- Add the per-job opt-in progress-webhook flag (PUSH-02, D-03, v1.9).
--
-- Pure-additive, zero-behavior-change schema foundation: progress_webhook is
-- delivery configuration (whether the worker fires a best-effort inline
-- progress webhook alongside the existing terminal done/failed webhook),
-- deliberately NOT stored in the jobs.options jsonb column (delivery config
-- is orthogonal to per-engine conversion parameters, D-03). NOT NULL DEFAULT
-- false means every existing row reads false and every existing INSERT that
-- omits the column still succeeds unchanged -- mirrors the verified
-- 0007_job_progress.sql zero-behavior-change precedent. ADD COLUMN ...
-- DEFAULT false is metadata-only in Postgres 18 (no full-table rewrite), so
-- this is safe to apply against a populated jobs table.
ALTER TABLE jobs
    ADD COLUMN progress_webhook boolean NOT NULL DEFAULT false;
