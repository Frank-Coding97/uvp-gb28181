-- A duplicate-key failure prevents accidental rollback while trace data exists.
DROP TEMPORARY TABLE IF EXISTS sip_trace_down_guard;
CREATE TEMPORARY TABLE sip_trace_down_guard (guard_id TINYINT NOT NULL PRIMARY KEY);
INSERT INTO sip_trace_down_guard (guard_id) VALUES (1);

SET @sip_trace_table_exists = (
    SELECT COUNT(*)
    FROM information_schema.tables
    WHERE table_schema = DATABASE() AND table_name = 'gb_sip_trace_message'
);
SET @sip_trace_guard_sql = IF(
    @sip_trace_table_exists = 1,
    'INSERT INTO sip_trace_down_guard (guard_id) SELECT 1 FROM gb_sip_trace_message LIMIT 1',
    'SELECT 1'
);
PREPARE sip_trace_guard_statement FROM @sip_trace_guard_sql;
EXECUTE sip_trace_guard_statement;
DEALLOCATE PREPARE sip_trace_guard_statement;

DROP TABLE IF EXISTS gb_sip_trace_message;
DROP TEMPORARY TABLE IF EXISTS sip_trace_down_guard;
