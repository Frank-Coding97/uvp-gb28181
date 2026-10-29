ALTER TABLE "gb_cascade_platform" ADD COLUMN IF NOT EXISTS "upgrade_enabled" BOOLEAN NOT NULL DEFAULT false;
