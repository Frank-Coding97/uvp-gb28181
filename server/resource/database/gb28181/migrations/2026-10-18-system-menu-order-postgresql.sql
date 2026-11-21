-- 「系统管理」子菜单整理：
--   ① 子菜单原先 sort 全是 0（服务升级是 90），实际显示顺序全靠 id 大小，
--      等于谁都没法控制排序；这里统一成 10 的倍数。
--   ② 开发/运维类菜单正式交付不露出来 ⇒ hide=1（不删除，页面仍可直达）：
--      「代码生成」（脚手架）、「文件管理」（宿主机文件浏览，对客有风险）。
-- 只改 sort / hide，不动 parent_id / path / permission / 授权关系。
-- 定位键用 path（这些 path 全局唯一），不用 id：id 是基线常量，path 才是跨版本契约。
-- 目标顺序：账号管理 / 角色管理 / 菜单管理 / 部门管理 / 在线用户 / 字典管理 /
--   系统配置 / 接口管理 / 服务升级 / 文件管理（隐藏）/ 代码生成（隐藏）/
--   插件管理（隐藏）/ 用户信息（隐藏）

UPDATE sys_menu SET sort=10 WHERE path='/system/account' AND deleted_at IS NULL;
UPDATE sys_menu SET sort=20 WHERE path='/system/role' AND deleted_at IS NULL;
UPDATE sys_menu SET sort=30 WHERE path='/system/menu' AND deleted_at IS NULL;
UPDATE sys_menu SET sort=40 WHERE path='/system/division' AND deleted_at IS NULL;
UPDATE sys_menu SET sort=50 WHERE path='/system/online-user' AND deleted_at IS NULL;
UPDATE sys_menu SET sort=60 WHERE path='/system/dictionary' AND deleted_at IS NULL;
UPDATE sys_menu SET sort=70 WHERE path='/system/sysconfig' AND deleted_at IS NULL;
UPDATE sys_menu SET sort=80 WHERE path='/system/api' AND deleted_at IS NULL;
UPDATE sys_menu SET sort=90 WHERE path='/system/affix' AND deleted_at IS NULL;
UPDATE sys_menu SET sort=100 WHERE path='/system/service-upgrade/index' AND deleted_at IS NULL;
UPDATE sys_menu SET sort=110 WHERE path='/system/codegen' AND deleted_at IS NULL;
UPDATE sys_menu SET sort=120 WHERE path='/system/pluginsmanager' AND deleted_at IS NULL;
UPDATE sys_menu SET sort=130 WHERE path='/system/userinfo' AND deleted_at IS NULL;
UPDATE sys_menu SET hide=1 WHERE path='/system/affix' AND deleted_at IS NULL;
UPDATE sys_menu SET hide=1 WHERE path='/system/codegen' AND deleted_at IS NULL;
