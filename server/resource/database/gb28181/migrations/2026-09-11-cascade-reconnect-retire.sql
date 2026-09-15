-- 退役国标级联「重连」权限元数据（MySQL 5.7+，幂等）。
-- 背景：列表页的「重连」操作已删除，POST /api/gb28181/cascade/platforms/:id/reconnect
--       端点已随之下线；注册失败的重试由级联运行时内部的定时器自动完成，
--       不再需要人工触发，该权限点成为指向已不存在端点的孤儿记录。
-- 本迁移只退役这些孤儿记录，不触碰其它级联权限。
-- 幂等：可重复执行；尚未写入过这些记录的环境执行时为空操作。
-- 注意：sys_casbin_rule / sys_menu_api / sys_role_menu 无 deleted_at 列，只能物理删除；
--       sys_menu / sys_api 走软删（deleted_at）以保留审计痕迹。

-- 1) Casbin 规则
DELETE FROM `sys_casbin_rule` WHERE `v1`='/api/gb28181/cascade/platforms/:id/reconnect' AND `v2`='POST';

-- 2) 菜单与 API 的绑定（先按 API 命中，再按菜单命中，覆盖两种绑定来源）
DELETE FROM `sys_menu_api` WHERE `api_id` IN (
  SELECT `id` FROM `sys_api` WHERE `path`='/api/gb28181/cascade/platforms/:id/reconnect' AND `method`='POST'
);
DELETE FROM `sys_menu_api` WHERE `menu_id` IN (
  SELECT `id` FROM `sys_menu` WHERE `permission`='gb28181:cascade:reconnect'
);

-- 3) 角色与菜单的绑定
DELETE FROM `sys_role_menu` WHERE `menu_id` IN (
  SELECT `id` FROM `sys_menu` WHERE `permission`='gb28181:cascade:reconnect'
);

-- 4) 按钮权限菜单软删
UPDATE `sys_menu` SET `deleted_at`=NOW(), `updated_at`=NOW()
WHERE `permission`='gb28181:cascade:reconnect'
  AND `deleted_at` IS NULL;

-- 5) API 记录软删
UPDATE `sys_api` SET `deleted_at`=NOW(), `updated_at`=NOW()
WHERE `path`='/api/gb28181/cascade/platforms/:id/reconnect' AND `method`='POST'
  AND `deleted_at` IS NULL;
