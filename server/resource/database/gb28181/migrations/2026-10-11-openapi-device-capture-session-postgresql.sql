CREATE TABLE IF NOT EXISTS "sys_openapi_device_capture_session" (
  "id" BIGSERIAL PRIMARY KEY,
  "client_id" BIGINT NOT NULL,
  "idempotency_digest" VARCHAR(64) NOT NULL,
  "request_digest" VARCHAR(64) NOT NULL,
  "session_id" VARCHAR(36) NOT NULL,
  "registry_session_id" VARCHAR(36) NOT NULL DEFAULT '',
  "operation_id" VARCHAR(64) NOT NULL DEFAULT '',
  "device_id" VARCHAR(20) NOT NULL,
  "channel_id" VARCHAR(20) NOT NULL,
  "internal_device_id" BIGINT NOT NULL,
  "internal_channel_id" BIGINT NOT NULL,
  "department_id" BIGINT NOT NULL,
  "snap_num" INTEGER NOT NULL,
  "interval_seconds" INTEGER NOT NULL,
  "state" VARCHAR(24) NOT NULL,
  "created_at" TIMESTAMP(3) NOT NULL,
  "updated_at" TIMESTAMP(3) NOT NULL,
  CONSTRAINT uk_openapi_capture_idem UNIQUE ("client_id", "idempotency_digest"),
  CONSTRAINT uk_openapi_capture_session UNIQUE ("session_id")
);
CREATE INDEX IF NOT EXISTS idx_openapi_capture_registry_session ON "sys_openapi_device_capture_session" ("registry_session_id");
CREATE INDEX IF NOT EXISTS idx_openapi_capture_session_client ON "sys_openapi_device_capture_session" ("client_id", "device_id", "channel_id");
CREATE INDEX IF NOT EXISTS idx_openapi_capture_state_updated ON "sys_openapi_device_capture_session" ("state", "created_at");
