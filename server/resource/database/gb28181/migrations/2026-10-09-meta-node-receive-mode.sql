-- Media node single-port RTP receive mode.
--
-- ⛔⛔ These two columns exist in the GORM model (zlm/repo/node_repo.go) and are
-- read/written by the playback, playback-retrieval and live-play paths, but they
-- were **never actually added to any database**: neither the MySQL baseline nor
-- any live MySQL database has them. Consequences on a SQLite green package:
--
--   INSERT INTO meta_node ... => SQL logic error: table meta_node has no column
--   named rtp_receive_mode
--   => the default node seed fails => the registry stays empty (nodes=0)
--   => the UI shows no media node at all ("cannot connect to the ZL node"),
--   while ZLM itself is running fine and all four services report healthy.
--
-- Defaults match the Go side: empty/'' means legacy multi-port.
SET @ddl := IF((SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'meta_node' AND COLUMN_NAME = 'rtp_receive_mode') = 0,
  'ALTER TABLE `meta_node` ADD COLUMN `rtp_receive_mode` varchar(8) NOT NULL DEFAULT ''multi'' AFTER `rtp_port_end`, ADD COLUMN `rtp_proxy_port` bigint NOT NULL DEFAULT 10000 AFTER `rtp_receive_mode`', 'SELECT 1');
PREPARE meta_node_receive_mode_stmt FROM @ddl;
EXECUTE meta_node_receive_mode_stmt;
DEALLOCATE PREPARE meta_node_receive_mode_stmt;