CREATE TABLE workflow_step_results (
    job_id       uuid NOT NULL REFERENCES jobs (id) ON DELETE CASCADE,
    step_id      uuid NOT NULL,
    attempt_id   uuid NOT NULL,
    object_key   text NOT NULL,
    format       text NOT NULL,
    size_bytes   bigint NOT NULL CHECK (size_bytes >= 0),
    content_type text NOT NULL,
    sha256       text NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (job_id, step_id)
);
