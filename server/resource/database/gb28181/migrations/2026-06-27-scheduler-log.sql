-- 2026-06-27 ZLM 调度日志(M3 T3.3)增量
-- 已存在环境跑此 SQL;新环境用 scheduler_log.sql。

CREATE TABLE IF NOT EXISTS `scheduler_log` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `happened_at` datetime(3) NOT NULL,
  `algorithm` varchar(32) NOT NULL DEFAULT '',
  `node_id` bigint NOT NULL DEFAULT '0',
  `node_name` varchar(64) NOT NULL DEFAULT '',
  `stream_id` varchar(64) NOT NULL DEFAULT '',
  `device_id` varchar(64) NOT NULL DEFAULT '',
  `channel_id` varchar(64) NOT NULL DEFAULT '',
  `error_message` varchar(255) NOT NULL DEFAULT '',
  PRIMARY KEY (`id`),
  KEY `idx_happened_at` (`happened_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci COMMENT='ZLM 调度日志(异步写,7d 保留)';
