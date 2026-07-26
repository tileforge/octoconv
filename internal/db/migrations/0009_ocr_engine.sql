-- Add 'ocr' to the jobs.engine allow-list (OCRP-01, Phase 44).
--
-- Hard prerequisite for the sixth (OCR/tesseract) engine class: no
-- engine="ocr" job row can be created until this constraint accepts the
-- value. The constraint name jobs_engine_check is Postgres's auto-generated
-- name for the inline, unnamed column CHECK declared in 0001_init.sql (the
-- standard <table>_<column>_check convention for an unnamed CHECK), already
-- confirmed live by the 0005_html_engine.sql/0006_audio_engine.sql
-- migrations this one mirrors. Additive only -- the jobs.operation CHECK is
-- untouched: OCR jobs use operation='convert', already valid since
-- 0001_init.sql (D-02, 44-CONTEXT.md); no free-text engine value is ever
-- admitted outside this closed allow-list.
ALTER TABLE jobs DROP CONSTRAINT jobs_engine_check;
ALTER TABLE jobs ADD CONSTRAINT jobs_engine_check
    CHECK (engine IN ('image', 'document', 'av', 'cad', 'archive', 'probe', 'html', 'audio', 'ocr'));
