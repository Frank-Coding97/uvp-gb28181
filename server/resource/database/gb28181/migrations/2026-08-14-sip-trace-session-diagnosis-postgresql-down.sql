DO $$
DECLARE
    has_diagnosis_rows BOOLEAN;
BEGIN
    IF to_regclass('gb_sip_trace_session_diagnosis') IS NOT NULL THEN
        EXECUTE 'SELECT EXISTS (SELECT 1 FROM gb_sip_trace_session_diagnosis LIMIT 1)' INTO has_diagnosis_rows;
        IF has_diagnosis_rows THEN
            RAISE EXCEPTION 'refusing to drop non-empty gb_sip_trace_session_diagnosis';
        END IF;
    END IF;
END $$;

DROP TABLE IF EXISTS gb_sip_trace_session_diagnosis;
