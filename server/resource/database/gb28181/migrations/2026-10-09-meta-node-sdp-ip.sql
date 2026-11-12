-- Per-node SDP announcement address: overrides the platform default for this
-- media node. Needed when nodes sit behind different NAT/port mappings.
SET @ddl := IF((SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'meta_node' AND COLUMN_NAME = 'sdp_ip') = 0,
  'ALTER TABLE `meta_node` ADD COLUMN `sdp_ip` varchar(253) NOT NULL DEFAULT '''' AFTER `receive_host`', 'SELECT 1');
PREPARE meta_node_sdp_ip_stmt FROM @ddl;
EXECUTE meta_node_sdp_ip_stmt;
DEALLOCATE PREPARE meta_node_sdp_ip_stmt;