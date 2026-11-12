ALTER TABLE "gb_channel" ADD COLUMN "stream_identification" TEXT NOT NULL DEFAULT '' CHECK (length("stream_identification") <= 32);
