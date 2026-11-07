ALTER TABLE worker_slot_holds
    ADD COLUMN staging_removed_at timestamptz,
    ADD COLUMN staging_attempted_at timestamptz;
