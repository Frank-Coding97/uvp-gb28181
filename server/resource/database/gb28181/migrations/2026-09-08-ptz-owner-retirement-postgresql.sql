-- Preserve old rows without fabricating a process-retirement certificate.
ALTER TABLE gb_ptz_operation_attempt ADD COLUMN IF NOT EXISTS retired_by_process_id VARCHAR(32) COLLATE "C" NULL;
ALTER TABLE gb_ptz_operation_attempt ADD COLUMN IF NOT EXISTS retired_at TIMESTAMP(3) NULL;
DO $$ BEGIN
IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='gb_ptz_operation_attempt'::regclass AND conname='ck_ptz_attempt_retirement') THEN
ALTER TABLE gb_ptz_operation_attempt ADD CONSTRAINT ck_ptz_attempt_retirement CHECK ((retired_by_process_id IS NULL AND retired_at IS NULL) OR (retired_by_process_id IS NOT NULL AND retired_at IS NOT NULL AND owner_process_id IS NOT NULL AND owner_run_id IS NOT NULL AND retired_by_process_id <> owner_process_id AND local_quiesced_at IS NULL));
END IF;
END $$;
