-- Register device recording playback as a hidden layout route (MySQL 5.7+).
-- The device management menu must already exist.

SET @DEVICE_MGMT_MENU_ID = (
    SELECT `id`
    FROM `sys_menu`
    WHERE `path` IN ('/gb28181/device-mgmt/index', '/gb28181/device-mgmt')
      AND `deleted_at` IS NULL
    ORDER BY CASE WHEN `path` = '/gb28181/device-mgmt/index' THEN 0 ELSE 1 END, `id`
    LIMIT 1
);

SET @GB_PARENT_MENU_ID = (
    SELECT `parent_id` FROM `sys_menu` WHERE `id` = @DEVICE_MGMT_MENU_ID
);

UPDATE `sys_menu`
SET
    `parent_id` = IFNULL(@GB_PARENT_MENU_ID, `parent_id`),
    `name` = 'gb28181-device-record-playback',
    `component` = 'gb28181/device-record-playback/index',
    `title` = '设备录像回放',
    `is_full` = 0,
    `hide` = 1,
    `disable` = 0,
    `keep_alive` = 0,
    `type` = 2,
    `updated_at` = NOW(),
    `deleted_at` = NULL
WHERE `path` = '/gb28181/device-record-playback/:channelId';

INSERT INTO `sys_menu` (
    `parent_id`, `path`, `name`, `component`, `title`,
    `is_full`, `hide`, `disable`, `keep_alive`, `sort`, `type`,
    `created_at`, `updated_at`, `created_by`
)
SELECT
    @GB_PARENT_MENU_ID,
    '/gb28181/device-record-playback/:channelId',
    'gb28181-device-record-playback',
    'gb28181/device-record-playback/index',
    '设备录像回放',
    0, 1, 0, 0, 99, 2,
    NOW(), NOW(), 1
WHERE @GB_PARENT_MENU_ID IS NOT NULL
  AND NOT EXISTS (
      SELECT 1 FROM `sys_menu`
      WHERE `path` = '/gb28181/device-record-playback/:channelId'
        AND `deleted_at` IS NULL
  );

SET @PLAYBACK_MENU_ID = (
    SELECT MIN(`id`)
    FROM `sys_menu`
    WHERE `path` = '/gb28181/device-record-playback/:channelId'
      AND `deleted_at` IS NULL
);

INSERT INTO `sys_role_menu` (`role_id`, `menu_id`)
SELECT source_role.`role_id`, @PLAYBACK_MENU_ID
FROM `sys_role_menu` source_role
WHERE source_role.`menu_id` = @DEVICE_MGMT_MENU_ID
  AND @PLAYBACK_MENU_ID IS NOT NULL
  AND NOT EXISTS (
      SELECT 1 FROM `sys_role_menu` existing
      WHERE existing.`role_id` = source_role.`role_id`
        AND existing.`menu_id` = @PLAYBACK_MENU_ID
  );
