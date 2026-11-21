-- The migration runner probes this column before executing this additive migration.
ALTER TABLE "gb_cascade_platform" ADD COLUMN "rtcp_keepalive" INTEGER NOT NULL DEFAULT 0 CHECK ("rtcp_keepalive" IN (0, 1));
