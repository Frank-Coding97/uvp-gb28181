-- Upgrade existing databases without inferring whether any account is a seeded admin.
-- Every pre-existing user receives the column default false.
SET @users_table_exists := (
  SELECT COUNT(*) FROM information_schema.TABLES
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'sys_users'
);
SET @must_change_password_exists := (
  SELECT COUNT(*) FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'sys_users'
    AND COLUMN_NAME = 'must_change_password'
);
SET @ddl := IF(@users_table_exists = 1 AND @must_change_password_exists = 0,
  'ALTER TABLE `sys_users` ADD COLUMN `must_change_password` tinyint(1) NOT NULL DEFAULT 0',
  'SELECT 1');
PREPARE initial_admin_password_stmt FROM @ddl;
EXECUTE initial_admin_password_stmt;
DEALLOCATE PREPARE initial_admin_password_stmt;
