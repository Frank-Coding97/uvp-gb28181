CREATE TABLE IF NOT EXISTS "sys_openapi_device_subscription" (
  "id" TEXT NOT NULL CHECK (length("id") <= 36) PRIMARY KEY,
  "client_id" INTEGER NOT NULL,
  "device_id" INTEGER NOT NULL CHECK ("device_id" >= 0),
  "kind" TEXT NOT NULL CHECK (length("kind") <= 32),
  "enabled" BOOLEAN NOT NULL DEFAULT TRUE,
  "expires_seconds" INTEGER NOT NULL DEFAULT 3600,
  "expires_at" DATETIME NOT NULL,
  "created_at" DATETIME NOT NULL,
  "updated_at" DATETIME NOT NULL,
  CONSTRAINT "uk_openapi_device_subscription" UNIQUE ("client_id", "device_id", "kind")
);
CREATE INDEX IF NOT EXISTS "idx_openapi_device_sub_client" ON "sys_openapi_device_subscription" ("client_id");
CREATE INDEX IF NOT EXISTS "idx_openapi_device_sub_enabled" ON "sys_openapi_device_subscription" ("enabled");
CREATE INDEX IF NOT EXISTS "idx_openapi_device_sub_expires" ON "sys_openapi_device_subscription" ("expires_at");
