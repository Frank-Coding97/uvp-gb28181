-- Repair AlarmType values that were stored as 0 because GB/T 28181-2022
-- places AlarmType inside Info. SQL Server 2017+; repeatable and data-preserving.

;WITH parsed AS (
  SELECT
    [id],
    [method],
    TRY_CONVERT(
      int,
      SUBSTRING(
        [raw_summary],
        CHARINDEX('<AlarmType>', [raw_summary]) + LEN('<AlarmType>'),
        CHARINDEX('</AlarmType>', [raw_summary]) - CHARINDEX('<AlarmType>', [raw_summary]) - LEN('<AlarmType>')
      )
    ) AS parsed_type
  FROM [gb_alarm_event]
  WHERE [alarm_type] = 0
    AND CHARINDEX('<Info>', [raw_summary]) > 0
    AND CHARINDEX('</Info>', [raw_summary]) > CHARINDEX('<Info>', [raw_summary])
    AND CHARINDEX('<AlarmType>', [raw_summary]) > 0
    AND CHARINDEX('</AlarmType>', [raw_summary]) > CHARINDEX('<AlarmType>', [raw_summary])
)
UPDATE alarm
SET alarm.[alarm_type] = parsed.parsed_type
FROM [gb_alarm_event] AS alarm
JOIN parsed ON parsed.id = alarm.id
WHERE alarm.[alarm_type] = 0
  AND (
    (parsed.method = 2 AND parsed.parsed_type BETWEEN 1 AND 5)
    OR (parsed.method = 5 AND parsed.parsed_type BETWEEN 1 AND 13)
    OR (parsed.method = 6 AND parsed.parsed_type BETWEEN 1 AND 2)
  );
