-- OpenAPI 客户端外部负责人字段增量升级。
-- 适用于已存在 sys_openapi_client 的 MySQL 8.x 存量库；可重复执行。
ALTER TABLE sys_openapi_client
  ADD COLUMN IF NOT EXISTS responsible_org_name varchar(200) COLLATE utf8mb4_bin NOT NULL DEFAULT '' AFTER responsible_user_id,
  ADD COLUMN IF NOT EXISTS responsible_name varchar(100) COLLATE utf8mb4_bin NOT NULL DEFAULT '' AFTER responsible_org_name,
  ADD COLUMN IF NOT EXISTS responsible_contact varchar(100) COLLATE utf8mb4_bin NOT NULL DEFAULT '' AFTER responsible_name;
