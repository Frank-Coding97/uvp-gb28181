-- Flatten the obsolete GB28181 directory into root-level menus (SQL Server 2017+).
-- Repeatable: resolve the directory by path, promote its direct children, then soft-delete it.

DECLARE @GB_DIRECTORY_ID BIGINT;

SELECT TOP 1 @GB_DIRECTORY_ID = [id]
FROM [sys_menu]
WHERE [path] = '/gb28181' AND [deleted_at] IS NULL
ORDER BY [id];

UPDATE [sys_menu]
SET [parent_id] = 0,
    [updated_at] = CURRENT_TIMESTAMP
WHERE [parent_id] = @GB_DIRECTORY_ID
  AND @GB_DIRECTORY_ID IS NOT NULL
  AND [deleted_at] IS NULL;

-- Zero sorts are rendered after every positive sort. Pin Home first so the
-- promoted GB28181 entries can sit immediately below it.
UPDATE [sys_menu]
SET [sort] = 1,
    [updated_at] = CURRENT_TIMESTAMP
WHERE [path] = '/home' AND [deleted_at] IS NULL;

UPDATE [sys_menu]
SET [svg_icon] = '',
    [icon] = 'lucide:Clapperboard',
    [sort] = 9,
    [updated_at] = CURRENT_TIMESTAMP
WHERE [path] = '/media' AND [deleted_at] IS NULL;

UPDATE [sys_menu]
SET [svg_icon] = '',
    [icon] = CASE [path]
        WHEN '/gb28181/device-mgmt' THEN 'lucide:Cctv'
        WHEN '/gb28181/device-mgmt/index' THEN 'lucide:Cctv'
        WHEN '/gb28181/device-mgmt/anomaly' THEN 'lucide:Cctv'
        WHEN '/gb28181/device-record-playback/:channelId' THEN 'lucide:History'
        WHEN '/gb28181/multi-screen-playback' THEN 'lucide:MonitorPlay'
        WHEN '/gb28181/alarm-management' THEN 'lucide:BellRing'
        WHEN '/gb28181/sip/platform' THEN 'lucide:Router'
        WHEN '/gb28181/sip/config' THEN 'lucide:ServerCog'
        WHEN '/gb28181/sip-traces' THEN 'lucide:FileText'
        WHEN '/gb28181/security' THEN 'lucide:Shield'
        WHEN '/security-preview' THEN 'lucide:Shield'
        WHEN '/gb28181/zlm/nodes' THEN 'lucide:Server'
        WHEN '/gb28181/zlm/nodes/:id' THEN 'lucide:Server'
        WHEN '/gb28181/zlm/scheduler' THEN 'lucide:Workflow'
        WHEN '/gb28181/zlm/scheduler/logs' THEN 'lucide:History'
        ELSE [icon]
    END,
    [sort] = CASE [path]
        WHEN '/gb28181/device-mgmt' THEN 2
        WHEN '/gb28181/device-mgmt/index' THEN 2
        WHEN '/gb28181/multi-screen-playback' THEN 3
        WHEN '/gb28181/alarm-management' THEN 4
        WHEN '/gb28181/security' THEN 5
        WHEN '/security-preview' THEN 5
        WHEN '/gb28181/sip/platform' THEN 6
        WHEN '/gb28181/sip/config' THEN 7
        WHEN '/gb28181/sip-traces' THEN 8
        WHEN '/gb28181/zlm/nodes' THEN 10
        WHEN '/gb28181/zlm/scheduler' THEN 11
        WHEN '/gb28181/zlm/scheduler/logs' THEN 12
        ELSE [sort]
    END,
    [updated_at] = CURRENT_TIMESTAMP
WHERE [path] IN (
    '/gb28181/device-mgmt',
    '/gb28181/device-mgmt/index',
    '/gb28181/device-mgmt/anomaly',
    '/gb28181/device-record-playback/:channelId',
    '/gb28181/multi-screen-playback',
    '/gb28181/alarm-management',
    '/gb28181/sip/platform',
    '/gb28181/sip/config',
    '/gb28181/sip-traces',
    '/gb28181/security',
    '/security-preview',
    '/gb28181/zlm/nodes',
    '/gb28181/zlm/nodes/:id',
    '/gb28181/zlm/scheduler',
    '/gb28181/zlm/scheduler/logs'
) AND [deleted_at] IS NULL;

UPDATE [sys_menu]
SET [hide] = 1,
    [disable] = 1,
    [deleted_at] = CURRENT_TIMESTAMP,
    [updated_at] = CURRENT_TIMESTAMP
WHERE [id] = @GB_DIRECTORY_ID
  AND @GB_DIRECTORY_ID IS NOT NULL
  AND [path] = '/gb28181'
  AND [deleted_at] IS NULL;
