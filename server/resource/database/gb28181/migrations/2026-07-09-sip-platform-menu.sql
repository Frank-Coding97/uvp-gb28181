-- 2026-07-09 SIP 平台接入信息菜单 seed
-- 用法:
-- 1) 先查父菜单 id:
--    SELECT id, title, path FROM sys_menu WHERE title LIKE '%国标%' OR path = '/gb28181';
-- 2) 把 @GB_PARENT_ID 设置为查询到的“国标平台”父菜单 id 后执行。

SET @GB_PARENT_ID = NULL;

UPDATE `sys_menu`
SET
  `parent_id` = IFNULL(@GB_PARENT_ID, `parent_id`),
  `component` = 'gb28181/sip/PlatformInfo',
  `title` = 'SIP 接入信息',
  `icon` = 'icon-link',
  `sort` = 4,
  `status` = 1,
  `updated_at` = NOW()
WHERE `path` = '/gb28181/sip/platform';

INSERT INTO `sys_menu` (
  `parent_id`, `name`, `path`, `component`, `title`, `icon`, `sort`, `status`, `created_at`, `updated_at`
)
SELECT
  @GB_PARENT_ID,
  'gb28181-sip-platform',
  '/gb28181/sip/platform',
  'gb28181/sip/PlatformInfo',
  'SIP 接入信息',
  'icon-link',
  4,
  1,
  NOW(),
  NOW()
WHERE @GB_PARENT_ID IS NOT NULL
  AND NOT EXISTS (
    SELECT 1 FROM `sys_menu` WHERE `path` = '/gb28181/sip/platform'
  );
