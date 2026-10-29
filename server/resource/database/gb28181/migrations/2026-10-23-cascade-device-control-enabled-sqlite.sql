ALTER TABLE "gb_cascade_platform" ADD COLUMN "device_control_enabled" INTEGER NOT NULL DEFAULT 0 CHECK ("device_control_enabled" IN (0, 1));
