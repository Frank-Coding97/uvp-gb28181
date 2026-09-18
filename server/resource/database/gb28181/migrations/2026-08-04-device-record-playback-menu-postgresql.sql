-- Register device recording playback as a hidden layout route (PostgreSQL 12+).
-- The device management menu must already exist.

UPDATE sys_menu AS playback
SET
    parent_id = source.parent_id,
    name = 'gb28181-device-record-playback',
    component = 'gb28181/device-record-playback/index',
    title = '设备录像回放',
    is_full = FALSE,
    hide = TRUE,
    disable = FALSE,
    keep_alive = FALSE,
    type = 2,
    updated_at = CURRENT_TIMESTAMP,
    deleted_at = NULL
FROM (
    SELECT id, parent_id
    FROM sys_menu
    WHERE path IN ('/gb28181/device-mgmt/index', '/gb28181/device-mgmt')
      AND deleted_at IS NULL
    ORDER BY CASE WHEN path = '/gb28181/device-mgmt/index' THEN 0 ELSE 1 END, id
    LIMIT 1
) AS source
WHERE playback.path = '/gb28181/device-record-playback/:channelId'
;

INSERT INTO sys_menu (
    parent_id, path, name, component, title,
    is_full, hide, disable, keep_alive, sort, type,
    created_at, updated_at, created_by
)
SELECT
    source.parent_id,
    '/gb28181/device-record-playback/:channelId',
    'gb28181-device-record-playback',
    'gb28181/device-record-playback/index',
    '设备录像回放',
    FALSE, TRUE, FALSE, FALSE, 99, 2,
    CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 1
FROM (
    SELECT id, parent_id
    FROM sys_menu
    WHERE path IN ('/gb28181/device-mgmt/index', '/gb28181/device-mgmt')
      AND deleted_at IS NULL
    ORDER BY CASE WHEN path = '/gb28181/device-mgmt/index' THEN 0 ELSE 1 END, id
    LIMIT 1
) AS source
WHERE TRUE
  AND NOT EXISTS (
      SELECT 1 FROM sys_menu
      WHERE path = '/gb28181/device-record-playback/:channelId'
        AND deleted_at IS NULL
  );

INSERT INTO sys_role_menu (role_id, menu_id)
SELECT source_role.role_id, playback.id
FROM sys_role_menu AS source_role
JOIN sys_menu AS source
  ON source.id = source_role.menu_id
 AND source.path IN ('/gb28181/device-mgmt/index', '/gb28181/device-mgmt')
 AND source.deleted_at IS NULL
JOIN sys_menu AS playback
  ON playback.path = '/gb28181/device-record-playback/:channelId'
 AND playback.deleted_at IS NULL
WHERE NOT EXISTS (
    SELECT 1 FROM sys_role_menu AS existing
    WHERE existing.role_id = source_role.role_id
      AND existing.menu_id = playback.id
);
