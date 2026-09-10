-- Persist the independent server-side recording directory and claim version.
-- Existing work-recording and recorder-claim tables are required; no table is created here.
-- MySQL 5.7+, repeatable.
SET @schema_name := DATABASE();

SET @required_tables := (
  SELECT COUNT(*)
  FROM information_schema.tables
  WHERE table_schema = @schema_name
    AND table_type = 'BASE TABLE'
    AND table_name IN ('gb_work_recording', 'gb_recorder_claim')
);
SET @sql := IF(
  @required_tables = 2,
  'SELECT 1',
  'SELECT * FROM `__gb_work_recording_directory_prerequisite_missing__`'
);
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @sql := IF(
  (SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = @schema_name AND table_name = 'gb_work_recording' AND column_name = 'recording_root') = 0,
  'ALTER TABLE `gb_work_recording` ADD COLUMN `recording_root` varchar(1024) NOT NULL DEFAULT ''''',
  'SELECT 1'
);
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @sql := IF(
  (SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = @schema_name AND table_name = 'gb_work_recording' AND column_name = 'recorder_claim_version') = 0,
  'ALTER TABLE `gb_work_recording` ADD COLUMN `recorder_claim_version` bigint unsigned NOT NULL DEFAULT 0',
  'SELECT 1'
);
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @sql := IF(
  (SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = @schema_name AND table_name = 'gb_recorder_claim' AND column_name = 'recording_root') = 0,
  'ALTER TABLE `gb_recorder_claim` ADD COLUMN `recording_root` varchar(1024) NOT NULL DEFAULT ''''',
  'SELECT 1'
);
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
