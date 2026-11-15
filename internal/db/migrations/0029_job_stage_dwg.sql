ALTER TABLE jobs DROP CONSTRAINT jobs_stage_check;
ALTER TABLE jobs
    ADD CONSTRAINT jobs_stage_check CHECK (stage IN ('guard', 'mesh', 'render', 'dxf', 'dwg', 'ink', 'rsvg', 'judge'));
