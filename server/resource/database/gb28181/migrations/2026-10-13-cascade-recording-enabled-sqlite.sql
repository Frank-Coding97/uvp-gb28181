-- The migration runner probes this column before executing this additive migration.
ALTER TABLE "gb_cascade_platform" ADD COLUMN "recording_enabled" INTEGER NOT NULL DEFAULT 0 CHECK ("recording_enabled" IN (0, 1));
