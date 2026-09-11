-- SIP Trace APIs and administrator-only menu grants. Safe to run repeatedly.
SET @GB_PARENT_ID = NULL;

INSERT INTO `sys_menu` (
  `parent_id`, `name`, `path`, `component`, `title`, `icon`, `sort`, `disable`, `created_at`, `updated_at`
)
SELECT @GB_PARENT_ID, 'gb28181-sip-traces', '/gb28181/sip-traces',
       'gb28181/sip/TraceWorkbench', 'SIP 日志', 'lucide:FileText', 7, 0, NOW(), NOW()
WHERE @GB_PARENT_ID IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM `sys_menu` WHERE `path` = '/gb28181/sip-traces');

UPDATE `sys_menu`
SET `parent_id` = IFNULL(@GB_PARENT_ID, `parent_id`),
    `component` = 'gb28181/sip/TraceWorkbench', `title` = 'SIP 日志',
    `icon` = 'lucide:FileText', `sort` = 7, `disable` = 0, `updated_at` = NOW()
WHERE `path` = '/gb28181/sip-traces';

INSERT INTO `sys_api` (`title`, `path`, `method`, `api_group`, `created_at`, `updated_at`, `created_by`)
SELECT seed.title, seed.path, seed.method, 'SIP 日志', NOW(), NOW(), 1
FROM (
  SELECT 'SIP Trace 健康状态' title, '/api/gb28181/sip-traces/health' path, 'GET' method
  UNION ALL SELECT 'SIP Trace 报文列表', '/api/gb28181/sip-traces/messages', 'GET'
  UNION ALL SELECT 'SIP Trace 报文详情', '/api/gb28181/sip-traces/messages/:id', 'GET'
  UNION ALL SELECT 'SIP Trace 会话列表', '/api/gb28181/sip-traces/sessions', 'GET'
  UNION ALL SELECT 'SIP Trace 会话报文', '/api/gb28181/sip-traces/sessions/:callId/messages', 'GET'
  UNION ALL SELECT '设备 SIP 诊断状态', '/api/gb28181/device-mgmt/device/:id/sip-trace-capture', 'GET'
  UNION ALL SELECT '启动设备 SIP 诊断', '/api/gb28181/device-mgmt/device/:id/sip-trace-captures', 'POST'
  UNION ALL SELECT '停止设备 SIP 诊断', '/api/gb28181/sip-traces/captures/:id/stop', 'POST'
) seed
WHERE NOT EXISTS (
  SELECT 1 FROM `sys_api` existing
  WHERE existing.path = seed.path AND existing.method = seed.method AND existing.deleted_at IS NULL
);

INSERT IGNORE INTO `sys_menu_api` (`menu_id`, `api_id`)
SELECT menu.id, api.id
FROM `sys_menu` menu
JOIN `sys_api` api ON api.api_group = 'SIP 日志' AND api.deleted_at IS NULL
WHERE menu.path = '/gb28181/sip-traces' AND menu.deleted_at IS NULL;

INSERT IGNORE INTO `sys_role_menu` (`role_id`, `menu_id`)
SELECT role.id, menu.id
FROM `sys_role` role
JOIN `sys_menu` menu ON menu.path = '/gb28181/sip-traces' AND menu.deleted_at IS NULL
WHERE role.name = '系统管理员' AND role.status = 1 AND role.deleted_at IS NULL;

INSERT INTO `sys_casbin_rule` (`ptype`, `v0`, `v1`, `v2`, `v3`, `v4`, `v5`)
SELECT 'p', CONCAT('role_', role.id), api.path, api.method, '*', '', ''
FROM `sys_role` role
JOIN `sys_api` api ON api.api_group = 'SIP 日志' AND api.deleted_at IS NULL
WHERE role.name = '系统管理员' AND role.status = 1 AND role.deleted_at IS NULL
  AND NOT EXISTS (
    SELECT 1 FROM `sys_casbin_rule` rule
    WHERE rule.ptype = 'p' AND rule.v0 = CONCAT('role_', role.id)
      AND rule.v1 = api.path AND rule.v2 = api.method
  );
