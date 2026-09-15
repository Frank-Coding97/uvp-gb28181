-- Cloud recording catalog schema (PostgreSQL 13+).
CREATE EXTENSION IF NOT EXISTS pgcrypto;

ALTER TABLE gb_recording_file
  ALTER COLUMN start_time DROP NOT NULL,
  ALTER COLUMN time_len DROP NOT NULL,
  ALTER COLUMN time_len DROP DEFAULT,
  ALTER COLUMN file_size DROP NOT NULL,
  ALTER COLUMN file_size DROP DEFAULT;

ALTER TABLE gb_recording_file
  ADD COLUMN channel_code varchar(20) NOT NULL DEFAULT '',
  ADD COLUMN channel_name varchar(255) NOT NULL DEFAULT '',
  ADD COLUMN device_name varchar(255) NOT NULL DEFAULT '',
  ADD COLUMN owner_dept_id bigint NOT NULL DEFAULT 0,
  ADD COLUMN file_key varchar(64),
  ADD COLUMN source varchar(16) NOT NULL DEFAULT 'hook',
  ADD COLUMN metadata_state varchar(16) NOT NULL DEFAULT 'complete',
  ADD COLUMN record_date date,
  ADD COLUMN discovered_at timestamp,
  ADD COLUMN last_seen_at timestamp,
  ADD COLUMN missing_at timestamp,
  ADD COLUMN reconcile_miss_count integer NOT NULL DEFAULT 0,
  ADD COLUMN updated_at timestamp;

UPDATE gb_recording_file f SET
  channel_code=COALESCE(c.channel_id,''),
  channel_name=COALESCE(NULLIF(c.alias,''),c.name,''),
  device_name=COALESCE((SELECT d.name FROM gb_device d WHERE d.device_id=f.device_id LIMIT 1),''),
  owner_dept_id=COALESCE(c.owner_dept_id,0),
  record_date=f.start_time::date,
  discovered_at=COALESCE(f.created_at,CURRENT_TIMESTAMP),
  last_seen_at=COALESCE(f.created_at,CURRENT_TIMESTAMP),
  updated_at=COALESCE(f.created_at,CURRENT_TIMESTAMP),
  file_key=encode(digest(convert_to(f.node_id::text,'UTF8') || decode('00','hex') || convert_to(f.file_path,'UTF8'),'sha256'),'hex')
FROM gb_channel c WHERE c.id=f.channel_id;
UPDATE gb_recording_file SET
  discovered_at=COALESCE(discovered_at,CURRENT_TIMESTAMP),
  last_seen_at=COALESCE(last_seen_at,CURRENT_TIMESTAMP),
  updated_at=COALESCE(updated_at,CURRENT_TIMESTAMP),
  file_key=COALESCE(file_key,encode(digest(convert_to(node_id::text,'UTF8') || decode('00','hex') || convert_to(file_path,'UTF8'),'sha256'),'hex'));
ALTER TABLE gb_recording_file ALTER COLUMN file_key SET NOT NULL, ALTER COLUMN discovered_at SET NOT NULL;

DROP INDEX IF EXISTS uk_recording_file_node_path;
CREATE UNIQUE INDEX uk_recording_file_key ON gb_recording_file(file_key);
CREATE INDEX idx_recording_file_date_tuple ON gb_recording_file(node_id,vhost,app,stream,record_date);
CREATE INDEX idx_recording_file_missing ON gb_recording_file(missing_at,reconcile_miss_count);

CREATE TABLE gb_recording_reconcile_state (
  node_id bigint PRIMARY KEY,
  status varchar(16) NOT NULL DEFAULT 'queued',
  trigger_source varchar(16) NOT NULL DEFAULT 'scheduled',
  requested_start timestamp, requested_end timestamp,
  effective_start timestamp, effective_end timestamp,
  started_at timestamp, finished_at timestamp,
  candidate_count integer NOT NULL DEFAULT 0, success_count integer NOT NULL DEFAULT 0,
  failure_count integer NOT NULL DEFAULT 0, discovered_count integer NOT NULL DEFAULT 0,
  inserted_count integer NOT NULL DEFAULT 0, updated_count integer NOT NULL DEFAULT 0,
  missing_count integer NOT NULL DEFAULT 0, unattributed_count integer NOT NULL DEFAULT 0,
  last_error varchar(500) NOT NULL DEFAULT '', updated_at timestamp NOT NULL
);

INSERT INTO sys_menu (parent_id,path,name,component,title,icon,sort,type,permission,hide,disable,created_at,updated_at,created_by)
SELECT dm.parent_id,'/gb28181/cloud-recordings','gb28181-cloud-recordings','gb28181/cloud-recordings/index','云端录像','lucide:Cloud',35,2,'gb28181:recording:view',0,0,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
FROM sys_menu dm
WHERE dm.id=(SELECT id FROM sys_menu WHERE path IN ('/gb28181/device-mgmt/index','/gb28181/device-mgmt') AND deleted_at IS NULL ORDER BY CASE WHEN path='/gb28181/device-mgmt/index' THEN 0 ELSE 1 END,id LIMIT 1)
  AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND deleted_at IS NULL);
INSERT INTO sys_menu (parent_id,path,name,component,title,type,permission,hide,created_at,updated_at,created_by)
SELECT m.id,'','','','执行录像对账',3,'gb28181:recording:reconcile',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
FROM sys_menu m WHERE m.path='/gb28181/cloud-recordings' AND m.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:recording:reconcile' AND deleted_at IS NULL);

INSERT INTO sys_api (title,path,method,api_group,created_at,updated_at,created_by)
SELECT v.title,v.path,v.method,'GB28181 云端录像',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM (VALUES
 ('查询云端录像列表','/api/gb28181/cloud-recordings/files','GET'),
 ('查询云端录像选项','/api/gb28181/cloud-recordings/files/options','GET'),
 ('查询云端录像详情','/api/gb28181/cloud-recordings/files/:id','GET'),
 ('申请云端录像访问','/api/gb28181/cloud-recordings/files/:id/access','POST'),
 ('查询正在录像会话','/api/gb28181/cloud-recordings/active','GET'),
 ('查询录像对账状态','/api/gb28181/cloud-recordings/reconciliations','GET'),
 ('触发录像对账','/api/gb28181/cloud-recordings/reconciliations','POST')
) v(title,path,method)
WHERE NOT EXISTS (SELECT 1 FROM sys_api a WHERE a.path=v.path AND a.method=v.method AND a.deleted_at IS NULL);

INSERT INTO sys_role_menu (role_id,menu_id)
SELECT source_role.role_id,recording_menu.id FROM sys_role_menu source_role
JOIN sys_menu device_menu ON device_menu.id=source_role.menu_id CROSS JOIN sys_menu recording_menu
WHERE device_menu.path IN ('/gb28181/device-mgmt/index','/gb28181/device-mgmt') AND device_menu.deleted_at IS NULL
  AND recording_menu.path='/gb28181/cloud-recordings' AND recording_menu.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_role_menu x WHERE x.role_id=source_role.role_id AND x.menu_id=recording_menu.id);
INSERT INTO sys_role_menu (role_id,menu_id)
SELECT 1,m.id FROM sys_menu m WHERE m.permission='gb28181:recording:reconcile' AND m.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_role_menu x WHERE x.role_id=1 AND x.menu_id=m.id);

INSERT INTO sys_menu_api (menu_id,api_id)
SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a
WHERE m.path='/gb28181/cloud-recordings' AND m.deleted_at IS NULL
  AND a.path LIKE '/api/gb28181/cloud-recordings/%' AND a.path<>'/api/gb28181/cloud-recordings/reconciliations'
  AND NOT EXISTS (SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);
INSERT INTO sys_menu_api (menu_id,api_id)
SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a
WHERE m.permission='gb28181:recording:reconcile' AND m.deleted_at IS NULL AND a.path='/api/gb28181/cloud-recordings/reconciliations'
  AND NOT EXISTS (SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);

INSERT INTO sys_casbin_rule (ptype,v0,v1,v2,v3,v4,v5)
SELECT 'p','role_' || rm.role_id::text,a.path,a.method,'*','','' FROM sys_role_menu rm
JOIN sys_menu m ON m.id=rm.menu_id CROSS JOIN sys_api a
WHERE m.path='/gb28181/cloud-recordings' AND a.path LIKE '/api/gb28181/cloud-recordings/%' AND a.path<>'/api/gb28181/cloud-recordings/reconciliations'
  AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule c WHERE c.ptype='p' AND c.v0='role_' || rm.role_id::text AND c.v1=a.path AND c.v2=a.method AND c.v3='*');
INSERT INTO sys_casbin_rule (ptype,v0,v1,v2,v3,v4,v5)
SELECT 'p','role_1',a.path,a.method,'*','','' FROM sys_api a WHERE a.path='/api/gb28181/cloud-recordings/reconciliations'
  AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule c WHERE c.ptype='p' AND c.v0='role_1' AND c.v1=a.path AND c.v2=a.method AND c.v3='*');
