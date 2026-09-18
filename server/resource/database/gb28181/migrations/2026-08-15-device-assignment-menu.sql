-- 设备分配菜单 + 按钮权限 + 角色绑定(MySQL 5.7+,幂等)。

-- 1. 菜单"设备分配"(type=2),挂在根级(与"设备列表"同级平铺)
INSERT INTO `sys_menu` (`parent_id`,`path`,`name`,`redirect`,`component`,`title`,`is_full`,`hide`,`disable`,`keep_alive`,`affix`,`is_link`,`link`,`iframe`,`svg_icon`,`icon`,`sort`,`type`,`permission`,`created_by`,`created_at`,`updated_at`)
SELECT 0,'/gb28181/device-assignment','device-assignment','','gb28181/device-assignment/index','设备分配',0,0,0,0,0,0,'',0,'','lucide:KeyRound',9,2,'',1,NOW(),NOW()
WHERE NOT EXISTS (SELECT 1 FROM `sys_menu` WHERE `name`='device-assignment' AND `deleted_at` IS NULL);

-- 2. 按钮权限:分配设备归属(type=3)
INSERT INTO `sys_menu` (`parent_id`,`path`,`name`,`redirect`,`component`,`title`,`is_full`,`hide`,`disable`,`keep_alive`,`affix`,`is_link`,`link`,`iframe`,`svg_icon`,`icon`,`sort`,`type`,`permission`,`created_by`,`created_at`,`updated_at`)
SELECT m.`id`,'','device-assignment-assign','','','分配设备归属',0,0,0,0,0,0,'',0,'','',1,3,'gb28181:device:assign',1,NOW(),NOW()
FROM `sys_menu` m WHERE m.`name`='device-assignment' AND m.`deleted_at` IS NULL
AND NOT EXISTS (SELECT 1 FROM `sys_menu` x WHERE x.`name`='device-assignment-assign' AND x.`deleted_at` IS NULL);

-- 3. 按钮权限:共享设备(type=3)
INSERT INTO `sys_menu` (`parent_id`,`path`,`name`,`redirect`,`component`,`title`,`is_full`,`hide`,`disable`,`keep_alive`,`affix`,`is_link`,`link`,`iframe`,`svg_icon`,`icon`,`sort`,`type`,`permission`,`created_by`,`created_at`,`updated_at`)
SELECT m.`id`,'','device-assignment-share','','','共享设备',0,0,0,0,0,0,'',0,'','',2,3,'gb28181:device:share',1,NOW(),NOW()
FROM `sys_menu` m WHERE m.`name`='device-assignment' AND m.`deleted_at` IS NULL
AND NOT EXISTS (SELECT 1 FROM `sys_menu` x WHERE x.`name`='device-assignment-share' AND x.`deleted_at` IS NULL);

-- 4. 角色绑定:复制"设备列表"菜单(device-mgmt-list)的角色绑定到新菜单及按钮
INSERT INTO `sys_role_menu` (`role_id`,`menu_id`)
SELECT rm.`role_id`, m.`id`
FROM `sys_role_menu` rm
JOIN `sys_menu` src ON src.`id`=rm.`menu_id` AND src.`name`='device-mgmt-list' AND src.`deleted_at` IS NULL
JOIN `sys_menu` m ON m.`name` IN ('device-assignment','device-assignment-assign','device-assignment-share') AND m.`deleted_at` IS NULL
WHERE NOT EXISTS (SELECT 1 FROM `sys_role_menu` x WHERE x.`role_id`=rm.`role_id` AND x.`menu_id`=m.`id`);
