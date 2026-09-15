-- device-firmware-upgrade:start
-- Firmware upgrade audit records. Session and idempotency keys are case-sensitive.
IF OBJECT_ID('gb_device_firmware_upgrade','U') IS NULL CREATE TABLE gb_device_firmware_upgrade (
  id BIGINT IDENTITY(1,1) NOT NULL PRIMARY KEY,
  operation_id NVARCHAR(64) COLLATE Latin1_General_100_BIN2 NOT NULL,
  idempotency_key NVARCHAR(128) COLLATE Latin1_General_100_BIN2 NOT NULL,
  device_id BIGINT NOT NULL,
  device_code NVARCHAR(20) COLLATE Latin1_General_100_BIN2 NOT NULL,
  firmware NVARCHAR(255) NOT NULL,
  file_url NVARCHAR(2048) NOT NULL,
  manufacturer NVARCHAR(255) NOT NULL,
  session_id NVARCHAR(128) COLLATE Latin1_General_100_BIN2 NOT NULL,
  sn BIGINT NOT NULL,
  profile_version NVARCHAR(8) NOT NULL,
  profile_charset NVARCHAR(16) NOT NULL,
  sip_status INT DEFAULT 0 NOT NULL,
  sip_call_id NVARCHAR(255) NULL,
  sip_cseq NVARCHAR(64) NULL,
  device_result NVARCHAR(16) NULL,
  device_error NVARCHAR(MAX) NULL,
  status NVARCHAR(16) NOT NULL,
  error_code NVARCHAR(64) NULL,
  error_message NVARCHAR(MAX) NULL,
  failed_reason NVARCHAR(8) NULL,
  current_firmware NVARCHAR(255) NULL,
  actor_id BIGINT DEFAULT 0 NOT NULL,
  actor_dept_id BIGINT DEFAULT 0 NOT NULL,
  created_at DATETIME2(6) NOT NULL,
  updated_at DATETIME2(6) NOT NULL,
  sent_at DATETIME2(6) NULL,
  accepted_at DATETIME2(6) NULL,
  completed_at DATETIME2(6) NULL,
  deadline_at DATETIME2(6) NULL,
  response_at DATETIME2(6) NULL,
  response_call_id NVARCHAR(255) NULL,
  response_cseq NVARCHAR(64) NULL,
  CONSTRAINT uk_firmware_upgrade_operation UNIQUE (operation_id),
  CONSTRAINT uk_firmware_upgrade_device_idempotency UNIQUE (device_id,idempotency_key),
  CONSTRAINT uk_firmware_upgrade_device_session UNIQUE (device_id,session_id),
  CONSTRAINT uk_firmware_upgrade_sn UNIQUE (sn)
);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name='idx_firmware_upgrade_device_sn' AND object_id=OBJECT_ID('gb_device_firmware_upgrade')) CREATE INDEX idx_firmware_upgrade_device_sn ON gb_device_firmware_upgrade (device_code,sn);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name='idx_firmware_upgrade_device_session' AND object_id=OBJECT_ID('gb_device_firmware_upgrade')) CREATE INDEX idx_firmware_upgrade_device_session ON gb_device_firmware_upgrade (device_code,session_id);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name='idx_firmware_upgrade_device_status' AND object_id=OBJECT_ID('gb_device_firmware_upgrade')) CREATE INDEX idx_firmware_upgrade_device_status ON gb_device_firmware_upgrade (device_id,status);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name='idx_firmware_upgrade_device_time' AND object_id=OBJECT_ID('gb_device_firmware_upgrade')) CREATE INDEX idx_firmware_upgrade_device_time ON gb_device_firmware_upgrade (device_id,created_at);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name='idx_firmware_upgrade_deadline' AND object_id=OBJECT_ID('gb_device_firmware_upgrade')) CREATE INDEX idx_firmware_upgrade_deadline ON gb_device_firmware_upgrade (deadline_at);

-- device-firmware-upgrade-permissions:start
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT N'查看设备升级','/api/gb28181/device-mgmt/device/:id/firmware-upgrades','GET',N'GB28181 设备维护',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/device/:id/firmware-upgrades' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT COALESCE((SELECT MIN(id) FROM sys_menu WHERE name='device-mgmt-list' AND deleted_at IS NULL),0),'','GbDeviceMaintenanceView','',N'查看设备维护',1,0,1,3,'gb28181:device:maintenance:view','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:device:maintenance:view' AND deleted_at IS NULL);

INSERT INTO sys_role_menu(role_id,menu_id) SELECT 1,m.id FROM sys_menu m WHERE m.permission='gb28181:device:maintenance:view' AND m.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_role_menu r WHERE r.role_id=1 AND r.menu_id=m.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device:maintenance:view' AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/device/:id/firmware-upgrades' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_casbin_rule(ptype,v0,v1,v2,v3,v4,v5) SELECT 'p',CONCAT('role_',rm.role_id),a.path,a.method,'*','','' FROM sys_role_menu rm JOIN sys_menu m ON m.id=rm.menu_id JOIN sys_menu_api ma ON ma.menu_id=m.id JOIN sys_api a ON a.id=ma.api_id WHERE m.permission='gb28181:device:maintenance:view' AND m.deleted_at IS NULL AND a.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/device/:id/firmware-upgrades' AND a.method='GET' AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule c WHERE c.ptype='p' AND c.v0=CONCAT('role_',rm.role_id) AND c.v1=a.path AND c.v2=a.method AND c.v3='*');

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT N'升级设备','/api/gb28181/device-mgmt/device/:id/firmware-upgrade','POST',N'GB28181 设备维护',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/device/:id/firmware-upgrade' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT COALESCE((SELECT MIN(id) FROM sys_menu WHERE name='device-mgmt-list' AND deleted_at IS NULL),0),'','GbDeviceUpgrade','',N'升级设备',1,0,1,3,'gb28181:device:upgrade','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:device:upgrade' AND deleted_at IS NULL);

INSERT INTO sys_role_menu(role_id,menu_id) SELECT 1,m.id FROM sys_menu m WHERE m.permission='gb28181:device:upgrade' AND m.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_role_menu r WHERE r.role_id=1 AND r.menu_id=m.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device:upgrade' AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/device/:id/firmware-upgrade' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_casbin_rule(ptype,v0,v1,v2,v3,v4,v5) SELECT 'p',CONCAT('role_',rm.role_id),a.path,a.method,'*','','' FROM sys_role_menu rm JOIN sys_menu m ON m.id=rm.menu_id JOIN sys_menu_api ma ON ma.menu_id=m.id JOIN sys_api a ON a.id=ma.api_id WHERE m.permission='gb28181:device:upgrade' AND m.deleted_at IS NULL AND a.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/device/:id/firmware-upgrade' AND a.method='POST' AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule c WHERE c.ptype='p' AND c.v0=CONCAT('role_',rm.role_id) AND c.v1=a.path AND c.v2=a.method AND c.v3='*');

-- device-firmware-upgrade:end
