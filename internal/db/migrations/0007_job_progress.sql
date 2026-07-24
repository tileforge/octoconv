-- Add durable progress/ETA/duration/throttle columns to jobs (PROG-02, v1.9 foundation).
--
-- Pure-additive, zero-behavior-change schema foundation for the risky downstream
-- phases: Phase 40 reads progress_pct in the job listing; Phase 41 writes
-- progress_pct/eta_seconds/progress_updated_at on every progress tick and
-- persists source_duration_seconds (the ffprobe-derived percent denominator).
-- All four columns are nullable with no fallback value assigned, so every
-- existing INSERT that omits them still succeeds and every existing row simply reads NULL --
-- GET /v1/jobs/{id} and POST /v1/jobs are unaffected. `canceled` is already in
-- the jobs.status CHECK since 0001_init.sql and needs no change here, nor does
-- the existing jobs_client_created_idx listing index (D-07 in 38-CONTEXT.md).
ALTER TABLE jobs
    ADD COLUMN progress_pct             smallint CHECK (progress_pct BETWEEN 0 AND 100),
    ADD COLUMN eta_seconds              int      CHECK (eta_seconds >= 0),
    ADD COLUMN source_duration_seconds  double precision CHECK (source_duration_seconds > 0),
    ADD COLUMN progress_updated_at      timestamptz;
