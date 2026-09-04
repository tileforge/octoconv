-- Add 'tectonic' to the jobs.engine allow-list (TEX-05, Phase 64).
--
-- Hard prerequisite for the ninth (typesetting) engine class: no
-- engine='tectonic' job row can be created until this constraint accepts the
-- value, and without it every latex->pdf job would fail on a constraint
-- violation that says nothing about the engine being new. The constraint name
-- jobs_engine_check is Postgres's auto-generated name for the inline, unnamed
-- column CHECK declared in 0001_init.sql, already confirmed live by the
-- 0005/0006/0009/0010 migrations this one mirrors.
--
-- Additive only. jobs.operation is untouched: tectonic jobs use
-- operation='convert', valid since 0001_init.sql. client_limits.engine and
-- client_usage_daily.engine deliberately carry NO allow-list (both migrations
-- say so in as many words), so they need no change -- stated here so nobody
-- goes looking for a second migration that does not exist.
ALTER TABLE jobs DROP CONSTRAINT jobs_engine_check;
ALTER TABLE jobs ADD CONSTRAINT jobs_engine_check
    CHECK (engine IN ('image', 'document', 'av', 'cad', 'archive', 'probe', 'html', 'audio', 'ocr', 'pandoc', 'tectonic'));
