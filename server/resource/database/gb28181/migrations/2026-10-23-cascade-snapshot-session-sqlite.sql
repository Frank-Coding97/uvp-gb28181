CREATE TABLE IF NOT EXISTS "gb_cascade_snapshot_session" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "platform_id" INTEGER NOT NULL,
  "upstream_session_id" TEXT NOT NULL CHECK (length("upstream_session_id") <= 128),
  "request_key" TEXT NOT NULL CHECK (length("request_key") <= 64),
  "request_fingerprint" TEXT NOT NULL CHECK (length("request_fingerprint") <= 64),
  "request_result" TEXT NOT NULL DEFAULT '' CHECK (length("request_result") <= 8),
  "published_channel_id" TEXT NOT NULL CHECK (length("published_channel_id") <= 20),
  "source_device_id" INTEGER NOT NULL,
  "source_channel_id" INTEGER NOT NULL,
  "source_device_epoch" INTEGER NOT NULL DEFAULT 0,
  "config_revision" INTEGER NOT NULL,
  "projection_revision" INTEGER NOT NULL,
  "state" TEXT NOT NULL DEFAULT 'pending' CHECK (length("state") <= 16),
  "expires_at" DATETIME NOT NULL,
  "completed_at" DATETIME,
  "created_at" DATETIME NOT NULL,
  "updated_at" DATETIME NOT NULL,
  CONSTRAINT uk_cascade_snapshot_session UNIQUE ("platform_id", "upstream_session_id"),
  CONSTRAINT uk_cascade_snapshot_request UNIQUE ("platform_id", "request_key")
);
CREATE INDEX IF NOT EXISTS idx_cascade_snapshot_expiry ON "gb_cascade_snapshot_session" ("platform_id", "expires_at");
