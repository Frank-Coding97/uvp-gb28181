IF OBJECT_ID(N'gb_sip_trace_message', N'U') IS NOT NULL
BEGIN
    EXEC sp_executesql N'
        IF EXISTS (SELECT 1 FROM gb_sip_trace_message)
            THROW 50000, ''refusing to drop non-empty gb_sip_trace_message'', 1;
    ';
END;

IF OBJECT_ID(N'gb_sip_trace_message', N'U') IS NOT NULL DROP TABLE gb_sip_trace_message;
