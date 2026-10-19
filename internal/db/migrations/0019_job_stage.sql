ALTER TABLE jobs
    ADD COLUMN stage text,
    ADD COLUMN stage_index smallint,
    ADD COLUMN stage_count smallint,
    ADD CONSTRAINT jobs_stage_check CHECK (stage IN ('guard', 'mesh', 'render', 'dxf', 'ink', 'rsvg', 'judge')),
    ADD CONSTRAINT jobs_stage_shape_check CHECK ((stage IS NULL AND stage_index IS NULL AND stage_count IS NULL) OR (stage IS NOT NULL AND stage_index IS NOT NULL AND stage_count IS NOT NULL AND stage_index BETWEEN 1 AND stage_count));
