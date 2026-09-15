-- Cloud recording physical deletion permissions (MySQL 5.7+).
SET @recording_menu_id := (SELECT MIN(`id`) FROM `sys_menu` WHERE `path`='/gb28181/cloud-recordings' AND `deleted_at` IS NULL);

INSERT INTO `sys_menu` (`parent_id`,`path`,`name`,`component`,`title`,`type`,`permission`,`hide`,`created_at`,`updated_at`,`created_by`)
SELECT @recording_menu_id,'','','','删除录像文件',3,'gb28181:recording:delete',1,NOW(),NOW(),1
WHERE @recording_menu_id IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM `sys_menu` WHERE `permission`='gb28181:recording:delete' AND `deleted_at` IS NULL);

INSERT INTO `sys_api` (`title`,`path`,`method`,`api_group`,`created_at`,`updated_at`,`created_by`)
SELECT v.title,v.path,v.method,'GB28181 云端录像删除',NOW(),NOW(),1 FROM (
  SELECT '删除单个云端录像' title,'/api/gb28181/cloud-recordings/files/:id' path,'DELETE' method
  UNION ALL SELECT '批量删除云端录像','/api/gb28181/cloud-recordings/files/batch-delete','POST'
) v WHERE NOT EXISTS (SELECT 1 FROM `sys_api` a WHERE a.`path`=v.path AND a.`method`=v.method AND a.`deleted_at` IS NULL);

SET @delete_menu_id := (SELECT MIN(`id`) FROM `sys_menu` WHERE `permission`='gb28181:recording:delete' AND `deleted_at` IS NULL);
INSERT INTO `sys_role_menu` (`role_id`,`menu_id`)
SELECT 1,@delete_menu_id WHERE @delete_menu_id IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM `sys_role_menu` WHERE `role_id`=1 AND `menu_id`=@delete_menu_id);

INSERT INTO `sys_menu_api` (`menu_id`,`api_id`)
SELECT @delete_menu_id,a.`id` FROM `sys_api` a
WHERE @delete_menu_id IS NOT NULL
  AND ((a.`path`='/api/gb28181/cloud-recordings/files/:id' AND a.`method`='DELETE')
    OR (a.`path`='/api/gb28181/cloud-recordings/files/batch-delete' AND a.`method`='POST'))
  AND a.`deleted_at` IS NULL
  AND NOT EXISTS (SELECT 1 FROM `sys_menu_api` x WHERE x.`menu_id`=@delete_menu_id AND x.`api_id`=a.`id`);

INSERT INTO `sys_casbin_rule` (`ptype`,`v0`,`v1`,`v2`,`v3`,`v4`,`v5`)
SELECT 'p','role_1',a.`path`,a.`method`,'*','','' FROM `sys_api` a
WHERE ((a.`path`='/api/gb28181/cloud-recordings/files/:id' AND a.`method`='DELETE')
    OR (a.`path`='/api/gb28181/cloud-recordings/files/batch-delete' AND a.`method`='POST'))
  AND a.`deleted_at` IS NULL
  AND NOT EXISTS (SELECT 1 FROM `sys_casbin_rule` c WHERE c.`ptype`='p' AND c.`v0`='role_1' AND c.`v1`=a.`path` AND c.`v2`=a.`method` AND c.`v3`='*');
