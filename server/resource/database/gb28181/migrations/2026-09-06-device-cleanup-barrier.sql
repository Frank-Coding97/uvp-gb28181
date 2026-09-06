-- Device access cleanup barrier (MySQL 5.7+/8.0).
-- access_epoch is the requested waterline. A newly added completed waterline
-- intentionally stays at 1, so upgraded rows with access_epoch > 1 remain
-- pending until the cleanup worker records completion.

SET @cleanup_schema_name := DATABASE();
SET @cleanup_access_epoch_exists := (
  SELECT COUNT(*)
  FROM information_schema.columns
  WHERE table_schema = @cleanup_schema_name
    AND table_name = 'gb_device'
    AND column_name = 'access_epoch'
);
-- A missing prerequisite deliberately executes an invalid column probe. This
-- is the MySQL-compatible fail-closed guard for ordinary migration scripts.
SET @cleanup_barrier_sql := IF(
  @cleanup_access_epoch_exists = 1,
  'SELECT 1',
  'SELECT access_epoch FROM `gb_device` LIMIT 0'
);
PREPARE cleanup_barrier_stmt FROM @cleanup_barrier_sql;
EXECUTE cleanup_barrier_stmt;
DEALLOCATE PREPARE cleanup_barrier_stmt;

SET @cleanup_completed_epoch_exists := (
  SELECT COUNT(*)
  FROM information_schema.columns
  WHERE table_schema = @cleanup_schema_name
    AND table_name = 'gb_device'
    AND column_name = 'cleanup_completed_epoch'
);
SET @cleanup_barrier_constraint_exists := (
  SELECT COUNT(*)
  FROM information_schema.table_constraints
  WHERE constraint_schema = @cleanup_schema_name
    AND table_name = 'gb_device'
    AND constraint_name = 'ck_gb_device_cleanup_completed_epoch'
);
SET @cleanup_barrier_sql := IF(
  @cleanup_completed_epoch_exists = 0,
  'ALTER TABLE `gb_device` ADD COLUMN `cleanup_completed_epoch` BIGINT NOT NULL DEFAULT 1, ADD CONSTRAINT `ck_gb_device_cleanup_completed_epoch` CHECK (`cleanup_completed_epoch` > 0 AND `cleanup_completed_epoch` <= `access_epoch`)',
  IF(
    @cleanup_barrier_constraint_exists = 0,
    'ALTER TABLE `gb_device` ADD CONSTRAINT `ck_gb_device_cleanup_completed_epoch` CHECK (`cleanup_completed_epoch` > 0 AND `cleanup_completed_epoch` <= `access_epoch`)',
    'SELECT 1'
  )
);
PREPARE cleanup_barrier_stmt FROM @cleanup_barrier_sql;
EXECUTE cleanup_barrier_stmt;
DEALLOCATE PREPARE cleanup_barrier_stmt;
