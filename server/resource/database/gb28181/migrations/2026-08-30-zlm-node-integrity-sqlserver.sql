-- Durable ZLM node revision and fail-closed endpoint recovery state (SQL Server 2017+).
-- Every ALTER is guarded so upgraded and fresh environments are both safe.
IF OBJECT_ID(N'meta_node', N'U') IS NOT NULL AND COL_LENGTH(N'meta_node', N'revision') IS NULL
  ALTER TABLE [meta_node] ADD [revision] BIGINT NOT NULL CONSTRAINT [df_meta_node_revision] DEFAULT 1 WITH VALUES;
IF OBJECT_ID(N'meta_node', N'U') IS NOT NULL AND COL_LENGTH(N'meta_node', N'recovery_required') IS NULL
  ALTER TABLE [meta_node] ADD [recovery_required] BIT NOT NULL CONSTRAINT [df_meta_node_recovery_required] DEFAULT 0 WITH VALUES;
IF OBJECT_ID(N'meta_node', N'U') IS NOT NULL AND COL_LENGTH(N'meta_node', N'recovery_reason') IS NULL
  ALTER TABLE [meta_node] ADD [recovery_reason] NVARCHAR(255) NOT NULL CONSTRAINT [df_meta_node_recovery_reason] DEFAULT N'' WITH VALUES;
IF OBJECT_ID(N'meta_node', N'U') IS NOT NULL AND COL_LENGTH(N'meta_node', N'recovery_fingerprint') IS NULL
  ALTER TABLE [meta_node] ADD [recovery_fingerprint] CHAR(64) NOT NULL CONSTRAINT [df_meta_node_recovery_fingerprint] DEFAULT N'' WITH VALUES;
