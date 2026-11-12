ALTER TABLE "gb_cascade_platform" ADD COLUMN IF NOT EXISTS "maintenance_enabled" BOOLEAN NOT NULL DEFAULT false;
