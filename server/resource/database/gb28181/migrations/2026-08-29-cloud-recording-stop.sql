-- Cloud recording stop permission (MySQL 5.7+).
SET @recording_menu_id := (SELECT MIN(`id`) FROM `sys_menu` WHERE `path`='/gb28181/cloud-recordings' AND `deleted_at` IS NULL);

INSERT INTO `sys_menu` (`parent_id`,`path`,`name`,`component`,`title`,`type`,`permission`,`hide`,`created_at`,`updated_at`,`created_by`)
SELECT @recording_menu_id,'','','','停止录像',3,'gb28181:recording:stop',1,NOW(),NOW(),1
WHERE @recording_menu_id IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM `sys_menu` WHERE `permission`='gb28181:recording:stop' AND `deleted_at` IS NULL);

INSERT INTO `sys_api` (`title`,`path`,`method`,`api_group`,`created_at`,`updated_at`,`created_by`)
SELECT '停止云端录像','/api/gb28181/cloud-recordings/active/:id/stop','POST','GB28181 云端录像控制',NOW(),NOW(),1
WHERE NOT EXISTS (SELECT 1 FROM `sys_api` WHERE `path`='/api/gb28181/cloud-recordings/active/:id/stop' AND `method`='POST' AND `deleted_at` IS NULL);

SET @stop_menu_id := (SELECT MIN(`id`) FROM `sys_menu` WHERE `permission`='gb28181:recording:stop' AND `deleted_at` IS NULL);
INSERT INTO `sys_role_menu` (`role_id`,`menu_id`)
SELECT 1,@stop_menu_id WHERE @stop_menu_id IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM `sys_role_menu` WHERE `role_id`=1 AND `menu_id`=@stop_menu_id);

INSERT INTO `sys_menu_api` (`menu_id`,`api_id`)
SELECT @stop_menu_id,a.`id` FROM `sys_api` a
WHERE @stop_menu_id IS NOT NULL AND a.`path`='/api/gb28181/cloud-recordings/active/:id/stop' AND a.`method`='POST' AND a.`deleted_at` IS NULL
  AND NOT EXISTS (SELECT 1 FROM `sys_menu_api` x WHERE x.`menu_id`=@stop_menu_id AND x.`api_id`=a.`id`);

INSERT INTO `sys_casbin_rule` (`ptype`,`v0`,`v1`,`v2`,`v3`,`v4`,`v5`)
SELECT 'p','role_1',a.`path`,a.`method`,'*','','' FROM `sys_api` a
WHERE a.`path`='/api/gb28181/cloud-recordings/active/:id/stop' AND a.`method`='POST' AND a.`deleted_at` IS NULL
  AND NOT EXISTS (SELECT 1 FROM `sys_casbin_rule` c WHERE c.`ptype`='p' AND c.`v0`='role_1' AND c.`v1`=a.`path` AND c.`v2`=a.`method` AND c.`v3`='*');
