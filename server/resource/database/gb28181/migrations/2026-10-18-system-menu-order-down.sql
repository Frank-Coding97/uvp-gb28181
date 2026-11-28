-- 回滚 2026-10-18-system-menu-order：系统管理子菜单 sort 还原成 0（服务升级还原成 90），
-- 「代码生成」「文件管理」重新显示（hide=0）。

UPDATE sys_menu SET sort=0 WHERE path='/system/account' AND deleted_at IS NULL;
UPDATE sys_menu SET sort=0 WHERE path='/system/role' AND deleted_at IS NULL;
UPDATE sys_menu SET sort=0 WHERE path='/system/menu' AND deleted_at IS NULL;
UPDATE sys_menu SET sort=0 WHERE path='/system/division' AND deleted_at IS NULL;
UPDATE sys_menu SET sort=0 WHERE path='/system/online-user' AND deleted_at IS NULL;
UPDATE sys_menu SET sort=0 WHERE path='/system/dictionary' AND deleted_at IS NULL;
UPDATE sys_menu SET sort=0 WHERE path='/system/sysconfig' AND deleted_at IS NULL;
UPDATE sys_menu SET sort=0 WHERE path='/system/api' AND deleted_at IS NULL;
UPDATE sys_menu SET sort=0 WHERE path='/system/affix' AND deleted_at IS NULL;
UPDATE sys_menu SET sort=90 WHERE path='/system/service-upgrade/index' AND deleted_at IS NULL;
UPDATE sys_menu SET sort=0 WHERE path='/system/codegen' AND deleted_at IS NULL;
UPDATE sys_menu SET sort=0 WHERE path='/system/pluginsmanager' AND deleted_at IS NULL;
UPDATE sys_menu SET sort=0 WHERE path='/system/userinfo' AND deleted_at IS NULL;
UPDATE sys_menu SET hide=0 WHERE path='/system/affix' AND deleted_at IS NULL;
UPDATE sys_menu SET hide=0 WHERE path='/system/codegen' AND deleted_at IS NULL;
