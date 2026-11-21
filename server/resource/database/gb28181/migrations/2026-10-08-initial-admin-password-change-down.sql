-- Rollback removes the state bit; take a database backup before manually reverting.
SET @must_change_password_exists := (
  SELECT COUNT(*) FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'sys_users'
    AND COLUMN_NAME = 'must_change_password'
);
SET @ddl := IF(@must_change_password_exists = 1,
  'ALTER TABLE `sys_users` DROP COLUMN `must_change_password`',
  'SELECT 1');
PREPARE initial_admin_password_down_stmt FROM @ddl;
EXECUTE initial_admin_password_down_stmt;
DEALLOCATE PREPARE initial_admin_password_down_stmt;
