ALTER TABLE worker_slot_holds
    ADD COLUMN job_id uuid,
    ADD COLUMN step_id uuid,
    ADD COLUMN capability_id text,
    ADD COLUMN capability_version integer,
    ADD COLUMN lease_expires_at timestamptz,
    ADD COLUMN attempt_deadline timestamptz,
    ADD COLUMN state text,
    ADD COLUMN progress_percent smallint,
    ADD COLUMN progress_unknown boolean,
    ADD COLUMN stage text,
    ADD COLUMN stage_index integer,
    ADD COLUMN stage_count integer,
    ADD COLUMN eta_seconds integer,
    ADD COLUMN progress_window_started timestamptz,
    ADD COLUMN failure_count integer NOT NULL DEFAULT 0,
    ADD COLUMN progress_in_window integer NOT NULL DEFAULT 0,
    ADD CONSTRAINT worker_slot_holds_state_check
        CHECK (state IS NULL OR state IN ('active', 'stopped', 'failed', 'canceled'));

CREATE UNIQUE INDEX worker_slot_holds_one_active_step
    ON worker_slot_holds (step_id)
    WHERE released_at IS NULL AND step_id IS NOT NULL;

ALTER TABLE worker_request_dedup
    ADD COLUMN body_sha256 text,
    ADD COLUMN response_body bytea,
    ADD COLUMN attempt_id uuid,
    ADD COLUMN created_at timestamptz NOT NULL DEFAULT now();

ALTER TABLE worker_identities
    ADD COLUMN max_slots integer NOT NULL DEFAULT 4 CHECK (max_slots >= 0);

ALTER TABLE worker_sessions
    ADD COLUMN pull_window_started timestamptz,
    ADD COLUMN pulls_in_window integer NOT NULL DEFAULT 0;

ALTER TABLE jobs
    ADD COLUMN step_id uuid,
    ADD COLUMN protocol_deadline timestamptz,
    ADD COLUMN not_before timestamptz;

CREATE INDEX jobs_protocol_queued_idx
    ON jobs (created_at, id)
    WHERE dispatch_owner = 'protocol' AND status = 'queued';
