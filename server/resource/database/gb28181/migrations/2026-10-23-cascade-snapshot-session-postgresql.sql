CREATE TABLE IF NOT EXISTS "gb_cascade_snapshot_session" (
  "id" BIGSERIAL PRIMARY KEY,
  "platform_id" BIGINT NOT NULL,
  "upstream_session_id" VARCHAR(128) NOT NULL,
  "request_key" VARCHAR(64) NOT NULL,
  "request_fingerprint" VARCHAR(64) NOT NULL,
  "request_result" VARCHAR(8) NOT NULL DEFAULT '',
  "published_channel_id" VARCHAR(20) NOT NULL,
  "source_device_id" BIGINT NOT NULL,
  "source_channel_id" BIGINT NOT NULL,
  "source_device_epoch" BIGINT NOT NULL DEFAULT 0,
  "config_revision" BIGINT NOT NULL,
  "projection_revision" BIGINT NOT NULL,
  "state" VARCHAR(16) NOT NULL DEFAULT 'pending',
  "expires_at" TIMESTAMP(3) NOT NULL,
  "completed_at" TIMESTAMP(3),
  "created_at" TIMESTAMP(3) NOT NULL,
  "updated_at" TIMESTAMP(3) NOT NULL,
  CONSTRAINT uk_cascade_snapshot_session UNIQUE ("platform_id", "upstream_session_id"),
  CONSTRAINT uk_cascade_snapshot_request UNIQUE ("platform_id", "request_key")
);
CREATE INDEX IF NOT EXISTS idx_cascade_snapshot_expiry ON "gb_cascade_snapshot_session" ("platform_id", "expires_at");
