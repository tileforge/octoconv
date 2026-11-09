CREATE TABLE workflow_definitions (
    name       text NOT NULL,
    version    integer NOT NULL CHECK (version >= 1),
    document   jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (name, version)
);

CREATE TABLE workflow_grants (
    name       text NOT NULL,
    version    integer NOT NULL,
    client_id  uuid NOT NULL REFERENCES clients (id),
    PRIMARY KEY (name, version, client_id),
    FOREIGN KEY (name, version) REFERENCES workflow_definitions (name, version)
);

CREATE TABLE worker_capability_ports (
    capability_id      text NOT NULL,
    capability_version integer NOT NULL,
    direction          text NOT NULL CHECK (direction IN ('input', 'output')),
    role               text NOT NULL,
    min_count          integer NOT NULL CHECK (min_count >= 0),
    max_count          integer NOT NULL CHECK (max_count >= 0),
    ordinal            integer NOT NULL CHECK (ordinal >= 0),
    PRIMARY KEY (capability_id, capability_version, direction, ordinal),
    FOREIGN KEY (capability_id, capability_version)
        REFERENCES worker_capabilities (id, version)
        ON DELETE CASCADE
);

CREATE TABLE workflow_snapshots (
    job_id             uuid PRIMARY KEY,
    definition_name    text NOT NULL,
    definition_version integer NOT NULL,
    document           jsonb NOT NULL,
    created_at         timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE jobs
    ADD COLUMN workflow_snapshot_id uuid,
    ADD CONSTRAINT jobs_workflow_snapshot_id_self_check
        CHECK (workflow_snapshot_id IS NULL OR workflow_snapshot_id = id);

ALTER TABLE workflow_snapshots
    ADD CONSTRAINT workflow_snapshots_job_id_fkey
    FOREIGN KEY (job_id) REFERENCES jobs (id)
    ON DELETE CASCADE;

ALTER TABLE jobs
    ADD CONSTRAINT jobs_workflow_snapshot_id_fkey
    FOREIGN KEY (workflow_snapshot_id) REFERENCES workflow_snapshots (job_id);

ALTER TABLE jobs
    ADD COLUMN admitted_input_bytes bigint,
    ADD CONSTRAINT jobs_admitted_input_bytes_nonnegative
        CHECK (admitted_input_bytes IS NULL OR admitted_input_bytes >= 0);
