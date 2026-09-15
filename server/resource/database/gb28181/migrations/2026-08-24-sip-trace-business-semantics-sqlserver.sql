IF COL_LENGTH(N'gb_sip_trace_message', N'from_id') IS NULL
    ALTER TABLE [gb_sip_trace_message] ADD [from_id] VARCHAR(64) NOT NULL CONSTRAINT [df_sip_trace_from_id] DEFAULT '';
IF COL_LENGTH(N'gb_sip_trace_message', N'to_id') IS NULL
    ALTER TABLE [gb_sip_trace_message] ADD [to_id] VARCHAR(64) NOT NULL CONSTRAINT [df_sip_trace_to_id] DEFAULT '';
IF COL_LENGTH(N'gb_sip_trace_message', N'business_code') IS NULL
    ALTER TABLE [gb_sip_trace_message] ADD [business_code] VARCHAR(64) NOT NULL CONSTRAINT [df_sip_trace_business_code] DEFAULT 'unknown';
IF COL_LENGTH(N'gb_sip_trace_message', N'business_type') IS NULL
    ALTER TABLE [gb_sip_trace_message] ADD [business_type] NVARCHAR(64) NOT NULL CONSTRAINT [df_sip_trace_business_type] DEFAULT N'未知业务';
IF COL_LENGTH(N'gb_sip_trace_message', N'business_confidence') IS NULL
    ALTER TABLE [gb_sip_trace_message] ADD [business_confidence] VARCHAR(16) NOT NULL CONSTRAINT [df_sip_trace_business_confidence] DEFAULT 'none';
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'idx_gb_sip_trace_business_occurred' AND object_id = OBJECT_ID(N'gb_sip_trace_message'))
    CREATE INDEX [idx_gb_sip_trace_business_occurred] ON [gb_sip_trace_message] ([business_code], [occurred_at]);
