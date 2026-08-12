-- Cloud recording download task control APIs (MySQL 5.7+).
-- The content route is intentionally absent: it is authorized by a one-time HttpOnly cookie.
SET @recording_menu_id := (
  SELECT MIN(`id`) FROM `sys_menu`
  WHERE `path`='/gb28181/cloud-recordings' AND `deleted_at` IS NULL
);

INSERT INTO `sys_api` (`title`,`path`,`method`,`api_group`,`created_at`,`updated_at`,`created_by`)
SELECT v.title,v.path,v.method,'GB28181 云端录像下载',NOW(),NOW(),1 FROM (
  SELECT '创建云端录像下载' title,'/api/gb28181/cloud-recordings/files/:id/downloads' path,'POST' method
  UNION ALL SELECT '查询云端录像下载','/api/gb28181/cloud-recordings/downloads/:taskId','GET'
  UNION ALL SELECT '取消云端录像下载','/api/gb28181/cloud-recordings/downloads/:taskId','DELETE'
) v
WHERE NOT EXISTS (
  SELECT 1 FROM `sys_api` a
  WHERE a.`path`=v.path AND a.`method`=v.method AND a.`deleted_at` IS NULL
);

INSERT INTO `sys_menu_api` (`menu_id`,`api_id`)
SELECT @recording_menu_id,a.`id` FROM `sys_api` a
WHERE @recording_menu_id IS NOT NULL
  AND a.`api_group`='GB28181 云端录像下载'
  AND (
    (a.`path`='/api/gb28181/cloud-recordings/files/:id/downloads' AND a.`method`='POST')
    OR (a.`path`='/api/gb28181/cloud-recordings/downloads/:taskId' AND a.`method` IN ('GET','DELETE'))
  )
  AND a.`deleted_at` IS NULL
  AND NOT EXISTS (
    SELECT 1 FROM `sys_menu_api` x
    WHERE x.`menu_id`=@recording_menu_id AND x.`api_id`=a.`id`
  );

INSERT INTO `sys_casbin_rule` (`ptype`,`v0`,`v1`,`v2`,`v3`,`v4`,`v5`)
SELECT 'p',CONCAT('role_',rm.`role_id`),a.`path`,a.`method`,'*','',''
FROM `sys_role_menu` rm
JOIN `sys_api` a ON (
  (a.`path`='/api/gb28181/cloud-recordings/files/:id/downloads' AND a.`method`='POST')
  OR (a.`path`='/api/gb28181/cloud-recordings/downloads/:taskId' AND a.`method` IN ('GET','DELETE'))
)
WHERE rm.`menu_id`=@recording_menu_id
  AND a.`api_group`='GB28181 云端录像下载'
  AND a.`deleted_at` IS NULL
  AND NOT EXISTS (
    SELECT 1 FROM `sys_casbin_rule` c
    WHERE c.`ptype`='p' AND c.`v0`=CONCAT('role_',rm.`role_id`)
      AND c.`v1`=a.`path` AND c.`v2`=a.`method` AND c.`v3`='*'
  );
