-- Validate separately so the table scan does not retain the exclusive lock
-- required by the preceding constraint replacement transaction.
ALTER TABLE presets VALIDATE CONSTRAINT presets_operation_check;
