SET @ddl := IF((SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'meta_node' AND COLUMN_NAME = 'hook_ip') = 0,
  'ALTER TABLE `meta_node` ADD COLUMN `hook_ip` varchar(45) NOT NULL DEFAULT '''' AFTER `host`', 'SELECT 1');
PREPARE meta_node_hook_ip_stmt FROM @ddl;
EXECUTE meta_node_hook_ip_stmt;
DEALLOCATE PREPARE meta_node_hook_ip_stmt;
