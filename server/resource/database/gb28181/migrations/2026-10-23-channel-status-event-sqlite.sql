-- The migration runner probes this column before executing this additive migration.
ALTER TABLE "gb_channel" ADD COLUMN "status_event" TEXT NOT NULL DEFAULT '' CHECK (length("status_event") <= 8);
