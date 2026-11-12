ALTER TABLE "gb_channel" ADD COLUMN IF NOT EXISTS "current_stream_identification" VARCHAR(32) DEFAULT NULL;
