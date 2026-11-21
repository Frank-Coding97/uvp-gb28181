CREATE TABLE IF NOT EXISTS "gb_cascade_alarm_delivery" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "delivery_key" TEXT NOT NULL CONSTRAINT "uk_cascade_alarm_delivery_key" UNIQUE,
  "platform_id" INTEGER NOT NULL,
  "source_device_code" TEXT NOT NULL,
  "source_resource_code" TEXT NOT NULL,
  "published_code" TEXT NOT NULL,
  "profile_version" TEXT NOT NULL,
  "binding_fingerprint" TEXT NOT NULL,
  "payload" BLOB NOT NULL,
  "notify_targets" TEXT NULL,
  "state" TEXT NOT NULL DEFAULT 'pending',
  "attempts" INTEGER NOT NULL DEFAULT 0,
  "next_attempt_at" DATETIME NOT NULL,
  "lease_owner" TEXT NOT NULL DEFAULT '',
  "lease_until" DATETIME NULL,
  "last_error" TEXT NULL,
  "sent_at" DATETIME NULL,
  "created_at" DATETIME NOT NULL,
  "updated_at" DATETIME NOT NULL
);
CREATE INDEX IF NOT EXISTS "idx_cascade_alarm_delivery_due" ON "gb_cascade_alarm_delivery" ("platform_id", "state", "next_attempt_at");
CREATE INDEX IF NOT EXISTS "idx_cascade_alarm_delivery_lease" ON "gb_cascade_alarm_delivery" ("lease_until");
