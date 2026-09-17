-- 恢复调度按钮权限归属与原菜单标题（PostgreSQL）
UPDATE sys_menu
SET parent_id=(SELECT MIN(id) FROM sys_menu WHERE path='/media/scheduling' AND type IN (1,2) AND deleted_at IS NULL),
    updated_at=CURRENT_TIMESTAMP
WHERE permission='gb28181:zlm:scheduler:manage'
  AND type=3
  AND deleted_at IS NULL
  AND (SELECT MIN(id) FROM sys_menu WHERE path='/media/scheduling' AND type IN (1,2) AND deleted_at IS NULL) IS NOT NULL;

UPDATE sys_menu
SET title='调度管理',updated_at=CURRENT_TIMESTAMP
WHERE path='/media/scheduling' AND type IN (1,2) AND deleted_at IS NULL AND title='调度日志';
