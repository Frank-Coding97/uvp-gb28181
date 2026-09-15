-- drop-catalog-anomaly-menu:start
-- 物理删除「目录异常」菜单(MySQL 5.7+,可重复执行)。
-- 该菜单的页面 gb28181/device-mgmt/anomaly/index 只是设备管理页的 re-export,
-- 与 /gb28181/device-mgmt/index 打开的是同一个页面,保留会造成重复入口与误解。
-- 本次只清理菜单本身及其权限绑定;gb_anomaly_record 数据表与 catalog 写入管道保持不变。
DELETE FROM `sys_casbin_rule` WHERE `v1` IN ('/api/gb28181/device-mgmt/anomaly','/api/gb28181/device-mgmt/anomaly/:id/resolve','/api/gb28181/device-mgmt/anomaly/batch-resolve');
DELETE ma FROM `sys_menu_api` ma JOIN `sys_menu` m ON m.`id`=ma.`menu_id` WHERE m.`path`='/gb28181/device-mgmt/anomaly';
DELETE FROM `sys_role_menu` WHERE `menu_id` IN (SELECT `id` FROM `sys_menu` WHERE `path`='/gb28181/device-mgmt/anomaly');
DELETE FROM `sys_menu` WHERE `path`='/gb28181/device-mgmt/anomaly';
-- drop-catalog-anomaly-menu:end
