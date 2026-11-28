-- 补齐 OpenAPI 鉴权锁初始行（降级前的 fail-closed 校验依赖它，见 down.go）。
-- 其余内容只有 SQLite 方言需要：图像库按钮权限等种子在 PG 侧由全量基线建库时写入。
INSERT INTO "sys_openapi_security_state" ("id", "must_auth_locked", "locked_at", "lock_version")
SELECT 1, false, NULL, 0
WHERE NOT EXISTS (SELECT 1 FROM "sys_openapi_security_state" WHERE "id" = 1);
