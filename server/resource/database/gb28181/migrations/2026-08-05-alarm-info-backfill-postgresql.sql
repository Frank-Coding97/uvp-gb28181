-- Repair AlarmType values that were stored as 0 because GB/T 28181-2022
-- places AlarmType inside Info. PostgreSQL 12+; repeatable and data-preserving.

WITH parsed AS (
  SELECT
    id,
    method,
    CAST(substring(raw_summary FROM '<AlarmType>[[:space:]]*([0-9]+)[[:space:]]*</AlarmType>') AS integer) AS parsed_type
  FROM gb_alarm_event
  WHERE alarm_type = 0
    AND raw_summary LIKE '%<Info>%'
    AND raw_summary LIKE '%</Info>%'
    AND raw_summary LIKE '%<AlarmType>%</AlarmType>%'
)
UPDATE gb_alarm_event AS alarm
SET alarm_type = parsed.parsed_type
FROM parsed
WHERE alarm.id = parsed.id
  AND alarm.alarm_type = 0
  AND (
    (parsed.method = 2 AND parsed.parsed_type BETWEEN 1 AND 5)
    OR (parsed.method = 5 AND parsed.parsed_type BETWEEN 1 AND 13)
    OR (parsed.method = 6 AND parsed.parsed_type BETWEEN 1 AND 2)
  );
