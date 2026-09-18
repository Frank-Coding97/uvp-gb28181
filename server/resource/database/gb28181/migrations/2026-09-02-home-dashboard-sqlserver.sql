IF OBJECT_ID('gb_dashboard_layout','U') IS NULL CREATE TABLE gb_dashboard_layout (
  id BIGINT IDENTITY(1,1) NOT NULL PRIMARY KEY, user_id BIGINT NOT NULL, dashboard_key NVARCHAR(32) NOT NULL,
  schema_version INT NOT NULL, revision BIGINT NOT NULL CONSTRAINT df_dashboard_layout_revision DEFAULT 1,
  layout_json NVARCHAR(MAX) NOT NULL, created_at DATETIME2(3) NOT NULL, updated_at DATETIME2(3) NOT NULL,
  CONSTRAINT uk_dashboard_layout_user_key UNIQUE (user_id,dashboard_key)
);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name='idx_dashboard_layout_updated' AND object_id=OBJECT_ID('gb_dashboard_layout')) CREATE INDEX idx_dashboard_layout_updated ON gb_dashboard_layout(updated_at);

IF OBJECT_ID('gb_sip_metric_minute','U') IS NULL CREATE TABLE gb_sip_metric_minute (
  id BIGINT IDENTITY(1,1) NOT NULL PRIMARY KEY, bucket_start DATETIME2(3) NOT NULL, method NVARCHAR(16) NOT NULL,
  direction NVARCHAR(8) NOT NULL, request_count BIGINT NOT NULL CONSTRAINT df_sip_metric_request_count DEFAULT 0,
  transaction_count BIGINT NOT NULL CONSTRAINT df_sip_metric_transaction_count DEFAULT 0,
  transaction_success BIGINT NOT NULL CONSTRAINT df_sip_metric_transaction_success DEFAULT 0,
  transaction_failure BIGINT NOT NULL CONSTRAINT df_sip_metric_transaction_failure DEFAULT 0,
  created_at DATETIME2(3) NOT NULL, updated_at DATETIME2(3) NOT NULL,
  CONSTRAINT uk_sip_metric_minute_bucket UNIQUE (bucket_start,method,direction)
);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name='idx_sip_metric_minute_bucket' AND object_id=OBJECT_ID('gb_sip_metric_minute')) CREATE INDEX idx_sip_metric_minute_bucket ON gb_sip_metric_minute(bucket_start);

IF OBJECT_ID('gb_sip_metric_flush','U') IS NULL CREATE TABLE gb_sip_metric_flush (
  id BIGINT IDENTITY(1,1) NOT NULL PRIMARY KEY, flush_id NVARCHAR(64) NOT NULL, created_at DATETIME2(3) NOT NULL,
  CONSTRAINT uk_sip_metric_flush_id UNIQUE (flush_id)
);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name='idx_sip_metric_flush_created' AND object_id=OBJECT_ID('gb_sip_metric_flush')) CREATE INDEX idx_sip_metric_flush_created ON gb_sip_metric_flush(created_at);

IF OBJECT_ID('gb_sip_metric_gap','U') IS NULL CREATE TABLE gb_sip_metric_gap (
  id BIGINT IDENTITY(1,1) NOT NULL PRIMARY KEY, started_at DATETIME2(3) NOT NULL, ended_at DATETIME2(3) NOT NULL,
  reason NVARCHAR(32) NOT NULL, dropped_count BIGINT NOT NULL CONSTRAINT df_sip_metric_gap_dropped DEFAULT 0,
  created_at DATETIME2(3) NOT NULL
);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name='idx_sip_metric_gap_window' AND object_id=OBJECT_ID('gb_sip_metric_gap')) CREATE INDEX idx_sip_metric_gap_window ON gb_sip_metric_gap(started_at,ended_at);

IF OBJECT_ID('gb_play_attempt','U') IS NULL CREATE TABLE gb_play_attempt (
  id BIGINT IDENTITY(1,1) NOT NULL PRIMARY KEY, correlation_id NVARCHAR(64) NOT NULL, user_id BIGINT NOT NULL,
  device_code NVARCHAR(20) NOT NULL, channel_code NVARCHAR(20) NOT NULL, node_id BIGINT NOT NULL CONSTRAINT df_play_attempt_node DEFAULT 0,
  reused BIT NOT NULL CONSTRAINT df_play_attempt_reused DEFAULT 0, outcome NVARCHAR(32) NOT NULL,
  failure_stage NVARCHAR(32) NOT NULL CONSTRAINT df_play_attempt_failure_stage DEFAULT '', started_at DATETIME2(3) NOT NULL,
  finished_at DATETIME2(3) NULL, created_at DATETIME2(3) NOT NULL, updated_at DATETIME2(3) NOT NULL,
  CONSTRAINT uk_play_attempt_correlation UNIQUE (correlation_id)
);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name='idx_play_attempt_user_started' AND object_id=OBJECT_ID('gb_play_attempt')) CREATE INDEX idx_play_attempt_user_started ON gb_play_attempt(user_id,started_at);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name='idx_play_attempt_device_started' AND object_id=OBJECT_ID('gb_play_attempt')) CREATE INDEX idx_play_attempt_device_started ON gb_play_attempt(device_code,started_at);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name='idx_play_attempt_outcome_started' AND object_id=OBJECT_ID('gb_play_attempt')) CREATE INDEX idx_play_attempt_outcome_started ON gb_play_attempt(outcome,started_at);
