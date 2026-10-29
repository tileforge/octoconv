-- Broaden the existing preset operation constraint; preserve all rows and versions.
-- Forward-only migration, like the existing runner. A manual downgrade must
-- refuse incompatible sanitize rows rather than rewrite or delete settings.
ALTER TABLE presets DROP CONSTRAINT presets_operation_check;
ALTER TABLE presets ADD CONSTRAINT presets_operation_check
    CHECK (operation IN ('convert', 'extract', 'archive', 'inspect', 'render', 'sanitize')) NOT VALID;
