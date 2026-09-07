-- RTP-only fixed evidence. NULL preserves unknown legacy history.
-- TEXT preserves canonical bytes; the application validates the full contract.
ALTER TABLE gb_device_operation_intent ADD COLUMN IF NOT EXISTS rtp_steps_json TEXT NULL;
DO $$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'gb_device_operation_intent'::regclass AND conname = 'ck_device_intent_rtp_size') THEN ALTER TABLE gb_device_operation_intent ADD CONSTRAINT ck_device_intent_rtp_size CHECK (rtp_steps_json IS NULL OR OCTET_LENGTH(rtp_steps_json) BETWEEN 1 AND 32768); END IF; END $$;
