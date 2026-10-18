-- The migration runner probes this column before executing this additive migration.
ALTER TABLE "gb_cascade_platform" ADD COLUMN "auto_push_catalog" INTEGER NOT NULL DEFAULT 0 CHECK ("auto_push_catalog" IN (0, 1));
