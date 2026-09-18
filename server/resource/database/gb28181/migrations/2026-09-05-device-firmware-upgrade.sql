-- device-firmware-upgrade:start
-- Firmware upgrade audit records. Session and idempotency keys are case-sensitive.
CREATE TABLE IF NOT EXISTS gb_device_firmware_upgrade (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  operation_id VARCHAR(64) NOT NULL,
  idempotency_key VARCHAR(128) NOT NULL,
  device_id BIGINT NOT NULL,
  device_code VARCHAR(20) NOT NULL,
  firmware VARCHAR(255) NOT NULL,
  file_url VARCHAR(2048) NOT NULL,
  manufacturer VARCHAR(255) NOT NULL,
  session_id VARCHAR(128) NOT NULL,
  sn BIGINT NOT NULL,
  profile_version VARCHAR(8) NOT NULL,
  profile_charset VARCHAR(16) NOT NULL,
  sip_status INT DEFAULT 0 NOT NULL,
  sip_call_id VARCHAR(255) NULL,
  sip_cseq VARCHAR(64) NULL,
  device_result VARCHAR(16) NULL,
  device_error TEXT NULL,
  status VARCHAR(16) NOT NULL,
  error_code VARCHAR(64) NULL,
  error_message TEXT NULL,
  failed_reason VARCHAR(8) NULL,
  current_firmware VARCHAR(255) NULL,
  actor_id BIGINT DEFAULT 0 NOT NULL,
  actor_dept_id BIGINT DEFAULT 0 NOT NULL,
  created_at DATETIME(6) NOT NULL,
  updated_at DATETIME(6) NOT NULL,
  sent_at DATETIME(6) NULL,
  accepted_at DATETIME(6) NULL,
  completed_at DATETIME(6) NULL,
  deadline_at DATETIME(6) NULL,
  response_at DATETIME(6) NULL,
  response_call_id VARCHAR(255) NULL,
  response_cseq VARCHAR(64) NULL,
  CONSTRAINT uk_firmware_upgrade_operation UNIQUE (operation_id),
  CONSTRAINT uk_firmware_upgrade_device_idempotency UNIQUE (device_id,idempotency_key),
  CONSTRAINT uk_firmware_upgrade_device_session UNIQUE (device_id,session_id),
  CONSTRAINT uk_firmware_upgrade_sn UNIQUE (sn),
  KEY idx_firmware_upgrade_device_sn (device_code,sn),
  KEY idx_firmware_upgrade_device_session (device_code,session_id),
  KEY idx_firmware_upgrade_device_status (device_id,status),
  KEY idx_firmware_upgrade_device_time (device_id,created_at),
  KEY idx_firmware_upgrade_deadline (deadline_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

-- device-firmware-upgrade-permissions:start
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看设备升级','/api/gb28181/device-mgmt/device/:id/firmware-upgrades','GET','GB28181 设备维护',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/device/:id/firmware-upgrades' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT COALESCE((SELECT MIN(id) FROM sys_menu WHERE name='device-mgmt-list' AND deleted_at IS NULL),0),'','GbDeviceMaintenanceView','','查看设备维护',1,0,1,3,'gb28181:device:maintenance:view','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:device:maintenance:view' AND deleted_at IS NULL);

INSERT INTO sys_role_menu(role_id,menu_id) SELECT 1,m.id FROM sys_menu m WHERE m.permission='gb28181:device:maintenance:view' AND m.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_role_menu r WHERE r.role_id=1 AND r.menu_id=m.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device:maintenance:view' AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/device/:id/firmware-upgrades' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_casbin_rule(ptype,v0,v1,v2,v3,v4,v5) SELECT 'p',CONCAT('role_',rm.role_id),a.path,a.method,'*','','' FROM sys_role_menu rm JOIN sys_menu m ON m.id=rm.menu_id JOIN sys_menu_api ma ON ma.menu_id=m.id JOIN sys_api a ON a.id=ma.api_id WHERE m.permission='gb28181:device:maintenance:view' AND m.deleted_at IS NULL AND a.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/device/:id/firmware-upgrades' AND a.method='GET' AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule c WHERE c.ptype='p' AND c.v0=CONCAT('role_',rm.role_id) AND c.v1=a.path AND c.v2=a.method AND c.v3='*');

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '升级设备','/api/gb28181/device-mgmt/device/:id/firmware-upgrade','POST','GB28181 设备维护',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/device/:id/firmware-upgrade' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT COALESCE((SELECT MIN(id) FROM sys_menu WHERE name='device-mgmt-list' AND deleted_at IS NULL),0),'','GbDeviceUpgrade','','升级设备',1,0,1,3,'gb28181:device:upgrade','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:device:upgrade' AND deleted_at IS NULL);

INSERT INTO sys_role_menu(role_id,menu_id) SELECT 1,m.id FROM sys_menu m WHERE m.permission='gb28181:device:upgrade' AND m.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_role_menu r WHERE r.role_id=1 AND r.menu_id=m.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device:upgrade' AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/device/:id/firmware-upgrade' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_casbin_rule(ptype,v0,v1,v2,v3,v4,v5) SELECT 'p',CONCAT('role_',rm.role_id),a.path,a.method,'*','','' FROM sys_role_menu rm JOIN sys_menu m ON m.id=rm.menu_id JOIN sys_menu_api ma ON ma.menu_id=m.id JOIN sys_api a ON a.id=ma.api_id WHERE m.permission='gb28181:device:upgrade' AND m.deleted_at IS NULL AND a.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/device/:id/firmware-upgrade' AND a.method='POST' AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule c WHERE c.ptype='p' AND c.v0=CONCAT('role_',rm.role_id) AND c.v1=a.path AND c.v2=a.method AND c.v3='*');

-- device-firmware-upgrade:end
