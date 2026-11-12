ALTER TABLE "gb_channel" ADD COLUMN "current_stream_identification" TEXT NULL CHECK ("current_stream_identification" IS NULL OR length("current_stream_identification") <= 32);
