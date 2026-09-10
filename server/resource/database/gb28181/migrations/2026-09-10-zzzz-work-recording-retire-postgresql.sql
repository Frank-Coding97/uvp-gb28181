-- 退役旧的单通道/批次录像权限元数据（PostgreSQL 12+，幂等）。
-- 背景：单通道与批次录像的对外路由（/api/gb28181/work-recordings*）与处理器已被删除，
--       该能力由作业单（/api/gb28181/work-orders）统一接管。
--       但历史迁移写入的菜单 / API / 授权记录仍留在库里，指向已不存在的端点，
--       会在菜单树里留下点不动、且与作业单重复的按钮权限。
-- 本迁移只退役这些孤儿记录，不触碰任何其它权限。
-- 幂等：可重复执行；尚未写入过这些记录的环境执行时为空操作。
-- 注意：sys_casbin_rule / sys_menu_api / sys_role_menu 无 deleted_at 列，只能物理删除；
--       sys_menu / sys_api 走软删（deleted_at）以保留审计痕迹。

-- 1) Casbin 规则
DELETE FROM sys_casbin_rule WHERE v1 LIKE '/api/gb28181/work-recordings%';

-- 2) 菜单与 API 的绑定（先按 API 命中，再按菜单命中，覆盖两种绑定来源）
DELETE FROM sys_menu_api WHERE api_id IN (
  SELECT id FROM sys_api WHERE path LIKE '/api/gb28181/work-recordings%'
);
DELETE FROM sys_menu_api WHERE menu_id IN (
  SELECT id FROM sys_menu
  WHERE permission IN ('gb28181:work-recording:start','gb28181:work-recording:stop','gb28181:work-recording:form')
);

-- 3) 角色与菜单的绑定
DELETE FROM sys_role_menu WHERE menu_id IN (
  SELECT id FROM sys_menu
  WHERE permission IN ('gb28181:work-recording:start','gb28181:work-recording:stop','gb28181:work-recording:form')
);

-- 4) 按钮权限菜单软删
UPDATE sys_menu SET deleted_at=CURRENT_TIMESTAMP, updated_at=CURRENT_TIMESTAMP
WHERE permission IN ('gb28181:work-recording:start','gb28181:work-recording:stop','gb28181:work-recording:form')
  AND deleted_at IS NULL;

-- 5) API 记录软删
UPDATE sys_api SET deleted_at=CURRENT_TIMESTAMP, updated_at=CURRENT_TIMESTAMP
WHERE path LIKE '/api/gb28181/work-recordings%'
  AND deleted_at IS NULL;
