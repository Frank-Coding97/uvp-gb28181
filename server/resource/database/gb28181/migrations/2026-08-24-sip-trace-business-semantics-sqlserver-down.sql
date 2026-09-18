IF EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'idx_gb_sip_trace_business_occurred' AND object_id = OBJECT_ID(N'gb_sip_trace_message'))
    DROP INDEX [idx_gb_sip_trace_business_occurred] ON [gb_sip_trace_message];
IF EXISTS (SELECT 1 FROM sys.default_constraints WHERE name = N'df_sip_trace_business_confidence') ALTER TABLE [gb_sip_trace_message] DROP CONSTRAINT [df_sip_trace_business_confidence];
IF COL_LENGTH(N'gb_sip_trace_message', N'business_confidence') IS NOT NULL ALTER TABLE [gb_sip_trace_message] DROP COLUMN [business_confidence];
IF EXISTS (SELECT 1 FROM sys.default_constraints WHERE name = N'df_sip_trace_business_type') ALTER TABLE [gb_sip_trace_message] DROP CONSTRAINT [df_sip_trace_business_type];
IF COL_LENGTH(N'gb_sip_trace_message', N'business_type') IS NOT NULL ALTER TABLE [gb_sip_trace_message] DROP COLUMN [business_type];
IF EXISTS (SELECT 1 FROM sys.default_constraints WHERE name = N'df_sip_trace_business_code') ALTER TABLE [gb_sip_trace_message] DROP CONSTRAINT [df_sip_trace_business_code];
IF COL_LENGTH(N'gb_sip_trace_message', N'business_code') IS NOT NULL ALTER TABLE [gb_sip_trace_message] DROP COLUMN [business_code];
IF EXISTS (SELECT 1 FROM sys.default_constraints WHERE name = N'df_sip_trace_to_id') ALTER TABLE [gb_sip_trace_message] DROP CONSTRAINT [df_sip_trace_to_id];
IF COL_LENGTH(N'gb_sip_trace_message', N'to_id') IS NOT NULL ALTER TABLE [gb_sip_trace_message] DROP COLUMN [to_id];
IF EXISTS (SELECT 1 FROM sys.default_constraints WHERE name = N'df_sip_trace_from_id') ALTER TABLE [gb_sip_trace_message] DROP CONSTRAINT [df_sip_trace_from_id];
IF COL_LENGTH(N'gb_sip_trace_message', N'from_id') IS NOT NULL ALTER TABLE [gb_sip_trace_message] DROP COLUMN [from_id];
