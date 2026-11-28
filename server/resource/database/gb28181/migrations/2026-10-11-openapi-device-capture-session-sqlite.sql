CREATE TABLE IF NOT EXISTS "sys_openapi_device_capture_session" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "client_id" INTEGER NOT NULL,
  "idempotency_digest" TEXT NOT NULL CHECK (length("idempotency_digest") <= 64),
  "request_digest" TEXT NOT NULL CHECK (length("request_digest") <= 64),
  "session_id" TEXT NOT NULL CHECK (length("session_id") <= 36),
  "registry_session_id" TEXT NOT NULL DEFAULT '' CHECK (length("registry_session_id") <= 36),
  "operation_id" TEXT NOT NULL DEFAULT '' CHECK (length("operation_id") <= 64),
  "device_id" TEXT NOT NULL CHECK (length("device_id") <= 20),
  "channel_id" TEXT NOT NULL CHECK (length("channel_id") <= 20),
  "internal_device_id" INTEGER NOT NULL CHECK ("internal_device_id" >= 0),
  "internal_channel_id" INTEGER NOT NULL CHECK ("internal_channel_id" >= 0),
  "department_id" INTEGER NOT NULL CHECK ("department_id" >= 0),
  "snap_num" INTEGER NOT NULL,
  "interval_seconds" INTEGER NOT NULL,
  "state" TEXT NOT NULL CHECK (length("state") <= 24),
  "created_at" DATETIME NOT NULL,
  "updated_at" DATETIME NOT NULL,
  CONSTRAINT "uk_openapi_capture_idem" UNIQUE ("client_id", "idempotency_digest"),
  CONSTRAINT "uk_openapi_capture_session" UNIQUE ("session_id")
);
CREATE INDEX IF NOT EXISTS "idx_openapi_capture_registry_session" ON "sys_openapi_device_capture_session" ("registry_session_id");
CREATE INDEX IF NOT EXISTS "idx_openapi_capture_session_client" ON "sys_openapi_device_capture_session" ("client_id", "device_id", "channel_id");
CREATE INDEX IF NOT EXISTS "idx_openapi_capture_state_updated" ON "sys_openapi_device_capture_session" ("state", "created_at");
