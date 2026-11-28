ALTER TABLE "gb_device" ADD COLUMN "reported_charset" TEXT NOT NULL DEFAULT '';
ALTER TABLE "gb_device" ADD COLUMN "wire_charset" TEXT NOT NULL DEFAULT '';
ALTER TABLE "gb_device" ADD COLUMN "reported_charset_at" DATETIME NULL;
