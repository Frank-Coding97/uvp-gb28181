-- 2026-06-30 设备管理页菜单 seed
-- 执行前把 @GB_PARENT_ID 设置为“国标平台”父菜单 id。
-- 本脚本可重复执行：已存在 path 时会纠正 component，不会重复插入。

SET @GB_PARENT_ID = NULL;

-- 设备管理主入口：多级目录 + 列表 / 卡片 / 地图 + 详情抽屉
UPDATE `sys_menu`
SET
    `parent_id` = IFNULL(@GB_PARENT_ID, `parent_id`),
    `component` = 'gb28181/device-mgmt/index',
    `title` = '设备管理',
    `icon` = 'icon-camera',
    `sort` = 5,
    `status` = 1,
    `updated_at` = NOW()
WHERE `path` = '/gb28181/device-mgmt';

INSERT INTO `sys_menu`
    (`parent_id`, `name`, `path`, `component`, `title`, `icon`, `sort`, `status`, `created_at`, `updated_at`)
SELECT
    @GB_PARENT_ID,
    'device-mgmt',
    '/gb28181/device-mgmt',
    'gb28181/device-mgmt/index',
    '设备管理',
    'icon-camera',
    5,
    1,
    NOW(),
    NOW()
WHERE @GB_PARENT_ID IS NOT NULL
  AND NOT EXISTS (
      SELECT 1 FROM `sys_menu` WHERE `path` = '/gb28181/device-mgmt'
  );

-- 目录异常隐藏入口：兼容已有菜单树直接访问该 path 的场景
UPDATE `sys_menu`
SET
    `parent_id` = IFNULL(@GB_PARENT_ID, `parent_id`),
    `component` = 'gb28181/device-mgmt/anomaly/index',
    `title` = '目录异常',
    `sort` = 6,
    `status` = 1,
    `hide` = 1,
    `updated_at` = NOW()
WHERE `path` = '/gb28181/device-mgmt/anomaly';

INSERT INTO `sys_menu`
    (`parent_id`, `name`, `path`, `component`, `title`, `sort`, `status`, `hide`, `created_at`, `updated_at`)
SELECT
    @GB_PARENT_ID,
    'device-mgmt-anomaly',
    '/gb28181/device-mgmt/anomaly',
    'gb28181/device-mgmt/anomaly/index',
    '目录异常',
    6,
    1,
    1,
    NOW(),
    NOW()
WHERE @GB_PARENT_ID IS NOT NULL
  AND NOT EXISTS (
      SELECT 1 FROM `sys_menu` WHERE `path` = '/gb28181/device-mgmt/anomaly'
  );
