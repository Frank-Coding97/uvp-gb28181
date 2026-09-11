-- Repair AlarmType values that were stored as 0 because GB/T 28181-2022
-- places AlarmType inside Info. MySQL 5.7+; repeatable and data-preserving.

UPDATE `gb_alarm_event`
SET `alarm_type` = CAST(
  SUBSTRING_INDEX(
    SUBSTRING_INDEX(SUBSTRING_INDEX(`raw_summary`, '<Info>', -1), '</AlarmType>', 1),
    '<AlarmType>',
    -1
  ) AS UNSIGNED
)
WHERE `alarm_type` = 0
  AND `raw_summary` LIKE '%<Info>%'
  AND `raw_summary` LIKE '%</Info>%'
  AND `raw_summary` LIKE '%<AlarmType>%</AlarmType>%'
  AND (
    (`method` = 2 AND CAST(SUBSTRING_INDEX(SUBSTRING_INDEX(SUBSTRING_INDEX(`raw_summary`, '<Info>', -1), '</AlarmType>', 1), '<AlarmType>', -1) AS UNSIGNED) BETWEEN 1 AND 5)
    OR (`method` = 5 AND CAST(SUBSTRING_INDEX(SUBSTRING_INDEX(SUBSTRING_INDEX(`raw_summary`, '<Info>', -1), '</AlarmType>', 1), '<AlarmType>', -1) AS UNSIGNED) BETWEEN 1 AND 13)
    OR (`method` = 6 AND CAST(SUBSTRING_INDEX(SUBSTRING_INDEX(SUBSTRING_INDEX(`raw_summary`, '<Info>', -1), '</AlarmType>', 1), '<AlarmType>', -1) AS UNSIGNED) BETWEEN 1 AND 2)
  );
