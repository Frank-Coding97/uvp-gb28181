-- The migration runner probes this column before executing this additive migration.
ALTER TABLE "gb_cascade_platform" ADD COLUMN "upgrade_enabled" INTEGER NOT NULL DEFAULT 0 CHECK ("upgrade_enabled" IN (0, 1));
