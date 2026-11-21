-- 存量库补齐 sys_openapi_security_state 初始行。
--
-- 背景：`-migrate-down` 在动手前会校验这张表「恰好 1 行且是未上锁状态」
-- （见 app/gb28181/migration/down.go），缺行时降级会被 fail-closed 拦下。
-- 全新安装由全量脚本写入；升级上来的库如果这张表是空的，降级路径就是断的。
--
-- ⚠️ 本迁移的其余内容（图像库按钮权限 / 批量下载接口 / 一键清理菜单）只有
--    SQLite 方言需要 —— 那批种子在 MySQL 侧早由 2026-11-04-snapshot-*.sql 补过。
INSERT INTO `sys_openapi_security_state` (`id`, `must_auth_locked`, `locked_at`, `lock_version`)
SELECT 1, 0, NULL, 0 FROM DUAL
WHERE NOT EXISTS (SELECT 1 FROM `sys_openapi_security_state` WHERE `id` = 1);
