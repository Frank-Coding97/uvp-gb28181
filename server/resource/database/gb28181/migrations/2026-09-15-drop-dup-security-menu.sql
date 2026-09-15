-- drop-dup-security-menu:start
-- 物理删除重复的「国标接入安全」菜单(MySQL 5.7+,可重复执行)。
-- 该菜单(path=/gb28181/security)的页面 gb28181/security/index 只是 security/preview.vue 的
-- re-export 壳,与在役菜单 /security-preview 打开的是同一个页面,且该行 disable=1 早已停用,
-- 两个同名菜单并存容易引起误解。
-- 只清理这条重复菜单及其关联绑定;在役入口 /security-preview(id=140371)、其下的 5 个按钮权限、
-- sys_api 与 sys_casbin_rule 中的接入安全接口授权全部保留不动。
DELETE ma FROM `sys_menu_api` ma JOIN `sys_menu` m ON m.`id`=ma.`menu_id` WHERE m.`path`='/gb28181/security';
DELETE FROM `sys_role_menu` WHERE `menu_id` IN (SELECT `id` FROM `sys_menu` WHERE `path`='/gb28181/security');
DELETE FROM `sys_menu` WHERE `path`='/gb28181/security';
-- drop-dup-security-menu:end
