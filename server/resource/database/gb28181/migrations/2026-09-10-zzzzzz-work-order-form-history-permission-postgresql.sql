-- 作业单表单历史值接口权限（PostgreSQL 12+，幂等）。
INSERT INTO sys_api (title, path, method, api_group, created_at, updated_at, created_by)
SELECT '作业单表单历史值', '/api/gb28181/work-orders/form-history', 'GET', 'GB28181 作业单', NOW(), NOW(), 1
WHERE NOT EXISTS (SELECT 1 FROM sys_api a WHERE a.path='/api/gb28181/work-orders/form-history' AND a.method='GET' AND a.deleted_at IS NULL);

INSERT INTO sys_menu_api (menu_id, api_id)
SELECT m.id, a.id FROM sys_menu m JOIN sys_api a ON a.deleted_at IS NULL
WHERE m.permission='gb28181:work-order:view' AND m.deleted_at IS NULL
  AND a.method='GET' AND a.path='/api/gb28181/work-orders/form-history'
  AND NOT EXISTS (SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);

INSERT INTO sys_casbin_rule (ptype, v0, v1, v2, v3, v4, v5)
SELECT DISTINCT 'p', 'role_1', a.path, a.method, '*', '', ''
FROM sys_api a
WHERE a.path='/api/gb28181/work-orders/form-history' AND a.method='GET' AND a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule c WHERE c.ptype='p' AND c.v0='role_1' AND c.v1=a.path AND c.v2=a.method AND c.v3='*');
