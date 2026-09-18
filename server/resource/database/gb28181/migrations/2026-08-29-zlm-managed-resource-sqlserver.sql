-- Management provenance ledger for ZLM resources (SQL Server).
IF OBJECT_ID('gb_zlm_managed_resource','U') IS NULL
CREATE TABLE gb_zlm_managed_resource (
  id BIGINT IDENTITY(1,1) NOT NULL PRIMARY KEY,
  node_id BIGINT NOT NULL,
  resource_type NVARCHAR(32) NOT NULL,
  resource_key NVARCHAR(255) NOT NULL,
  app NVARCHAR(64) NOT NULL DEFAULT '',
  stream NVARCHAR(255) NOT NULL DEFAULT '',
  identity_fingerprint CHAR(64) NOT NULL,
  summary NVARCHAR(512) NOT NULL DEFAULT '',
  created_by BIGINT NOT NULL DEFAULT 0,
  created_at DATETIME2(3) NOT NULL,
  last_observed_at DATETIME2(3) NULL,
  tombstoned_at DATETIME2(3) NULL,
  updated_at DATETIME2(3) NOT NULL
);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id=OBJECT_ID(N'gb_zlm_managed_resource') AND name=N'uk_gb_zlm_managed_resource_identity')
CREATE UNIQUE INDEX uk_gb_zlm_managed_resource_identity ON gb_zlm_managed_resource(node_id,resource_type,resource_key);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id=OBJECT_ID(N'gb_zlm_managed_resource') AND name=N'idx_gb_zlm_managed_resource_observed')
CREATE INDEX idx_gb_zlm_managed_resource_observed ON gb_zlm_managed_resource(node_id,last_observed_at);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id=OBJECT_ID(N'gb_zlm_managed_resource') AND name=N'idx_gb_zlm_managed_resource_tombstone')
CREATE INDEX idx_gb_zlm_managed_resource_tombstone ON gb_zlm_managed_resource(node_id,tombstoned_at);
