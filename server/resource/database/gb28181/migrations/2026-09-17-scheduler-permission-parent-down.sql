-- 恢复调度按钮权限归属与原菜单标题（MySQL）
UPDATE `sys_menu`
SET `parent_id`=(SELECT `id` FROM (SELECT MIN(`id`) AS `id` FROM `sys_menu` WHERE `path`='/media/scheduling' AND `type` IN (1,2) AND `deleted_at` IS NULL) AS `target_menu`),
    `updated_at`=CURRENT_TIMESTAMP
WHERE `permission`='gb28181:zlm:scheduler:manage'
  AND `type`=3
  AND `deleted_at` IS NULL
  AND (SELECT `id` FROM (SELECT MIN(`id`) AS `id` FROM `sys_menu` WHERE `path`='/media/scheduling' AND `type` IN (1,2) AND `deleted_at` IS NULL) AS `target_menu`) IS NOT NULL;

UPDATE `sys_menu`
SET `title`='调度管理',`updated_at`=CURRENT_TIMESTAMP
WHERE `path`='/media/scheduling' AND `type` IN (1,2) AND `deleted_at` IS NULL AND `title`='调度日志';
