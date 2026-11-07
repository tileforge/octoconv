CREATE TABLE worker_identities (
    worker_id        uuid PRIMARY KEY,
    manifest_sha256  text NOT NULL CHECK (manifest_sha256 ~ '^[0-9a-f]{64}$'),
    manifest_bytes   bytea NOT NULL,
    approved_at      timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE worker_capabilities (
    id                text NOT NULL,
    version           integer NOT NULL CHECK (version >= 1),
    worker_id         uuid NOT NULL UNIQUE REFERENCES worker_identities (worker_id),
    client_operation  text NOT NULL,
    legacy_operation  text,
    legacy_engine     text,
    parameter_schema  jsonb NOT NULL,
    PRIMARY KEY (id, version),
    CHECK (
        (legacy_operation IS NULL AND legacy_engine IS NULL)
        OR (legacy_operation IS NOT NULL AND legacy_engine IS NOT NULL)
    ),
    CHECK (
        legacy_engine IS NULL OR legacy_engine IN (
            'image', 'document', 'av', 'cad', 'archive', 'probe',
            'html', 'audio', 'ocr', 'pandoc', 'tectonic'
        )
    )
);

CREATE TABLE worker_capability_formats (
    capability_id       text NOT NULL,
    capability_version  integer NOT NULL,
    source_format       text NOT NULL,
    target_format       text NOT NULL,
    client_operation    text NOT NULL,
    PRIMARY KEY (capability_id, capability_version, source_format, target_format),
    FOREIGN KEY (capability_id, capability_version)
        REFERENCES worker_capabilities (id, version),
    UNIQUE (client_operation, source_format, target_format)
);

CREATE TABLE worker_certificate_denylist (
    issuer_sha256    text NOT NULL,
    serial_decimal   text NOT NULL,
    worker_id        uuid NOT NULL,
    revoked_at       timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (issuer_sha256, serial_decimal)
);

CREATE TABLE worker_sessions (
    id                   uuid PRIMARY KEY,
    worker_id            uuid NOT NULL REFERENCES worker_identities (worker_id),
    token_hash           text NOT NULL UNIQUE,
    cert_serial_decimal  text NOT NULL,
    issuer_sha256        text NOT NULL,
    free_slots           integer NOT NULL CHECK (free_slots >= 0),
    opened_at            timestamptz NOT NULL DEFAULT now(),
    expires_at           timestamptz NOT NULL,
    closed_at            timestamptz
);

CREATE TABLE worker_attempt_credentials (
    attempt_id           uuid PRIMARY KEY,
    token_hash           text NOT NULL UNIQUE,
    session_id           uuid NOT NULL REFERENCES worker_sessions (id),
    capability_id        text NOT NULL,
    capability_version   integer NOT NULL CHECK (capability_version >= 1)
);

CREATE TABLE worker_slot_holds (
    id           uuid PRIMARY KEY,
    owner_key    text NOT NULL,
    session_id   uuid NOT NULL,
    released_at  timestamptz
);

CREATE TABLE worker_request_dedup (
    session_id  uuid NOT NULL,
    dedup_key   text NOT NULL,
    PRIMARY KEY (session_id, dedup_key)
);

ALTER TABLE jobs
    ADD COLUMN dispatch_owner text NOT NULL DEFAULT 'legacy'
        CHECK (dispatch_owner IN ('legacy', 'protocol')),
    ADD COLUMN capability_id text,
    ADD COLUMN capability_version integer;
