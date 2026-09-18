-- Device access cleanup barrier (PostgreSQL 12+).
-- access_epoch is the requested waterline. A newly added completed waterline
-- intentionally stays at 1, so upgraded rows with access_epoch > 1 remain
-- pending until the cleanup worker records completion.

DO $$ BEGIN IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = 'gb_device' AND column_name = 'access_epoch') THEN RAISE EXCEPTION 'device cleanup barrier requires gb_device.access_epoch'; END IF; END $$;
ALTER TABLE gb_device ADD COLUMN IF NOT EXISTS cleanup_completed_epoch BIGINT NOT NULL DEFAULT 1;
DO $$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'gb_device'::regclass AND conname = 'ck_gb_device_cleanup_completed_epoch') THEN ALTER TABLE gb_device ADD CONSTRAINT ck_gb_device_cleanup_completed_epoch CHECK (cleanup_completed_epoch > 0 AND cleanup_completed_epoch <= access_epoch); END IF; END $$;
