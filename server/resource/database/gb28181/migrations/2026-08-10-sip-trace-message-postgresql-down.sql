DO $$
DECLARE
    has_trace_rows BOOLEAN;
BEGIN
    IF to_regclass('gb_sip_trace_message') IS NOT NULL THEN
        EXECUTE 'SELECT EXISTS (SELECT 1 FROM gb_sip_trace_message LIMIT 1)' INTO has_trace_rows;
        IF has_trace_rows THEN
            RAISE EXCEPTION 'refusing to drop non-empty gb_sip_trace_message';
        END IF;
    END IF;
END $$;

DROP TABLE IF EXISTS gb_sip_trace_message;
