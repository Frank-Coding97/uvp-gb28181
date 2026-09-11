-- Cloud recording catalog schema (SQL Server 2019+).
ALTER TABLE gb_recording_file ALTER COLUMN start_time datetime2 NULL;
ALTER TABLE gb_recording_file ALTER COLUMN time_len decimal(12,3) NULL;
ALTER TABLE gb_recording_file ALTER COLUMN file_size bigint NULL;
ALTER TABLE gb_recording_file ADD channel_code nvarchar(20) NOT NULL CONSTRAINT DF_recording_file_channel_code DEFAULT '', channel_name nvarchar(255) NOT NULL CONSTRAINT DF_recording_file_channel_name DEFAULT '', device_name nvarchar(255) NOT NULL CONSTRAINT DF_recording_file_device_name DEFAULT '', owner_dept_id bigint NOT NULL CONSTRAINT DF_recording_file_owner_dept DEFAULT 0, file_key varchar(64) NULL, source varchar(16) NOT NULL CONSTRAINT DF_recording_file_source DEFAULT 'hook', metadata_state varchar(16) NOT NULL CONSTRAINT DF_recording_file_metadata DEFAULT 'complete', record_date date NULL, discovered_at datetime2 NULL, last_seen_at datetime2 NULL, missing_at datetime2 NULL, reconcile_miss_count int NOT NULL CONSTRAINT DF_recording_file_miss DEFAULT 0, updated_at datetime2 NULL;
UPDATE f SET channel_code=ISNULL(c.channel_id,''), channel_name=ISNULL(NULLIF(c.alias,''),ISNULL(c.name,'')), device_name=ISNULL(d.name,''), owner_dept_id=ISNULL(c.owner_dept_id,0), record_date=CONVERT(date,f.start_time), discovered_at=ISNULL(f.created_at,SYSUTCDATETIME()), last_seen_at=ISNULL(f.created_at,SYSUTCDATETIME()), updated_at=ISNULL(f.created_at,SYSUTCDATETIME()), file_key=LOWER(CONVERT(varchar(64),HASHBYTES('SHA2_256',CONVERT(varbinary(max),CONVERT(varchar(max),CONCAT(CONVERT(varchar(20),f.node_id),CHAR(0),f.file_path)) COLLATE Latin1_General_100_BIN2_UTF8)),2)) FROM gb_recording_file f LEFT JOIN gb_channel c ON c.id=f.channel_id LEFT JOIN gb_device d ON d.device_id=f.device_id;
UPDATE gb_recording_file SET discovered_at=ISNULL(discovered_at,SYSUTCDATETIME()), last_seen_at=ISNULL(last_seen_at,SYSUTCDATETIME()), updated_at=ISNULL(updated_at,SYSUTCDATETIME()), file_key=ISNULL(file_key,LOWER(CONVERT(varchar(64),HASHBYTES('SHA2_256',CONVERT(varbinary(max),CONVERT(varchar(max),CONCAT(CONVERT(varchar(20),node_id),CHAR(0),file_path)) COLLATE Latin1_General_100_BIN2_UTF8)),2)));
ALTER TABLE gb_recording_file ALTER COLUMN file_key varchar(64) NOT NULL; ALTER TABLE gb_recording_file ALTER COLUMN discovered_at datetime2 NOT NULL;
DROP INDEX IF EXISTS uk_recording_file_node_path ON gb_recording_file;
CREATE UNIQUE INDEX uk_recording_file_key ON gb_recording_file(file_key) WHERE file_key IS NOT NULL;
CREATE INDEX idx_recording_file_date_tuple ON gb_recording_file(node_id,vhost,app,stream,record_date);
CREATE INDEX idx_recording_file_missing ON gb_recording_file(missing_at,reconcile_miss_count);
CREATE TABLE gb_recording_reconcile_state (node_id bigint NOT NULL PRIMARY KEY, status varchar(16) NOT NULL DEFAULT 'queued', trigger_source varchar(16) NOT NULL DEFAULT 'scheduled', requested_start datetime2 NULL, requested_end datetime2 NULL, effective_start datetime2 NULL, effective_end datetime2 NULL, started_at datetime2 NULL, finished_at datetime2 NULL, candidate_count int NOT NULL DEFAULT 0, success_count int NOT NULL DEFAULT 0, failure_count int NOT NULL DEFAULT 0, discovered_count int NOT NULL DEFAULT 0, inserted_count int NOT NULL DEFAULT 0, updated_count int NOT NULL DEFAULT 0, missing_count int NOT NULL DEFAULT 0, unattributed_count int NOT NULL DEFAULT 0, last_error varchar(500) NOT NULL DEFAULT '', updated_at datetime2 NOT NULL);

INSERT INTO [sys_menu] ([parent_id],[path],[name],[component],[title],[icon],[sort],[type],[permission],[hide],[disable],[created_at],[updated_at],[created_by])
SELECT dm.[parent_id],'/gb28181/cloud-recordings','gb28181-cloud-recordings','gb28181/cloud-recordings/index',N'云端录像','lucide:Cloud',35,2,'gb28181:recording:view',0,0,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
FROM [sys_menu] dm WHERE dm.[id]=(SELECT TOP 1 [id] FROM [sys_menu] WHERE [path] IN ('/gb28181/device-mgmt/index','/gb28181/device-mgmt') AND [deleted_at] IS NULL ORDER BY CASE WHEN [path]='/gb28181/device-mgmt/index' THEN 0 ELSE 1 END,[id])
  AND NOT EXISTS (SELECT 1 FROM [sys_menu] WHERE [path]='/gb28181/cloud-recordings' AND [deleted_at] IS NULL);
INSERT INTO [sys_menu] ([parent_id],[path],[name],[component],[title],[type],[permission],[hide],[created_at],[updated_at],[created_by])
SELECT m.[id],'','','',N'执行录像对账',3,'gb28181:recording:reconcile',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM [sys_menu] m
WHERE m.[path]='/gb28181/cloud-recordings' AND m.[deleted_at] IS NULL AND NOT EXISTS (SELECT 1 FROM [sys_menu] WHERE [permission]='gb28181:recording:reconcile' AND [deleted_at] IS NULL);

INSERT INTO [sys_api] ([title],[path],[method],[api_group],[created_at],[updated_at],[created_by])
SELECT v.title,v.path,v.method,N'GB28181 云端录像',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM (VALUES
 (N'查询云端录像列表','/api/gb28181/cloud-recordings/files','GET'),
 (N'查询云端录像选项','/api/gb28181/cloud-recordings/files/options','GET'),
 (N'查询云端录像详情','/api/gb28181/cloud-recordings/files/:id','GET'),
 (N'申请云端录像访问','/api/gb28181/cloud-recordings/files/:id/access','POST'),
 (N'查询正在录像会话','/api/gb28181/cloud-recordings/active','GET'),
 (N'查询录像对账状态','/api/gb28181/cloud-recordings/reconciliations','GET'),
 (N'触发录像对账','/api/gb28181/cloud-recordings/reconciliations','POST')
) v(title,path,method) WHERE NOT EXISTS (SELECT 1 FROM [sys_api] a WHERE a.[path]=v.path AND a.[method]=v.method AND a.[deleted_at] IS NULL);

INSERT INTO [sys_role_menu] ([role_id],[menu_id])
SELECT source_role.[role_id],recording_menu.[id] FROM [sys_role_menu] source_role JOIN [sys_menu] device_menu ON device_menu.[id]=source_role.[menu_id] CROSS JOIN [sys_menu] recording_menu
WHERE device_menu.[path] IN ('/gb28181/device-mgmt/index','/gb28181/device-mgmt') AND device_menu.[deleted_at] IS NULL AND recording_menu.[path]='/gb28181/cloud-recordings' AND recording_menu.[deleted_at] IS NULL
  AND NOT EXISTS (SELECT 1 FROM [sys_role_menu] x WHERE x.[role_id]=source_role.[role_id] AND x.[menu_id]=recording_menu.[id]);
INSERT INTO [sys_role_menu] ([role_id],[menu_id]) SELECT 1,m.[id] FROM [sys_menu] m WHERE m.[permission]='gb28181:recording:reconcile' AND m.[deleted_at] IS NULL
  AND NOT EXISTS (SELECT 1 FROM [sys_role_menu] x WHERE x.[role_id]=1 AND x.[menu_id]=m.[id]);

INSERT INTO [sys_menu_api] ([menu_id],[api_id])
SELECT m.[id],a.[id] FROM [sys_menu] m CROSS JOIN [sys_api] a WHERE m.[path]='/gb28181/cloud-recordings' AND m.[deleted_at] IS NULL AND a.[path] LIKE '/api/gb28181/cloud-recordings/%' AND a.[path]<>'/api/gb28181/cloud-recordings/reconciliations'
  AND NOT EXISTS (SELECT 1 FROM [sys_menu_api] x WHERE x.[menu_id]=m.[id] AND x.[api_id]=a.[id]);
INSERT INTO [sys_menu_api] ([menu_id],[api_id])
SELECT m.[id],a.[id] FROM [sys_menu] m CROSS JOIN [sys_api] a WHERE m.[permission]='gb28181:recording:reconcile' AND m.[deleted_at] IS NULL AND a.[path]='/api/gb28181/cloud-recordings/reconciliations'
  AND NOT EXISTS (SELECT 1 FROM [sys_menu_api] x WHERE x.[menu_id]=m.[id] AND x.[api_id]=a.[id]);

INSERT INTO [sys_casbin_rule] ([ptype],[v0],[v1],[v2],[v3],[v4],[v5])
SELECT 'p','role_' + CAST(rm.[role_id] AS varchar(20)),a.[path],a.[method],'*','','' FROM [sys_role_menu] rm JOIN [sys_menu] m ON m.[id]=rm.[menu_id] CROSS JOIN [sys_api] a
WHERE m.[path]='/gb28181/cloud-recordings' AND a.[path] LIKE '/api/gb28181/cloud-recordings/%' AND a.[path]<>'/api/gb28181/cloud-recordings/reconciliations'
  AND NOT EXISTS (SELECT 1 FROM [sys_casbin_rule] c WHERE c.[ptype]='p' AND c.[v0]='role_' + CAST(rm.[role_id] AS varchar(20)) AND c.[v1]=a.[path] AND c.[v2]=a.[method] AND c.[v3]='*');
INSERT INTO [sys_casbin_rule] ([ptype],[v0],[v1],[v2],[v3],[v4],[v5])
SELECT 'p','role_1',a.[path],a.[method],'*','','' FROM [sys_api] a WHERE a.[path]='/api/gb28181/cloud-recordings/reconciliations'
  AND NOT EXISTS (SELECT 1 FROM [sys_casbin_rule] c WHERE c.[ptype]='p' AND c.[v0]='role_1' AND c.[v1]=a.[path] AND c.[v2]=a.[method] AND c.[v3]='*');
