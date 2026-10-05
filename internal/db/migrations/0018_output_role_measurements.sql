ALTER TABLE job_outputs DROP CONSTRAINT job_outputs_role_check;
ALTER TABLE job_outputs ADD CONSTRAINT job_outputs_role_check
    CHECK (role IN ('primary', 'report', 'preview', 'thumbnail', 'measurements'));
