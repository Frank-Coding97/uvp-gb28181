CREATE TABLE IF NOT EXISTS "sys_openapi_device_subscription" (
  "id" VARCHAR(36) PRIMARY KEY,
  "client_id" BIGINT NOT NULL,
  "device_id" BIGINT NOT NULL,
  "kind" VARCHAR(32) NOT NULL,
  "enabled" BOOLEAN NOT NULL DEFAULT TRUE,
  "expires_seconds" INTEGER NOT NULL DEFAULT 3600,
  "expires_at" TIMESTAMP(3) NOT NULL,
  "created_at" TIMESTAMP(3) NOT NULL,
  "updated_at" TIMESTAMP(3) NOT NULL,
  CONSTRAINT uk_openapi_device_subscription UNIQUE ("client_id", "device_id", "kind")
);
CREATE INDEX IF NOT EXISTS idx_openapi_device_sub_client ON "sys_openapi_device_subscription" ("client_id");
CREATE INDEX IF NOT EXISTS idx_openapi_device_sub_enabled ON "sys_openapi_device_subscription" ("enabled");
CREATE INDEX IF NOT EXISTS idx_openapi_device_sub_expires ON "sys_openapi_device_subscription" ("expires_at");
