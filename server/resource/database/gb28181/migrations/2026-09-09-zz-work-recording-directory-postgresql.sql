-- Persist the independent server-side recording directory and claim version.
-- Existing work-recording and recorder-claim tables are required; no table is created here.
-- PostgreSQL 12+, repeatable.
DO $$ BEGIN IF to_regclass('gb_work_recording') IS NULL OR to_regclass('gb_recorder_claim') IS NULL THEN RAISE EXCEPTION 'gb_work_recording and gb_recorder_claim must exist before work-recording directory migration'; END IF; END $$;

ALTER TABLE gb_work_recording
  ADD COLUMN IF NOT EXISTS recording_root VARCHAR(1024) NOT NULL DEFAULT '';
ALTER TABLE gb_work_recording
  ADD COLUMN IF NOT EXISTS recorder_claim_version BIGINT NOT NULL DEFAULT 0;
ALTER TABLE gb_recorder_claim
  ADD COLUMN IF NOT EXISTS recording_root VARCHAR(1024) NOT NULL DEFAULT '';
