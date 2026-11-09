ALTER TABLE worker_sessions
    ADD COLUMN last_heartbeat_at timestamptz,
    ADD COLUMN drain_at timestamptz;
