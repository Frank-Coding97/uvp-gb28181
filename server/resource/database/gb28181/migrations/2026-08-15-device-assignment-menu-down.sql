-- 回滚设备分配菜单/按钮/角色绑定(MySQL,软删)
UPDATE `sys_menu` SET `deleted_at`=NOW() WHERE `name` IN ('device-assignment','device-assignment-assign','device-assignment-share') AND `deleted_at` IS NULL;
