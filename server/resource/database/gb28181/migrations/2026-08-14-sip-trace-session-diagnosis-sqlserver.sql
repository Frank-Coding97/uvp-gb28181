IF OBJECT_ID(N'gb_sip_trace_session_diagnosis', N'U') IS NULL
BEGIN
    CREATE TABLE [gb_sip_trace_session_diagnosis] (
        [id] BIGINT IDENTITY(1,1) NOT NULL,
        [session_day] DATE NOT NULL,
        [observed_at] DATETIME2(6) NOT NULL,
        [correlation_key] VARCHAR(128) NOT NULL,
        [state] VARCHAR(16) NOT NULL CONSTRAINT [df_sip_trace_diagnosis_state] DEFAULT 'active',
        [category] VARCHAR(32) NOT NULL,
        [code] VARCHAR(64) NOT NULL,
        [stage] VARCHAR(32) NOT NULL,
        [source] VARCHAR(32) NOT NULL,
        [device_id] VARCHAR(64) NOT NULL CONSTRAINT [df_sip_trace_diagnosis_device_id] DEFAULT '',
        [channel_id] VARCHAR(64) NOT NULL CONSTRAINT [df_sip_trace_diagnosis_channel_id] DEFAULT '',
        [call_id] VARCHAR(255) NOT NULL CONSTRAINT [df_sip_trace_diagnosis_call_id] DEFAULT '',
        [cseq] INT NOT NULL CONSTRAINT [df_sip_trace_diagnosis_cseq] DEFAULT 0,
        [method] VARCHAR(32) NOT NULL CONSTRAINT [df_sip_trace_diagnosis_method] DEFAULT '',
        [status_code] SMALLINT NOT NULL CONSTRAINT [df_sip_trace_diagnosis_status_code] DEFAULT 0,
        [stream_id] VARCHAR(255) NOT NULL CONSTRAINT [df_sip_trace_diagnosis_stream_id] DEFAULT '',
        [resolved_at] DATETIME2(6) NULL,
        CONSTRAINT [pk_sip_trace_session_diagnosis] PRIMARY KEY ([id]),
        CONSTRAINT [uk_sip_trace_diagnosis_session] UNIQUE ([session_day], [category], [correlation_key])
    );
END;
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'idx_sip_trace_diagnosis_category_state_observed' AND object_id = OBJECT_ID(N'gb_sip_trace_session_diagnosis'))
    CREATE INDEX [idx_sip_trace_diagnosis_category_state_observed] ON [gb_sip_trace_session_diagnosis] ([session_day], [category], [state], [observed_at]);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'idx_sip_trace_diagnosis_device_observed' AND object_id = OBJECT_ID(N'gb_sip_trace_session_diagnosis'))
    CREATE INDEX [idx_sip_trace_diagnosis_device_observed] ON [gb_sip_trace_session_diagnosis] ([device_id], [observed_at]);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'idx_sip_trace_diagnosis_call_cseq' AND object_id = OBJECT_ID(N'gb_sip_trace_session_diagnosis'))
    CREATE INDEX [idx_sip_trace_diagnosis_call_cseq] ON [gb_sip_trace_session_diagnosis] ([call_id], [cseq]);
