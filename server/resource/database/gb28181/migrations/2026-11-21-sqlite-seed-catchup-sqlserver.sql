-- 补齐 OpenAPI 鉴权锁初始行（降级前的 fail-closed 校验依赖它，见 down.go）。
-- 其余内容只有 SQLite 方言需要：图像库按钮权限等种子在 SQL Server 侧由全量基线建库时写入。
IF NOT EXISTS (SELECT 1 FROM [sys_openapi_security_state] WHERE [id] = 1)
  INSERT INTO [sys_openapi_security_state] ([id], [must_auth_locked], [locked_at], [lock_version])
  VALUES (1, 0, NULL, 0);
