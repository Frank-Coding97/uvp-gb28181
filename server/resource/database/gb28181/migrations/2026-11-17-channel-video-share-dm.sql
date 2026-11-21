-- Keep internal block semicolons off line ends: the production executor splits there.
-- Catalog guards are scoped to the configured schema, independent of compatibility mode.
DECLARE object_count INTEGER; BEGIN SELECT COUNT(*) INTO object_count FROM ALL_TABLES WHERE OWNER = SYS_CONTEXT('USERENV', 'CURRENT_SCHEMA') AND TABLE_NAME = 'GB_CHANNEL_VIDEO_SHARE'; IF object_count = 0 THEN EXECUTE IMMEDIATE 'CREATE TABLE gb_channel_video_share (
  id BIGINT IDENTITY(1,1) PRIMARY KEY,
  channel_pk NUMBER(19) NOT NULL UNIQUE,
  public_id VARCHAR(64) NOT NULL UNIQUE,
  generation VARCHAR(64) NOT NULL,
  enabled NUMBER(1) DEFAULT 0 NOT NULL,
  activated_at TIMESTAMP NULL,
  expires_at TIMESTAMP NULL,
  created_by NUMBER(19) NULL,
  device_pk NUMBER(19) NOT NULL,
  device_id VARCHAR(20) NOT NULL,
  device_epoch NUMBER(19) DEFAULT 0 NOT NULL,
  source VARCHAR(16) NOT NULL,
  policy_actor_id NUMBER(19) NULL,
  policy_revision NUMBER(19) DEFAULT 0 NOT NULL,
  key_id VARCHAR(64) NOT NULL,
  revision NUMBER(19) DEFAULT 1 NOT NULL,
  revoked_at TIMESTAMP NULL,
  revoke_reason VARCHAR(64) DEFAULT '''' NOT NULL,
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP NOT NULL
)'; END IF; END;
DECLARE object_count INTEGER; BEGIN SELECT COUNT(*) INTO object_count FROM ALL_INDEXES WHERE OWNER = SYS_CONTEXT('USERENV', 'CURRENT_SCHEMA') AND INDEX_NAME = 'IDX_VIDEO_SHARE_ENABLED_EXPIRY'; IF object_count = 0 THEN EXECUTE IMMEDIATE 'CREATE INDEX idx_video_share_enabled_expiry ON gb_channel_video_share (enabled, expires_at)'; END IF; END;
DECLARE object_count INTEGER; BEGIN SELECT COUNT(*) INTO object_count FROM ALL_INDEXES WHERE OWNER = SYS_CONTEXT('USERENV', 'CURRENT_SCHEMA') AND INDEX_NAME = 'IDX_VIDEO_SHARE_CREATED_BY'; IF object_count = 0 THEN EXECUTE IMMEDIATE 'CREATE INDEX idx_video_share_created_by ON gb_channel_video_share (created_by)'; END IF; END;
DECLARE object_count INTEGER; BEGIN SELECT COUNT(*) INTO object_count FROM ALL_INDEXES WHERE OWNER = SYS_CONTEXT('USERENV', 'CURRENT_SCHEMA') AND INDEX_NAME = 'IDX_VIDEO_SHARE_DEVICE_PK'; IF object_count = 0 THEN EXECUTE IMMEDIATE 'CREATE INDEX idx_video_share_device_pk ON gb_channel_video_share (device_pk)'; END IF; END;
DECLARE object_count INTEGER; BEGIN SELECT COUNT(*) INTO object_count FROM ALL_TABLES WHERE OWNER = SYS_CONTEXT('USERENV', 'CURRENT_SCHEMA') AND TABLE_NAME = 'GB_DEVICE_VIDEO_SHARE_POLICY'; IF object_count = 0 THEN EXECUTE IMMEDIATE 'CREATE TABLE gb_device_video_share_policy (
  id BIGINT IDENTITY(1,1) PRIMARY KEY,
  device_pk NUMBER(19) NOT NULL UNIQUE,
  default_enabled NUMBER(1) DEFAULT 0 NOT NULL,
  source VARCHAR(16) NOT NULL,
  policy_actor_id NUMBER(19) NULL,
  policy_revision NUMBER(19) DEFAULT 0 NOT NULL,
  initialized_at TIMESTAMP NOT NULL,
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP NOT NULL
)'; END IF; END;
DECLARE object_count INTEGER; BEGIN SELECT COUNT(*) INTO object_count FROM ALL_TABLES WHERE OWNER = SYS_CONTEXT('USERENV', 'CURRENT_SCHEMA') AND TABLE_NAME = 'GB_VIDEO_SHARE_AUTHORITY'; IF object_count = 0 THEN EXECUTE IMMEDIATE 'CREATE TABLE gb_video_share_authority (id NUMBER(19) NOT NULL PRIMARY KEY, revision NUMBER(19) DEFAULT 0 NOT NULL)'; END IF; END;
INSERT INTO gb_video_share_authority (id, revision) SELECT 1, 0 FROM DUAL WHERE NOT EXISTS (SELECT 1 FROM gb_video_share_authority WHERE id = 1);
