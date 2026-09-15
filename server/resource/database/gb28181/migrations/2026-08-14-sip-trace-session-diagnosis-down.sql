DROP TEMPORARY TABLE IF EXISTS sip_trace_session_diagnosis_down_guard;
CREATE TEMPORARY TABLE sip_trace_session_diagnosis_down_guard (guard_id TINYINT NOT NULL PRIMARY KEY);
INSERT INTO sip_trace_session_diagnosis_down_guard (guard_id) VALUES (1);

SET @sip_trace_diagnosis_table_exists = (
    SELECT COUNT(*)
    FROM information_schema.tables
    WHERE table_schema = DATABASE() AND table_name = 'gb_sip_trace_session_diagnosis'
);
SET @sip_trace_diagnosis_guard_sql = IF(
    @sip_trace_diagnosis_table_exists = 1,
    'INSERT INTO sip_trace_session_diagnosis_down_guard (guard_id) SELECT 1 FROM gb_sip_trace_session_diagnosis LIMIT 1',
    'SELECT 1'
);
PREPARE sip_trace_diagnosis_guard_statement FROM @sip_trace_diagnosis_guard_sql;
EXECUTE sip_trace_diagnosis_guard_statement;
DEALLOCATE PREPARE sip_trace_diagnosis_guard_statement;

DROP TABLE IF EXISTS gb_sip_trace_session_diagnosis;
DROP TEMPORARY TABLE IF EXISTS sip_trace_session_diagnosis_down_guard;
