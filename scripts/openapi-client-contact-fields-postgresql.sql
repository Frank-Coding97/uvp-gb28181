-- OpenAPI 客户端外部负责人字段增量升级。
-- 适用于已存在 sys_openapi_client 的 PostgreSQL 存量库；可重复执行。
ALTER TABLE sys_openapi_client
  ADD COLUMN IF NOT EXISTS responsible_org_name varchar(200) NOT NULL DEFAULT '';
ALTER TABLE sys_openapi_client
  ADD COLUMN IF NOT EXISTS responsible_name varchar(100) NOT NULL DEFAULT '';
ALTER TABLE sys_openapi_client
  ADD COLUMN IF NOT EXISTS responsible_contact varchar(100) NOT NULL DEFAULT '';
