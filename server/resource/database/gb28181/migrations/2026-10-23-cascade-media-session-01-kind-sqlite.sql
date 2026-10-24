-- The migration runner probes this column before executing this additive migration.
ALTER TABLE "gb_cascade_media_session" ADD COLUMN "session_kind" TEXT NOT NULL DEFAULT 'video' CHECK (length("session_kind") <= 16);
