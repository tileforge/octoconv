-- Give job_outputs a logical identity, and index the retention scan (PR #4 review).
--
-- Two independent fixes, both against Phase 51's accounting/retention work.
--
-- (1) UNIQUE (job_id, ordinal) on job_outputs.
--
-- recordUsage sums `size_bytes` over EVERY job_outputs row for the job -- it
-- deliberately does not filter on ordinal, so that a future multi-output job
-- (SEED-017 PDF split) is not silently under-counted. That is correct only if
-- one logical output is one row. It was not: AddOutput was an unconditional
-- INSERT, and MarkActive explicitly permits active -> active so a same-task
-- asynq retry can re-enter the handler. So the sequence
--
--     AddOutput commits -> MarkDone fails transiently -> asynq retries
--     -> MarkActive (active -> active, allowed) -> convert reruns
--     -> AddOutput inserts a SECOND row with the same ordinal
--
-- left two rows for one logical output, and the eventual successful MarkDone
-- summed both -- permanently overstating bytes_out in the authoritative
-- rollup. The rollup is written inside the terminal transition transaction and
-- is never revisited, so the overstatement is not self-healing.
--
-- (job_id, ordinal) is the logical identity: a retry regenerates the same
-- ordinals, and a multi-output job assigns distinct ones. AddOutput becomes an
-- upsert against this constraint (internal/jobs/repo.go).
--
-- The DELETE below is required before the constraint can be added: duplicates
-- may already exist in a live database from exactly the retry path described
-- above. It keeps the newest row per (job_id, ordinal) -- the newest is the
-- one whose object_key points at the output the last successful conversion
-- actually uploaded. `id` breaks ties when created_at collides.
--
-- (2) jobs_retention_idx.
--
-- PurgeJobs filters and orders the terminal-job population by created_at every
-- sweep. No existing index serves it: jobs_client_created_idx is scoped by
-- client_id, and jobs_inflight_idx's predicate covers precisely the queued/
-- active statuses this query EXCLUDES. Without this index the batch limit
-- bounds the deletion but not the scan and sort, which ran every tick.
--
-- The predicate mirrors PurgeJobs' status filter exactly. If that filter ever
-- changes, this index stops being usable and must change with it.
--
-- Blast radius: one additive constraint, one additive index, plus a delete
-- that only ever removes rows that are duplicates by the new identity.

DELETE FROM job_outputs a
USING job_outputs b
WHERE a.job_id = b.job_id
  AND a.ordinal = b.ordinal
  AND (a.created_at, a.id) < (b.created_at, b.id);

ALTER TABLE job_outputs
    ADD CONSTRAINT job_outputs_job_ordinal_key UNIQUE (job_id, ordinal);

CREATE INDEX jobs_retention_idx ON jobs (created_at)
    WHERE status NOT IN ('queued', 'active');
